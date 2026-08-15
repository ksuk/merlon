package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRequiresActorBeforeConnectingForApply(t *testing.T) {
	err := run(context.Background(), []string{"--source-dir", t.TempDir(), "--apply"}, func(string) string { return "" }, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--actor") {
		t.Fatalf("err = %v, want --actor validation", err)
	}
}
