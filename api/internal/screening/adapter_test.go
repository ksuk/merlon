package screening

import (
	"context"
	"errors"
	"testing"
)

type fakeFetcher struct {
	body []byte
	err  error
}

func (f *fakeFetcher) Fetch(_ context.Context, _ string) ([]byte, error) {
	return f.body, f.err
}

const ofacFixture = `<?xml version="1.0"?>
<sdnList>
  <sdnEntry>
    <uid>7382</uid>
    <firstName>Jong Un</firstName>
    <lastName>Kim</lastName>
    <sdnType>Individual</sdnType>
    <akaList>
      <aka><lastName>Kim Jong-Un</lastName></aka>
    </akaList>
    <nationalityList>
      <nationality><country>Korea, North</country></nationality>
    </nationalityList>
  </sdnEntry>
  <sdnEntry>
    <uid>9001</uid>
    <lastName>Korea Mining Development Trading Corporation</lastName>
    <sdnType>Entity</sdnType>
  </sdnEntry>
</sdnList>`

func TestOFACAdapter_ParsesSDNXML(t *testing.T) {
	adapter := &OFACAdapter{ListID: "ofac_sdn", Fetcher: &fakeFetcher{body: []byte(ofacFixture)}}

	data, err := adapter.FetchList(context.Background())
	if err != nil {
		t.Fatalf("FetchList: %v", err)
	}

	if data.ListType != "sanctions" {
		t.Errorf("ListType = %q, want sanctions", data.ListType)
	}
	if len(data.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(data.Entries))
	}
	first := data.Entries[0]
	if first.EntryID != "7382" || first.Type != "individual" {
		t.Errorf("first entry = %+v", first)
	}
	if len(first.Names) != 2 || first.Names[0] != "Jong Un Kim" || first.Names[1] != "Kim Jong-Un" {
		t.Errorf("first.Names = %v", first.Names)
	}
	if first.Country != "Korea, North" {
		t.Errorf("first.Country = %q", first.Country)
	}
}

const euCSVFixture = "entry_id,name,country,type\n" +
	"EU-001,Kim Jong Un,KP,individual\n" +
	"EU-001,Kim Jong-Un,KP,individual\n" +
	"EU-002,Some Entity,MO,entity\n"

func TestEUAdapter_ParsesCSVAndMergesAliasRows(t *testing.T) {
	adapter := &EUAdapter{ListID: "eu_sanctions", Fetcher: &fakeFetcher{body: []byte(euCSVFixture)}}

	data, err := adapter.FetchList(context.Background())
	if err != nil {
		t.Fatalf("FetchList: %v", err)
	}
	if len(data.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(data.Entries))
	}
	if data.Entries[0].EntryID != "EU-001" || len(data.Entries[0].Names) != 2 {
		t.Errorf("entries[0] = %+v", data.Entries[0])
	}
}

func TestEUAdapter_RetainsSecondaryIdentifiersAndEntityTypeAcrossAliasRows(t *testing.T) {
	fixture := "entry_id,name,dates_of_birth,addresses,country,entity_type\n" +
		"EU-003,Example Person,1980-01-02|1981-02-03,1 Chiyoda Tokyo|2 Chiyoda Tokyo,JP,individual\n" +
		"EU-003,Example P.,1982-03-04,3 Chiyoda Tokyo,JP,individual\n"
	data, err := (&EUAdapter{ListID: "eu", Fetcher: &fakeFetcher{body: []byte(fixture)}}).FetchList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	entry := data.Entries[0]
	if len(entry.DatesOfBirth) != 3 || len(entry.Addresses) != 3 || entry.Country != "JP" || entry.EntityType != "individual" {
		t.Fatalf("entry=%+v, want all secondary values retained", entry)
	}
}

const mofCSVFixture = "entry_id,name,country,type\n" +
	"MOF-001,Kim Jong Un,KP,individual\n"

func TestMOFAdapter_ParsesCSV(t *testing.T) {
	adapter := &MOFAdapter{ListID: "mof_japan", Format: "csv", Fetcher: &fakeFetcher{body: []byte(mofCSVFixture)}}

	data, err := adapter.FetchList(context.Background())
	if err != nil {
		t.Fatalf("FetchList: %v", err)
	}
	if len(data.Entries) != 1 || data.Entries[0].EntryID != "MOF-001" {
		t.Errorf("entries = %+v", data.Entries)
	}
}

