// Package screening implements the Go-side orchestration around the Rust
// screening engine: external sanctions/PEP list ingestion, CDD-tier-driven
// rescreening scheduling, and list freshness monitoring (screening.md).
//
// All external-source-specific parsing (OFAC SDN XML, EU/MOF CSV, UN XML,
// PEP provider JSON) is confined to this file (Adapter Isolation principle):
// importer.go and scheduler.go only ever see the source-agnostic
// RawListData/RawListEntry shapes.
package screening

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// RawListEntry is a single sanctions/PEP list entry as parsed from an
// upstream source, before conversion to the Rust engine's
// engine.ScreeningListConfig entry shape (config.rs ListEntry).
type RawListEntry struct {
	EntryID string
	Names   []string
	Country string
	Type    string
}

// RawListData is the full parsed content of one list fetch, corresponding
// 1:1 to a ScreeningListConfig (list_id/list_type/name/source/entries).
type RawListData struct {
	ListID   string
	ListType string
	Name     string
	Source   string
	Entries  []RawListEntry
}

// ErrPEPNotConfigured is returned by PEPAdapter.FetchList when no PEP data
// provider endpoint has been configured. RunImportJob treats this as a
// skip-and-audit-log case, not a failure (screening.md "PEP リスト未設定の場合、
// PEP 照合はスキップされるが、その旨を監査ログに記録する").
var ErrPEPNotConfigured = errors.New("PEP list provider not configured")

// HTTPFetcher retrieves raw bytes from an upstream URL. Production code uses
// defaultHTTPFetcher; tests inject a fixture-backed fake so that no real
// OFAC/EU/UN/MOF/PEP endpoint is ever contacted from this codebase (sandbox
// constraint: adapters must stay mockable, real endpoint connectivity is an
// operator-side deployment concern).
type HTTPFetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// maxListResponseBytes caps how much of an upstream response body an adapter
// will read, so a misbehaving or compromised endpoint cannot exhaust memory.
const maxListResponseBytes = 64 << 20 // 64 MiB

// defaultHTTPFetcher is the production HTTPFetcher, used when no fixture
// fetcher is injected.
type defaultHTTPFetcher struct {
	client *http.Client
}

// NewDefaultHTTPFetcher builds the production HTTPFetcher used by list
// adapters, with a bounded per-request timeout.
func NewDefaultHTTPFetcher(timeout time.Duration) HTTPFetcher {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &defaultHTTPFetcher{client: &http.Client{Timeout: timeout}}
}

func (f *defaultHTTPFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %q: HTTP %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxListResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response from %q: %w", url, err)
	}
	return body, nil
}

// ListAdapter fetches and parses one external sanctions/PEP list source.
// Concrete adapters (OFACAdapter, EUAdapter, UNAdapter, MOFAdapter,
// PEPAdapter) encapsulate the format-specific parser for their source
// (screening.md §リスト自動取り込み table); RunImportJob only depends on this
// interface.
type ListAdapter interface {
	FetchList(ctx context.Context) (*RawListData, error)
}

// OFACAdapter fetches the OFAC SDN list (XML, full replace, daily default).
type OFACAdapter struct {
	ListID  string
	URL     string
	Fetcher HTTPFetcher
}

func (a *OFACAdapter) FetchList(ctx context.Context) (*RawListData, error) {
	body, err := a.Fetcher.Fetch(ctx, a.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch OFAC SDN list: %w", err)
	}
	return parseOFACSDNXML(a.ListID, body)
}

// ofacSDNList models the subset of the OFAC SDN.xml schema this system
// consumes (uid/name/type/aka/nationality). Adjust field mappings here if
// the operator's mirrored copy of the feed differs from the public schema.
type ofacSDNList struct {
	XMLName xml.Name       `xml:"sdnList"`
	Entries []ofacSDNEntry `xml:"sdnEntry"`
}

type ofacSDNEntry struct {
	UID       string `xml:"uid"`
	FirstName string `xml:"firstName"`
	LastName  string `xml:"lastName"`
	SDNType   string `xml:"sdnType"`
	AkaList   struct {
		Akas []struct {
			FirstName string `xml:"firstName"`
			LastName  string `xml:"lastName"`
		} `xml:"aka"`
	} `xml:"akaList"`
	NationalityList struct {
		Nationalities []struct {
			Country string `xml:"country"`
		} `xml:"nationality"`
	} `xml:"nationalityList"`
}

