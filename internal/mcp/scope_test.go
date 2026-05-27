package mcp

import (
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
)

func TestResolveToolScope_ProjectInputOverridesSession(t *testing.T) {
	projectRoot := t.TempDir()
	writeMCPProjectFile(t, projectRoot, ".sofarpc/config.local.json", `{"directUrl":"bolt://project-host:12200"}`)
	sessions := NewSessionStore()
	session := sessions.Create(Session{ProjectRoot: projectRoot})

	scope, err := resolveToolScope(target.Sources{}, sessions, session.ID, "", projectRoot)
	if err != nil {
		t.Fatalf("resolveToolScope: %v", err)
	}
	report := target.Resolve(target.Input{}, scope.Sources)
	if scope.Source != "project" {
		t.Fatalf("scope source: got %q want project", scope.Source)
	}
	if scope.ProjectRoot != projectRoot {
		t.Fatalf("projectRoot: got %q want %q", scope.ProjectRoot, projectRoot)
	}
	if !strings.Contains(report.Target.DirectURL, "project-host") {
		t.Fatalf("target should come from explicit project, got %+v", report.Target)
	}
}

func TestResolveToolScope_ProjectInputRejectsMismatchedSession(t *testing.T) {
	projectRoot := t.TempDir()
	sessionRoot := t.TempDir()
	sessions := NewSessionStore()
	session := sessions.Create(Session{ProjectRoot: sessionRoot})

	_, err := resolveToolScope(target.Sources{}, sessions, session.ID, "", projectRoot)
	if err == nil {
		t.Fatal("mismatched session and project roots should be an error")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error should mention mismatch, got %v", err)
	}
}

func TestResolveToolScope_SessionNotFoundIsError(t *testing.T) {
	_, err := resolveToolScope(target.Sources{}, NewSessionStore(), "ws_missing", "", "")
	if err == nil {
		t.Fatal("missing session should be an error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error should mention missing session, got %v", err)
	}
}
