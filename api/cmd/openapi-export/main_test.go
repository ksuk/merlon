package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ksuk/merlon/api/internal/server"
)

// wantPaths derives the expected path set from the same spec builder the
// CLI itself calls, so this test doesn't duplicate a hardcoded path list
// that could drift from internal/server/openapi_test.go's previousPaths.
func wantPaths(t *testing.T) map[string]any {
	t.Helper()
	spec := server.BuildOpenAPISpec()
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("server.BuildOpenAPISpec()[\"paths\"] missing or not an object")
	}
	return paths
}

func decodeAndCheck(t *testing.T, data []byte) {
	t.Helper()

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if got["openapi"] != "3.0.3" {
		t.Errorf(`output["openapi"] = %v, want "3.0.3"`, got["openapi"])
	}

	gotPaths, ok := got["paths"].(map[string]any)
	if !ok {
		t.Fatal("output.paths missing or not an object")
	}
	for p := range wantPaths(t) {
		if _, ok := gotPaths[p]; !ok {
			t.Errorf("path %q missing from exported openapi.json", p)
		}
	}
}

func TestRun_Stdout(t *testing.T) {
	var buf bytes.Buffer
	if err := run("", &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	decodeAndCheck(t, buf.Bytes())
}

func TestRun_WritesFileCreatingParentDirs(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "nested", "sub", "openapi.json")

	if err := run(outPath, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	decodeAndCheck(t, data)
}

func TestRun_InvalidOutputPathErrors(t *testing.T) {
	// A path under a file (not a directory) can't have children created,
	// so this must surface an error rather than panic or silently succeed.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := run(filepath.Join(blocker, "sub", "openapi.json"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("run: expected error for output path under a non-directory, got nil")
	}
}
