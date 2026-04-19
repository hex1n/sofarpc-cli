package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// decodedSkeleton converts contract.BuildSkeleton's []json.RawMessage
// into []any so the MCP SDK's schema inference emits a heterogeneous
// items type rather than a byte array.
func decodedSkeleton(raw []json.RawMessage) []any {
	out := make([]any, len(raw))
	for i, r := range raw {
		var v any
		if err := json.Unmarshal(r, &v); err != nil {
			v = string(r)
		}
		out[i] = v
	}
	return out
}

// DescribeOutput is the structured payload for sofarpc_describe. On
// success, Overloads + Selected + Skeleton are populated; on failure,
// Error is set and the CallToolResult reports IsError=true.
type DescribeOutput struct {
	Service     string                  `json:"service,omitempty"`
	Method      string                  `json:"method,omitempty"`
	Overloads   []facadesemantic.Method `json:"overloads,omitempty"`
	Selected    int                     `json:"selected,omitempty"`
	Skeleton    []any                   `json:"skeleton,omitempty"`
	Diagnostics DescribeDiagnostics     `json:"diagnostics,omitempty"`
	Error       *errcode.Error          `json:"error,omitempty"`
}

// DescribeDiagnostics surfaces contract-source metadata so agents can
// decide whether to refresh (e.g. on IndexStale).
type DescribeDiagnostics struct {
	ContractSource string `json:"contractSource,omitempty"`
	CacheHit       bool   `json:"cacheHit,omitempty"`
	// Refreshed is true when this call regenerated the facade index
	// before resolving. Agents can surface it so users see that a stale
	// answer has been replaced with a fresh one.
	Refreshed bool `json:"refreshed,omitempty"`
}

func registerDescribe(server *sdkmcp.Server, opts Options, holder *facadeHolder) {
	reindexer := opts.Reindexer
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_describe",
		Description: "Describe a service method: resolve overloads, list param/return types, and return a JSON skeleton populated from the local facade index. Pass refresh=true to regenerate the index before resolving.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in DescribeInput) (*sdkmcp.CallToolResult, DescribeOutput, error) {
		refreshed := false
		if in.Refresh {
			if reindexer == nil {
				// No indexer wired means the agent can't self-heal by
				// retrying describe — it's a config problem, not a stale
				// index. Route to doctor so the taxonomy matches reality.
				out := DescribeOutput{
					Service: in.Service,
					Method:  in.Method,
					Error: errcode.New(errcode.FacadeNotConfigured, "describe",
						"refresh requested but no reindexer is configured").
						WithHint("sofarpc_doctor", nil,
							"set SOFARPC_INDEXER_JAR and SOFARPC_PROJECT_ROOT, then reopen"),
				}
				return errorResult(out), out, nil
			}
			newStore, err := reindexer.Reindex(ctx)
			if err != nil {
				// IndexerFailed (not IndexStale): the indexer subprocess
				// itself could not produce a fresh index. Another refresh
				// won't fix it — the agent should route the human to
				// doctor instead of retrying.
				out := DescribeOutput{
					Service: in.Service,
					Method:  in.Method,
					Error: errcode.New(errcode.IndexerFailed, "describe",
						"indexer run failed: "+err.Error()).
						WithHint("sofarpc_doctor", nil,
							"inspect indexer output; source roots or jar path may be wrong"),
				}
				return errorResult(out), out, nil
			}
			holder.Set(newStore)
			refreshed = true
		}

		facade := holder.Get()
		if facade == nil {
			out := DescribeOutput{
				Service: in.Service,
				Method:  in.Method,
				Error:   facadeNotConfiguredError(in.Service, in.Method, reindexer != nil),
			}
			return errorResult(out), out, nil
		}

		result, err := contract.ResolveMethod(facade, in.Service, in.Method, in.Types)
		if err != nil {
			out := DescribeOutput{Service: in.Service, Method: in.Method, Error: asErrcodeError(err)}
			return errorResult(out), out, nil
		}

		skeleton := contract.BuildSkeleton(result.Method.ParamTypes, facade)
		out := DescribeOutput{
			Service:   in.Service,
			Method:    in.Method,
			Overloads: result.Overloads,
			Selected:  result.Selected,
			Skeleton:  decodedSkeleton(skeleton),
			Diagnostics: DescribeDiagnostics{
				ContractSource: "facade-store",
				Refreshed:      refreshed,
			},
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: summarizeDescribe(out)},
			},
		}, out, nil
	})
}

func errorResult(out DescribeOutput) *sdkmcp.CallToolResult {
	text := "describe failed"
	if out.Error != nil {
		text = fmt.Sprintf("%s: %s", out.Error.Code, out.Error.Message)
	}
	return &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
	}
}

// facadeNotConfiguredError picks the right recovery hint depending on
// whether a reindexer is wired. With one, the agent can self-heal by
// calling describe again with refresh=true; without one, only a human
// can fix the config, so we point at sofarpc_doctor.
//
// service/method are threaded in so the self-heal hint carries the
// original call's context. Without them the agent would have to
// remember and re-supply them, defeating the "follow the hint
// verbatim" contract.
func facadeNotConfiguredError(service, method string, canReindex bool) *errcode.Error {
	err := errcode.New(errcode.FacadeNotConfigured, "describe",
		"facade index is not configured")
	if canReindex {
		args := map[string]any{"refresh": true}
		if service != "" {
			args["service"] = service
		}
		if method != "" {
			args["method"] = method
		}
		return err.WithHint("sofarpc_describe", args,
			"run the indexer first, then describe again")
	}
	return err.WithHint("sofarpc_doctor", nil,
		"indexer is not wired yet; see docs/architecture.md §6")
}

func asErrcodeError(err error) *errcode.Error {
	var ecerr *errcode.Error
	if errors.As(err, &ecerr) {
		return ecerr
	}
	return errcode.New(errcode.ContractUnresolvable, "describe", err.Error())
}

func summarizeDescribe(out DescribeOutput) string {
	return fmt.Sprintf("%s.%s overload %d/%d",
		out.Service, out.Method, out.Selected+1, len(out.Overloads))
}
