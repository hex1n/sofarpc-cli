package mcp

import (
	"context"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TargetInput mirrors architecture.md §3.3. Fields are optional; omitted
// values fall through to env / defaults.
type TargetInput struct {
	Cwd              string `json:"cwd,omitempty"`
	Project          string `json:"project,omitempty"`
	SessionID        string `json:"sessionId,omitempty"`
	Service          string `json:"service,omitempty"`
	DirectURL        string `json:"directUrl,omitempty"`
	RegistryAddress  string `json:"registryAddress,omitempty"`
	RegistryProtocol string `json:"registryProtocol,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	Serialization    string `json:"serialization,omitempty"`
	TimeoutMS        int    `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS int    `json:"connectTimeoutMs,omitempty"`
	Explain          bool   `json:"explain,omitempty"`
}

// TargetOutput is the structured payload for sofarpc_target. It combines
// the resolver's layered report with the probe result so a single call
// tells the agent both "what target was picked" and "is it reachable".
type TargetOutput struct {
	ProjectRoot  string               `json:"projectRoot,omitempty"`
	Target       target.Config        `json:"target"`
	Service      string               `json:"service,omitempty"`
	Layers       []target.Layer       `json:"layers,omitempty"`
	ConfigErrors []target.ConfigError `json:"configErrors,omitempty"`
	Reachability target.ProbeResult   `json:"reachability"`
	Explain      []string             `json:"explain,omitempty"`
	// Trace is a per-field record of which layer won and which lower
	// layers carried a shadowed value. Populated only when explain=true
	// — agents can branch on it without parsing Explain strings.
	Trace []target.FieldTrace `json:"trace,omitempty"`
}

func registerTarget(server *sdkmcp.Server, opts Options) {
	sources := opts.TargetSources
	sessions := opts.Sessions
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_target",
		Title:       "Resolve SOFARPC Target",
		Description: "Resolve the invocation target without executing a request. Returns the merged target, the config layers that produced it, and a reachability probe.",
		Annotations: networkReadOnlyAnnotations("Resolve SOFARPC Target"),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in TargetInput) (*sdkmcp.CallToolResult, TargetOutput, error) {
		notifyToolProgress(ctx, req, 0, 2, "resolving target scope")
		scope, err := resolveToolScope(sources, sessions, in.SessionID, in.Cwd, in.Project)
		if err != nil {
			out := TargetOutput{}
			return toolResult(out, err.Error(), true), out, nil
		}
		toolSources := scope.Sources
		input := target.Input{
			Service:          in.Service,
			DirectURL:        in.DirectURL,
			RegistryAddress:  in.RegistryAddress,
			RegistryProtocol: in.RegistryProtocol,
			Protocol:         in.Protocol,
			Serialization:    in.Serialization,
			TimeoutMS:        in.TimeoutMS,
			ConnectTimeoutMS: in.ConnectTimeoutMS,
			Explain:          in.Explain,
		}
		notifyToolProgress(ctx, req, 1, 2, "probing target")
		report := target.Resolve(input, toolSources)
		probe := target.Probe(report.Target)

		out := TargetOutput{
			ProjectRoot:  scope.ProjectRoot,
			Target:       report.Target,
			Service:      report.Service,
			Layers:       report.Layers,
			ConfigErrors: report.ConfigErrors,
			Reachability: probe,
			Explain:      report.Explain,
			Trace:        report.Trace,
		}

		result := toolResult(out, summarizeTarget(out), false)
		if report.Target.Mode == "" || len(report.ConfigErrors) > 0 {
			result.IsError = true
		}
		notifyToolProgress(ctx, req, 2, 2, "target resolved")
		return result, out, nil
	})
}

// summarizeTarget gives agents a terse one-line text rendering that
// complements the structured payload. When resolution fails, the text
// points at the next tool the agent should call.
func summarizeTarget(out TargetOutput) string {
	if len(out.ConfigErrors) > 0 {
		return "target config error: " + formatConfigErrors(out.ConfigErrors)
	}
	if out.Target.Mode == "" {
		return "target mode unresolved — call sofarpc_doctor or provide directUrl/registryAddress"
	}
	addr := out.Target.DirectURL
	if out.Target.Mode == target.ModeRegistry {
		addr = out.Target.RegistryAddress
	}
	reach := "unknown"
	if out.Reachability.Reachable {
		reach = "reachable"
	} else if out.Reachability.Message != "" {
		reach = "unreachable: " + out.Reachability.Message
	}
	return fmt.Sprintf("mode=%s target=%s %s", out.Target.Mode, addr, reach)
}
