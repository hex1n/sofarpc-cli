package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildVersionDefaultsToDev(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = ""

	if got := buildVersion(); got != "dev" {
		t.Fatalf("buildVersion() = %q, want dev", got)
	}
}

func TestRunVersionText(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	})
	version = "v0.1.0"
	commit = "abc1234"
	date = "2026-04-24T00:00:00Z"

	var buf bytes.Buffer
	if err := runVersion(&buf, nil); err != nil {
		t.Fatalf("runVersion: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"sofarpc-mcp", "v0.1.0", "commit=abc1234", "date=2026-04-24T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Fatalf("version output %q missing %q", out, want)
		}
	}
}

func TestRunVersionJSON(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	})
	version = "v0.1.0"
	commit = "abc1234"
	date = "2026-04-24T00:00:00Z"

	var buf bytes.Buffer
	if err := runVersion(&buf, []string{"-json"}); err != nil {
		t.Fatalf("runVersion: %v", err)
	}
	var got versionInfo
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal version json: %v", err)
	}
	if got.Name != "sofarpc-mcp" || got.Version != "v0.1.0" || got.Commit != "abc1234" || got.Date != "2026-04-24T00:00:00Z" {
		t.Fatalf("version json = %+v", got)
	}
}
