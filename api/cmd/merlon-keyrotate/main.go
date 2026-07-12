// Command merlon-keyrotate re-encrypts customers.attributes' direct-PII
// fields under the encryption key ring's current key version, in batches,
// so a key rotation (security.md §2.1) completes without downtime: reads
// and writes keep working throughout, since Encryptor.Decrypt already
// selects the key by each ciphertext's embedded key_version regardless of
// which version is "current".
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ksuk/merlon/api/internal/crypto"
)

// directPIIAttributeKeys mirrors store.directPIIAttributeKeys (the data model
// §3.1). Duplicated here rather than imported since store's copy is
// unexported and this CLI intentionally has no other dependency on the
// store package's Postgres repository types.
var directPIIAttributeKeys = []string{
	"full_name",
	"address",
	"date_of_birth",
	"phone",
	"email",
	"account_number",
	"id_document_number",
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "rotate" {
		fmt.Fprintln(os.Stderr, "usage: merlon-keyrotate rotate --database-url <url> --batch-size 500")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("rotate", flag.ExitOnError)
	databaseURL := fs.String("database-url", os.Getenv("MERLON_DATABASE_URL"), "PostgreSQL connection string")
	batchSize := fs.Int("batch-size", 500, "number of customer rows to process per batch")
	keyRingEnv := fs.String("key-ring-env", "MERLON_ENCRYPTION_KEY_RING", "environment variable holding the key ring spec")
	fs.Parse(os.Args[2:])

	if *databaseURL == "" {
		fmt.Fprintln(os.Stderr, "--database-url (or MERLON_DATABASE_URL) is required")
		os.Exit(1)
	}

	keyRing, err := crypto.NewKeyRingFromEnv(*keyRingEnv)
	if err != nil {
		slog.Error("key ring", "error", err)
		os.Exit(1)
	}
	encryptor := crypto.NewEncryptor(keyRing)

	pool, err := pgxpool.New(context.Background(), *databaseURL)
	if err != nil {
		slog.Error("connect", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	reencrypted, err := RotateAll(context.Background(), pool, encryptor, keyRing.CurrentVersion(), *batchSize)
	if err != nil {
		slog.Error("rotate", "error", err)
		os.Exit(1)
	}
	slog.Info("key rotation complete", "reencrypted_rows", reencrypted, "current_key_version", keyRing.CurrentVersion())
}

// needsRotation reports whether ciphertext's embedded key_version differs
// from currentVersion, i.e. whether it must be re-encrypted.
func needsRotation(ciphertext string, currentVersion uint8) (bool, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return false, fmt.Errorf("invalid ciphertext: %w", err)
	}
	if len(raw) < 1 {
		return false, fmt.Errorf("ciphertext too short")
	}
	return raw[0] != currentVersion, nil
}

// rotateAttributes returns a copy of attrs with every direct-PII field
// whose embedded key_version differs from currentVersion decrypted and
// re-encrypted under the encryptor's current key, and reports whether
// anything changed. A value that isn't a recognized ciphertext (e.g.
// plaintext written before encryption was ever configured) is left as-is:
// key rotation only touches already-encrypted data.
func rotateAttributes(e *crypto.Encryptor, currentVersion uint8, attrs map[string]any) (map[string]any, bool, error) {
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}

	changed := false
	for _, key := range directPIIAttributeKeys {
		s, ok := out[key].(string)
		if !ok || s == "" {
			continue
		}
		stale, err := needsRotation(s, currentVersion)
		if err != nil {
			// Not recognizable ciphertext (e.g. legacy plaintext) -- leave
			// untouched, matching store.decryptDirectPII's tolerance.
			continue
		}
		if !stale {
			continue
		}
		plaintext, err := e.Decrypt(s)
		if err != nil {
			return nil, false, fmt.Errorf("decrypt %q: %w", key, err)
		}
		reencrypted, err := e.Encrypt(plaintext)
		if err != nil {
			return nil, false, fmt.Errorf("re-encrypt %q: %w", key, err)
		}
		out[key] = reencrypted
		changed = true
	}
	return out, changed, nil
}

// RotateAll re-encrypts every customers row whose direct-PII fields aren't
// already under currentVersion, processing batchSize rows per round trip.
// It is idempotent and resumable by construction: each row's fields are
// only rewritten if they're actually stale, so re-running (e.g. after an
// interruption) is a no-op for rows already rotated and picks up exactly
// where the previous run left off, with no separate checkpoint state
// needed.
func RotateAll(ctx context.Context, pool *pgxpool.Pool, e *crypto.Encryptor, currentVersion uint8, batchSize int) (int, error) {
	total := 0
	var lastID *string

	for {
		var (
			rows pgx.Rows
			err  error
		)
		if lastID == nil {
			rows, err = pool.Query(ctx, `SELECT id, attributes FROM customers ORDER BY id LIMIT $1`, batchSize)
		} else {
			rows, err = pool.Query(ctx, `SELECT id, attributes FROM customers WHERE id > $1 ORDER BY id LIMIT $2`, *lastID, batchSize)
		}
		if err != nil {
			return total, err
		}

		type row struct {
			id    string
			attrs map[string]any
		}
		var batch []row
		for rows.Next() {
			var id string
			var raw []byte
			if err := rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return total, err
			}
			attrs := make(map[string]any)
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &attrs); err != nil {
					rows.Close()
					return total, fmt.Errorf("unmarshal attributes for customer %s: %w", id, err)
				}
			}
			batch = append(batch, row{id: id, attrs: attrs})
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return total, rowsErr
		}
		if len(batch) == 0 {
			break
		}

		for _, r := range batch {
			id := r.id
			lastID = &id
			rotated, changed, err := rotateAttributes(e, currentVersion, r.attrs)
			if err != nil {
				return total, fmt.Errorf("customer %s: %w", r.id, err)
			}
			if !changed {
				continue
			}
			encoded, err := json.Marshal(rotated)
			if err != nil {
				return total, fmt.Errorf("marshal rotated attributes for customer %s: %w", r.id, err)
			}
			if _, err := pool.Exec(ctx, `UPDATE customers SET attributes = $2 WHERE id = $1`, r.id, encoded); err != nil {
				return total, fmt.Errorf("update customer %s: %w", r.id, err)
			}
			total++
		}

		if len(batch) < batchSize {
			break
		}
	}

	return total, nil
}
