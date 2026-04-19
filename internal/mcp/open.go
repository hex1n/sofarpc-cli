package mcp

import (
	"context"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/core/workspace"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// OpenOutput is the structured payload for sofarpc_open. It mirrors
// architecture §3.1: enough information for the agent to decide whether
// to proceed or call sofarpc_doctor.
type OpenOutput struct {
	SessionID    string         `json:"sessionId"`
	ProjectRoot  string         `json:"projectRoot"`
	Target       target.Config  `json:"target,omitempty"`
	Layers       []target.Layer `json:"layers,omitempty"`
	Facade       FacadeState    `json:"facade"`
	Capabilities Capabilities   `json:"capabilities"`
}

// FacadeState reports the local facade/index status. Until the indexer
// is wired (architecture §6), Configured/Indexed stay false.
type FacadeState struct {
	Configured bool `json:"configured"`
	Indexed    bool `json:"indexed"`
	Services   int  `json:"services"`
}

// Capabilities is an up-front capability banner so agents know which
// tools will succeed without round-tripping. Keep field names stable.
//
// Reindex tells the agent whether sofarpc_describe refresh=true is a
// real recovery path: without a wired indexer the handler will reject
// refresh up-front, and the agent should skip it rather than learn
// that the hard way.
type Capabilities struct {
	FacadeIndex bool `json:"facadeIndex"`
	Worker      bool `json:"worker"`
	Reindex     bool `json:"reindex"`
}

// facadeBanner is implemented by facade stores that can cheaply report
// the number of indexed services. *indexer.Index satisfies it; in-memory
// test stores don't need to.
type facadeBanner interface {
	Size() int
	Services() []string
}

func registerOpen(server *sdkmcp.Server, opts Options, holder *facadeHolder) {
	envCfg := opts.TargetSources.Env
	sessions := opts.Sessions
	workerReady := opts.Worker != nil && !opts.Worker.Profile.Empty()
	reindexReady := opts.Reindexer != nil
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_open",
		Description: "Open a sofarpc workspace. Returns the resolved target, facade state, and a session id the agent can reuse in subsequent calls.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, in OpenInput) (*sdkmcp.CallToolResult, OpenOutput, error) {
		facade := holder.Get()
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

		facadeState := FacadeState{Configured: facade != nil}
		if banner, ok := facade.(facadeBanner); ok {
			facadeState.Indexed = banner.Size() > 0
			facadeState.Services = len(banner.Services())
		}

		out := OpenOutput{
			SessionID:   session.ID,
			ProjectRoot: ws.ProjectRoot,
			Target:      report.Target,
			Layers:      report.Layers,
			Facade:      facadeState,
			Capabilities: Capabilities{
				FacadeIndex: facade != nil,
				Worker:      workerReady,
				Reindex:     reindexReady,
			},
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
	base := fmt.Sprintf("%s project=%s %s", out.SessionID, out.ProjectRoot, targetState)
	// When the facade is empty but a reindexer is wired, point the agent
	// at the concrete recovery path instead of waiting for a later
	// describe call to fail. This is the one place we know both facts
	// up-front.
	if !out.Facade.Indexed && out.Capabilities.Reindex {
		base += " — call sofarpc_describe refresh=true to populate the index"
	}
	return base
}