func parseOFACSDNXML(listID string, body []byte) (*RawListData, error) {
	var parsed ofacSDNList
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse OFAC SDN XML: %w", err)
	}

	data := &RawListData{
		ListID:   listID,
		ListType: "sanctions",
		Name:     "OFAC SDN",
		Source:   "OFAC",
	}
	for _, e := range parsed.Entries {
		entry := RawListEntry{
			EntryID: e.UID,
			Type:    strings.ToLower(e.SDNType),
		}
		if name := strings.TrimSpace(e.FirstName + " " + e.LastName); name != "" {
			entry.Names = append(entry.Names, name)
		}
		for _, aka := range e.AkaList.Akas {
			if name := strings.TrimSpace(aka.FirstName + " " + aka.LastName); name != "" {
				entry.Names = append(entry.Names, name)
			}
		}
		if len(e.NationalityList.Nationalities) > 0 {
			entry.Country = e.NationalityList.Nationalities[0].Country
		}
		data.Entries = append(data.Entries, entry)
	}
	return data, nil
}

// EUAdapter fetches the EU Financial Sanctions consolidated list (CSV, full
// replace, daily default). Rows sharing the same entity_id are merged into
// one entry with multiple name aliases.
type EUAdapter struct {
	ListID  string
	URL     string
	Fetcher HTTPFetcher
}

func (a *EUAdapter) FetchList(ctx context.Context) (*RawListData, error) {
	body, err := a.Fetcher.Fetch(ctx, a.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch EU sanctions list: %w", err)
	}
	return parseEUCSV(a.ListID, body)
}

func parseEUCSV(listID string, body []byte) (*RawListData, error) {
	entries, err := parseNameAliasCSV(body)
	if err != nil {
		return nil, fmt.Errorf("parse EU sanctions CSV: %w", err)
	}
	return &RawListData{
		ListID:   listID,
		ListType: "sanctions",
		Name:     "EU Financial Sanctions",
		Source:   "EU",
		Entries:  entries,
	}, nil
}

// MOFAdapter fetches the Japanese MOF/METI foreign exchange act sanctions
// list (外国為替及び外国貿易法), published as CSV or HTML depending on the
// operator's mirrored source (screening.md table). Format defaults to CSV.
type MOFAdapter struct {
	ListID  string
	URL     string
	Format  string // "csv" (default) or "html"
	Fetcher HTTPFetcher
}

func (a *MOFAdapter) FetchList(ctx context.Context) (*RawListData, error) {
	body, err := a.Fetcher.Fetch(ctx, a.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch MOF/METI sanctions list: %w", err)
	}

	var entries []RawListEntry
	switch a.Format {
	case "html":
		entries, err = parseNameAliasHTMLTable(body)
	default:
		entries, err = parseNameAliasCSV(body)
	}
	if err != nil {
		return nil, fmt.Errorf("parse MOF/METI sanctions list: %w", err)
	}

	return &RawListData{
		ListID:   a.ListID,
		ListType: "sanctions",
		Name:     "日本外為法制裁対象者リスト",
		Source:   "MOF/METI",
		Entries:  entries,
	}, nil
}

// parseNameAliasCSV parses the shared CSV shape used by the EU and MOF
// adapters: header `entry_id,name,country,type`, one row per name alias,
// multiple rows sharing an entry_id are merged into one entry.
func parseNameAliasCSV(body []byte) ([]RawListEntry, error) {
	reader := csv.NewReader(strings.NewReader(string(body)))
	reader.TrimLeadingSpace = true

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	header := rows[0]
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, required := range []string{"entry_id", "name"} {
		if _, ok := col[required]; !ok {
			return nil, fmt.Errorf("CSV missing required column %q", required)
		}
	}

	order := []string{}
	byID := map[string]*RawListEntry{}
	for _, row := range rows[1:] {
		id := field(row, col, "entry_id")
		if id == "" {
			continue
		}
		e, ok := byID[id]
		if !ok {
			e = &RawListEntry{
				EntryID: id,
				Country: field(row, col, "country"),
				Type:    field(row, col, "type"),
			}
			byID[id] = e
			order = append(order, id)
		}
		if name := field(row, col, "name"); name != "" {
			e.Names = append(e.Names, name)
		}
	}

	entries := make([]RawListEntry, 0, len(order))
	for _, id := range order {
		entries = append(entries, *byID[id])
	}
	return entries, nil
}

