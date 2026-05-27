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

type toolContext struct {
	toolScope
	Contract       contractSnapshot
	ContractBanner ContractBanner
}

func resolveOpenContext(base target.Sources, holder *contractHolder, cwd, project string) (toolContext, error) {
	scope, err := resolveWorkspaceScope(base, cwd, project)
	if err != nil {
		return toolContext{}, err
	}
	return attachContractContext(scope, holder), nil
}

func resolveToolContext(base target.Sources, sessions *SessionStore, holder *contractHolder, sessionID, cwd, project string) (toolContext, error) {
	scope, err := resolveToolScope(base, sessions, sessionID, cwd, project)
	if err != nil {
		return toolContext{}, err
	}
	return attachContractContext(scope, holder), nil
}

func resolveToolScope(base target.Sources, sessions *SessionStore, sessionID, cwd, project string) (toolScope, error) {
	sessionID = strings.TrimSpace(sessionID)
	if strings.TrimSpace(cwd) != "" || strings.TrimSpace(project) != "" {
		scope, err := resolveWorkspaceScope(base, cwd, project)
		if err != nil {
			return toolScope{}, err
		}
		if sessionID != "" {
			sessionRoot, err := sessionProjectRoot(sessions, sessionID)
			if err != nil {
				return toolScope{}, err
			}
			if !sameProjectRoot(sessionRoot, scope.ProjectRoot) {
				return toolScope{}, fmt.Errorf("session %q project root %q does not match requested project root %q", sessionID, sessionRoot, scope.ProjectRoot)
			}
		}
		return scope, nil
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

func resolveWorkspaceScope(base target.Sources, cwd, project string) (toolScope, error) {
	ws, err := workspace.Resolve(workspace.Input{Cwd: cwd, Project: project})
	if err != nil {
		return toolScope{}, fmt.Errorf("resolve workspace: %w", err)
	}
	return toolScope{ProjectRoot: ws.ProjectRoot, Sources: ws.Sources(base.Env), Source: "project"}, nil
}

func attachContractContext(scope toolScope, holder *contractHolder) toolContext {
	snapshot := contractSnapshot{}
	if holder != nil {
		snapshot = holder.ForProject(scope.ProjectRoot)
	}
	return toolContext{
		toolScope:      scope,
		Contract:       snapshot,
		ContractBanner: buildContractBannerForSnapshot(snapshot),
	}
}

func buildContractBannerForSnapshot(snapshot contractSnapshot) ContractBanner {
	return buildContractBanner(snapshot.store, snapshot.loadError, snapshot.root)
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
