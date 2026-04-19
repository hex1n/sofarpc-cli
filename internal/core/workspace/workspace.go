// Package workspace resolves the project root sofarpc-cli anchors to.
// That's the only on-disk concern — sofarpc-cli no longer reads any
// project-level config file. All non-flag configuration comes through
// SOFARPC_* env supplied by the MCP host.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
)

// Input is what the agent (or sofarpc_open handler) supplies. All fields
// are optional; missing ones are filled by Resolve from the process CWD.
type Input struct {
	Cwd     string
	Project string
}

// Workspace is the materialised view of a project directory. ProjectRoot
// is always populated — it is the path Resolve decided to anchor at.
type Workspace struct {
	ProjectRoot string
}

// Resolve figures out the project root.
//
// Resolution order for ProjectRoot:
//  1. Input.Project (absolute, or relative to Input.Cwd / os.Getwd)
//  2. Input.Cwd
//  3. os.Getwd()
func Resolve(in Input) (Workspace, error) {
	root, err := resolveRoot(in)
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{ProjectRoot: root}, nil
}

// Sources projects the workspace into the layers the target resolver
// expects. env is typically the MCP-env layer built by the entrypoint;
// pass target.Config{} when there is none.
func (w Workspace) Sources(env target.Config) target.Sources {
	return target.Sources{
		Env:         env,
		ProjectRoot: w.ProjectRoot,
	}
}

func resolveRoot(in Input) (string, error) {
	candidate := in.Project
	if candidate == "" {
		candidate = in.Cwd
	}
	if candidate == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
		candidate = wd
	} else if !filepath.IsAbs(candidate) {
		base := in.Cwd
		if base == "" {
			wd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("getwd: %w", err)
			}
			base = wd
		}
		candidate = filepath.Join(base, candidate)
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("stat project root %q: %w", candidate, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project root %q is not a directory", candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("abs project root: %w", err)
	}
	return abs, nil
}