func field(row []string, col map[string]int, name string) string {
	i, ok := col[name]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// mofHTMLRowPattern extracts a 4-column <tr><td>entry_id</td><td>name</td>
// <td>country</td><td>type</td></tr> table row. Government-published HTML
// sanction lists commonly use a plain table with no nested markup inside
// cells; this deliberately does not attempt to handle arbitrary HTML.
var mofHTMLRowPattern = regexp.MustCompile(`(?is)<tr>\s*<td>(.*?)</td>\s*<td>(.*?)</td>\s*<td>(.*?)</td>\s*<td>(.*?)</td>\s*</tr>`)

func parseNameAliasHTMLTable(body []byte) ([]RawListEntry, error) {
	matches := mofHTMLRowPattern.FindAllStringSubmatch(string(body), -1)

	order := []string{}
	byID := map[string]*RawListEntry{}
	for _, m := range matches {
		id := strings.TrimSpace(m[1])
		if id == "" || strings.EqualFold(id, "entry_id") {
			continue // header row
		}
		e, ok := byID[id]
		if !ok {
			e = &RawListEntry{EntryID: id, Country: strings.TrimSpace(m[3]), Type: strings.TrimSpace(m[4])}
			byID[id] = e
			order = append(order, id)
		}
		if name := strings.TrimSpace(m[2]); name != "" {
			e.Names = append(e.Names, name)
		}
	}

	entries := make([]RawListEntry, 0, len(order))
	for _, id := range order {
		entries = append(entries, *byID[id])
	}
	return entries, nil
}

// UNAdapter fetches the UN Security Council consolidated sanctions list
// (XML, full replace, daily default).
type UNAdapter struct {
	ListID  string
	URL     string
	Fetcher HTTPFetcher
}

func (a *UNAdapter) FetchList(ctx context.Context) (*RawListData, error) {
	body, err := a.Fetcher.Fetch(ctx, a.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch UN consolidated list: %w", err)
	}
	return parseUNXML(a.ListID, body)
}

// unConsolidatedList models the subset of the UN Security Council
// consolidated list XML schema this system consumes.
type unConsolidatedList struct {
	XMLName     xml.Name `xml:"CONSOLIDATED_LIST"`
	Individuals struct {
		Individual []unListEntry `xml:"INDIVIDUAL"`
	} `xml:"INDIVIDUALS"`
	Entities struct {
		Entity []unListEntry `xml:"ENTITY"`
	} `xml:"ENTITIES"`
}

type unListEntry struct {
	DataID      string `xml:"DATAID"`
	FirstName   string `xml:"FIRST_NAME"`
	SecondName  string `xml:"SECOND_NAME"`
	Nationality struct {
		Value []string `xml:"VALUE"`
	} `xml:"NATIONALITY"`
}

func parseUNXML(listID string, body []byte) (*RawListData, error) {
	var parsed unConsolidatedList
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse UN consolidated list XML: %w", err)
	}

	data := &RawListData{
		ListID:   listID,
		ListType: "sanctions",
		Name:     "UN Security Council Consolidated List",
		Source:   "UN",
	}
	for _, e := range parsed.Individuals.Individual {
		data.Entries = append(data.Entries, unEntryToRaw(e, "individual"))
	}
	for _, e := range parsed.Entities.Entity {
		data.Entries = append(data.Entries, unEntryToRaw(e, "entity"))
	}
	return data, nil
}

func unEntryToRaw(e unListEntry, entryType string) RawListEntry {
	entry := RawListEntry{EntryID: e.DataID, Type: entryType}
	if name := strings.TrimSpace(e.FirstName + " " + e.SecondName); name != "" {
		entry.Names = append(entry.Names, name)
	}
	if len(e.Nationality.Value) > 0 {
		entry.Country = e.Nationality.Value[0]
	}
	return entry
}

// PEPAdapter fetches a commercial PEP (and, when ListType is "PEP-RCA", PEP
// family/close-associate) data provider feed (JSON, differential update).
// PEP data is commercial-only; when URL is empty the provider has not been
// contracted/configured and FetchList returns ErrPEPNotConfigured so the
// import job can skip and audit-log it rather than fail (screening.md).
type PEPAdapter struct {
	ListID   string
	ListType string // "pep" (default) or "PEP-RCA"
	URL      string
	Fetcher  HTTPFetcher
}

func (a *PEPAdapter) FetchList(ctx context.Context) (*RawListData, error) {
	if a.URL == "" {
		return nil, ErrPEPNotConfigured
	}

	body, err := a.Fetcher.Fetch(ctx, a.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch PEP provider feed: %w", err)
	}
	return parsePEPJSON(a.ListID, a.listType(), body)
}

func (a *PEPAdapter) listType() string {
	if a.ListType == "" {
		return "pep"
	}
	return a.ListType
}

type pepProviderResponse struct {
	Entries []struct {
		EntryID string   `json:"entry_id"`
		Names   []string `json:"names"`
		Country string   `json:"country"`
		Type    string   `json:"type"`
	} `json:"entries"`
}

func parsePEPJSON(listID, listType string, body []byte) (*RawListData, error) {
	var parsed pepProviderResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse PEP provider JSON: %w", err)
	}

	data := &RawListData{
		ListID:   listID,
		ListType: listType,
		Name:     "PEP Provider Feed",
		Source:   "PEP provider",
	}
	for _, e := range parsed.Entries {
		data.Entries = append(data.Entries, RawListEntry{
			EntryID: e.EntryID,
			Names:   e.Names,
			Country: e.Country,
			Type:    e.Type,
		})
	}
	return data, nil
}