const mofHTMLFixture = `<table>
<tr><td>entry_id</td><td>name</td><td>country</td><td>type</td></tr>
<tr><td>MOF-001</td><td>Kim Jong Un</td><td>KP</td><td>individual</td></tr>
<tr><td>MOF-002</td><td>Banco Delta Asia</td><td>MO</td><td>entity</td></tr>
</table>`

func TestMOFAdapter_ParsesHTMLTable(t *testing.T) {
	adapter := &MOFAdapter{ListID: "mof_japan", Format: "html", Fetcher: &fakeFetcher{body: []byte(mofHTMLFixture)}}

	data, err := adapter.FetchList(context.Background())
	if err != nil {
		t.Fatalf("FetchList: %v", err)
	}
	if len(data.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(data.Entries))
	}
	if data.Entries[1].EntryID != "MOF-002" || data.Entries[1].Names[0] != "Banco Delta Asia" {
		t.Errorf("entries[1] = %+v", data.Entries[1])
	}
}

const unXMLFixture = `<?xml version="1.0"?>
<CONSOLIDATED_LIST>
  <INDIVIDUALS>
    <INDIVIDUAL>
      <DATAID>KPi.001</DATAID>
      <FIRST_NAME>Jong Un</FIRST_NAME>
      <SECOND_NAME>Kim</SECOND_NAME>
      <NATIONALITY><VALUE>Korea, North</VALUE></NATIONALITY>
    </INDIVIDUAL>
  </INDIVIDUALS>
  <ENTITIES>
    <ENTITY>
      <DATAID>KPe.001</DATAID>
      <FIRST_NAME>Korea Mining Development Trading Corporation</FIRST_NAME>
    </ENTITY>
  </ENTITIES>
</CONSOLIDATED_LIST>`

func TestUNAdapter_ParsesConsolidatedListXML(t *testing.T) {
	adapter := &UNAdapter{ListID: "un_sc", Fetcher: &fakeFetcher{body: []byte(unXMLFixture)}}

	data, err := adapter.FetchList(context.Background())
	if err != nil {
		t.Fatalf("FetchList: %v", err)
	}
	if len(data.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(data.Entries))
	}
	if data.Entries[0].Type != "individual" || data.Entries[1].Type != "entity" {
		t.Errorf("entries = %+v", data.Entries)
	}
	if data.Entries[0].Country != "Korea, North" {
		t.Errorf("entries[0].Country = %q", data.Entries[0].Country)
	}
}

const pepJSONFixture = `{"entries":[
  {"entry_id":"PEP-001","names":["Taro Yamada","山田 太郎"],"country":"JP","type":"individual"}
]}`

func TestPEPAdapter_ParsesJSON(t *testing.T) {
	adapter := &PEPAdapter{ListID: "pep_provider", URL: "https://pep.example.test/feed", Fetcher: &fakeFetcher{body: []byte(pepJSONFixture)}}

	data, err := adapter.FetchList(context.Background())
	if err != nil {
		t.Fatalf("FetchList: %v", err)
	}
	if data.ListType != "pep" {
		t.Errorf("ListType = %q, want pep", data.ListType)
	}
	if len(data.Entries) != 1 || data.Entries[0].EntryID != "PEP-001" {
		t.Errorf("entries = %+v", data.Entries)
	}
}

func TestPEPAdapter_RCAListTypePreserved(t *testing.T) {
	adapter := &PEPAdapter{
		ListID:   "pep_rca_provider",
		ListType: "PEP-RCA",
		URL:      "https://pep.example.test/rca-feed",
		Fetcher:  &fakeFetcher{body: []byte(pepJSONFixture)},
	}

	data, err := adapter.FetchList(context.Background())
	if err != nil {
		t.Fatalf("FetchList: %v", err)
	}
	if data.ListType != "PEP-RCA" {
		t.Errorf("ListType = %q, want PEP-RCA", data.ListType)
	}
}

func TestPEPAdapter_SkipsWhenNotConfigured(t *testing.T) {
	adapter := &PEPAdapter{ListID: "pep_provider", URL: ""}

	_, err := adapter.FetchList(context.Background())
	if !errors.Is(err, ErrPEPNotConfigured) {
		t.Errorf("err = %v, want ErrPEPNotConfigured", err)
	}
}
