// Command openapi-export renders the Merlon HTTP API's OpenAPI 3.0.3
// document (built by internal/server.BuildOpenAPISpec, the same spec served
// live at GET /api/v1/openapi.json) as pretty-printed JSON, so it can be
// checked into docs/api/ for the docs site build rather than fetched from a
// running server.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ksuk/merlon/api/internal/server"
)

func main() {
	outPath := flag.String("o", "", "output file path (default: stdout)")
	flag.Parse()

	if err := run(*outPath, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "openapi-export:", err)
		os.Exit(1)
	}
}

// run marshals the OpenAPI spec as pretty-printed JSON and writes it to
// stdout (outPath == "") or to outPath, creating any missing parent
// directories first.
func run(outPath string, stdout io.Writer) error {
	data, err := json.MarshalIndent(server.BuildOpenAPISpec(), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal openapi spec: %w", err)
	}
	data = append(data, '\n')

	if outPath == "" {
		_, err := stdout.Write(data)
		return err
	}

	if dir := filepath.Dir(outPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	return nil
}
