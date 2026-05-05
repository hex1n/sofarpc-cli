package mcp

import (
	"context"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/core/workspace"
	"github.com/hex1n/sofarpc-cli/internal/sourcecontract"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// OpenOutput is the structured payload for sofarpc_open. It returns the
// resolved workspace, the ambient target, and a capability banner the
// agent can branch on before its first invoke.
type OpenOutput struct {
	SessionID    string               `json:"sessionId"`
	ProjectRoot  string               `json:"projectRoot"`
	Target       target.Config        `json:"target,omitempty"`
	Layers       []target.Layer       `json:"layers,omitempty"`
	ConfigErrors []target.ConfigError `json:"configErrors,omitempty"`
	Capabilities Capabilities         `json:"capabilities"`
	Contract     ContractBanner       `json:"contract"`
}

// Capabilities is an up-front capability banner so agents know which
// tools will succeed without round-tripping. Keep field names stable.
type Capabilities struct {
	DirectInvoke bool `json:"directInvoke"`
	Describe     bool `json:"describe"`
	Replay       bool `json:"replay"`
}

// ContractBanner gives agents an up-front view of contract readiness and
// sourcecontract health at workspace-open time.
//
// LoadError is set when the entrypoint tried to materialize a store but
// failed (missing project root, unreadable directory, etc). Agents can
// branch on it before falling back to trusted-mode invoke.
type ContractBanner struct {
	Attached       bool              `json:"attached"`
	Source         string            `json:"source,omitempty"`
	IndexedClasses int               `json:"indexedClasses,omitempty"`
	IndexedFiles   int               `json:"indexedFiles,omitempty"`
	ParsedClasses  int               `json:"parsedClasses,omitempty"`
	IndexFailures  map[string]string `json:"indexFailures,omitempty"`
	ParseFailures  map[string]string `json:"parseFailures,omitempty"`
	LoadError      string            `json:"loadError,omitempty"`
}

func registerOpen(server *sdkmcp.Server, opts Options, holder *contractHolder) {
	envCfg := opts.TargetSources.Env
	sessions := opts.Sessions
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_open",
		Description: "Open a sofarpc workspace. Returns the resolved target, a capability banner, and a session id the agent can reuse in subsequent calls.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, in OpenInput) (*sdkmcp.CallToolResult, OpenOutput, error) {
		store := holder.Get()
		loadErr := holder.LoadError()
		ws, err := workspace.Resolve(workspace.Input{
			Cwd:     in.Cwd,
			Project: in.Project,
		})
		if err != nil {
			return &sdkmcp.CallToolResult{
				IsError: true,
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: err.Error()}},
			}, OpenOutput{}, nil
		}

		report := target.Resolve(target.Input{}, ws.Sources(envCfg))

		session := sessions.Create(Session{
			ProjectRoot: ws.ProjectRoot,
			Target:      report.Target,
		})

		out := OpenOutput{
			SessionID:    session.ID,
			ProjectRoot:  ws.ProjectRoot,
			Target:       report.Target,
			Layers:       report.Layers,
			ConfigErrors: report.ConfigErrors,
			Capabilities: Capabilities{
				DirectInvoke: true,
				Describe:     store != nil,
				Replay:       sessions != nil,
			},
			Contract: buildContractBanner(store, loadErr),
		}

		result := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: summarizeOpen(out)},
			},
		}
		return result, out, nil
	})
}

func summarizeOpen(out OpenOutput) string {
	targetState := "no target resolved"
	if out.Target.Mode != "" {
		targetState = fmt.Sprintf("target.mode=%s", out.Target.Mode)
	}
	return fmt.Sprintf("%s project=%s %s", out.SessionID, out.ProjectRoot, targetState)
}

func buildContractBanner(store any, loadErr string) ContractBanner {
	if store == nil {
		return ContractBanner{LoadError: loadErr}
	}
	banner := ContractBanner{
		Attached:  true,
		Source:    "contract-store",
		LoadError: loadErr,
	}
	if sized, ok := store.(interface{ Size() int }); ok {
		banner.ParsedClasses = sized.Size()
	}
	if diagProvider, ok := store.(interface {
		Diagnostics() sourcecontract.Diagnostics
	}); ok {
		diag := diagProvider.Diagnostics()
		banner.Source = "sourcecontract"
		banner.IndexedClasses = diag.IndexedClasses
		banner.IndexedFiles = diag.IndexedFiles
		banner.ParsedClasses = diag.ParsedClasses
		banner.IndexFailures = diag.IndexFailures
		banner.ParseFailures = diag.ParseFailures
	}
	return banner
}
