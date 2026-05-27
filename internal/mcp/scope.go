package mcp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/core/workspace"
)

type toolScope struct {
	ProjectRoot string
	Sources     target.Sources
	Source      string
}

func resolveToolScope(base target.Sources, sessions *SessionStore, sessionID, cwd, project string) (toolScope, error) {
	sessionID = strings.TrimSpace(sessionID)
	if strings.TrimSpace(cwd) != "" || strings.TrimSpace(project) != "" {
		ws, err := workspace.Resolve(workspace.Input{Cwd: cwd, Project: project})
		if err != nil {
			return toolScope{}, fmt.Errorf("resolve workspace: %w", err)
		}
		if sessionID != "" {
			sessionRoot, err := sessionProjectRoot(sessions, sessionID)
			if err != nil {
				return toolScope{}, err
			}
			if !sameProjectRoot(sessionRoot, ws.ProjectRoot) {
				return toolScope{}, fmt.Errorf("session %q project root %q does not match requested project root %q", sessionID, sessionRoot, ws.ProjectRoot)
			}
		}
		return toolScope{ProjectRoot: ws.ProjectRoot, Sources: ws.Sources(base.Env), Source: "project"}, nil
	}
	if sessionID != "" {
		root, err := sessionProjectRoot(sessions, sessionID)
		if err != nil {
			return toolScope{}, err
		}
		return toolScope{ProjectRoot: root, Sources: target.ProjectSources(root, base.Env), Source: "session"}, nil
	}
	if strings.TrimSpace(base.ProjectRoot) != "" {
		return toolScope{ProjectRoot: base.ProjectRoot, Sources: target.ProjectSources(base.ProjectRoot, base.Env), Source: "ambient"}, nil
	}
	return toolScope{Sources: base, Source: "ambient"}, nil
}

func sessionProjectRoot(sessions *SessionStore, sessionID string) (string, error) {
	if sessions == nil {
		return "", fmt.Errorf("session %q cannot be resolved: no session store attached", sessionID)
	}
	session, ok := sessions.Get(sessionID)
	if !ok {
		return "", fmt.Errorf("session %q not found", sessionID)
	}
	root := strings.TrimSpace(session.ProjectRoot)
	if root == "" {
		return "", fmt.Errorf("session %q has no project root", sessionID)
	}
	return root, nil
}

func sameProjectRoot(left, right string) bool {
	left = canonicalProjectRoot(left)
	right = canonicalProjectRoot(right)
	if left == "" || right == "" {
		return false
	}
	return strings.EqualFold(left, right)
}

func canonicalProjectRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Clean(root)
}
