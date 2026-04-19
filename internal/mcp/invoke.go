package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/worker"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// InvokeOutput is the structured payload for sofarpc_invoke. Ok=true
// means the invocation (dry-run or real) produced a usable outcome —
// either a Plan to inspect or a Result from the worker.
type InvokeOutput struct {
	Ok          bool           `json:"ok"`
	Plan        *invoke.Plan   `json:"plan,omitempty"`
	Result      any            `json:"result,omitempty"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
	Error       *errcode.Error `json:"error,omitempty"`
}

func registerInvoke(server *sdkmcp.Server, opts Options, holder *facadeHolder) {
	sources := opts.TargetSources
	sessions := opts.Sessions
	client := opts.Worker
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_invoke",
		Description: "Plan and execute a SOFARPC generic invocation. dryRun=true returns the plan without contacting the worker.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in InvokeInput) (*sdkmcp.CallToolResult, InvokeOutput, error) {
		facade := holder.Get()
		args, err := normalizeArgs(in.Args)
		if err != nil {
			return errorInvokeResult(err), InvokeOutput{Error: asErrcodeError(err)}, nil
		}

		plan, err := invoke.BuildPlan(invoke.Input{
			Service:    in.Service,
			Method:     in.Method,
			ParamTypes: in.Types,
			Args:       args,
			Target: target.Input{
				Service:          in.Service,
				DirectURL:        in.DirectURL,
				RegistryAddress:  in.RegistryAddress,
				RegistryProtocol: in.RegistryProtocol,
				TimeoutMS:        in.TimeoutMS,
			},
		}, facade, sources)
		if err != nil {
			out := InvokeOutput{Error: asErrcodeError(err)}
			return errorInvokeResult(err), out, nil
		}

		if sessions != nil && in.SessionID != "" {
			sessions.UpdatePlan(in.SessionID, plan)
		}

		if in.DryRun {
			out := InvokeOutput{Ok: true, Plan: &plan}
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{
					&sdkmcp.TextContent{Text: summarizeInvokePlan(plan, true)},
				},
			}, out, nil
		}

		resp, werr := client.Invoke(ctx, planToWireRequest(plan))
		if werr != nil {
			out := InvokeOutput{Plan: &plan, Error: asErrcodeError(werr)}
			return errorInvokeResult(werr), out, nil
		}
		out := InvokeOutput{
			Ok:          true,
			Plan:        &plan,
			Result:      resp.Result,
			Diagnostics: resp.Diagnostics,
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: summarizeInvokePlan(plan, false)},
			},
		}, out, nil
	})
}

// planToWireRequest converts an invoke.Plan into the worker wire shape.
// Classloader is intentionally left nil — the runtime worker treats a
// missing classloader as "use default", and we don't yet surface stub
// jar paths from the indexer.
func planToWireRequest(plan invoke.Plan) worker.Request {
	tgt := plan.Target
	return worker.Request{
		Action:     worker.ActionInvoke,
		Service:    plan.Service,
		Method:     plan.Method,
		ParamTypes: plan.ParamTypes,
		Args:       plan.Args,
		Target:     &tgt,
	}
}

// normalizeArgs coerces the loosely-typed Args field into []any:
//   - nil                 → nil (plan renders a skeleton).
//   - []any               → pass through verbatim.
//   - "@<path>" string    → read the file, parse as a JSON array.
//   - anything else       → input.args-invalid.
//
// Relative paths are resolved against the MCP server process's CWD. The
// file must contain a JSON array; non-array content is rejected so the
// failure shape matches inline args.
func normalizeArgs(raw any) ([]any, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case []any:
		return v, nil
	case string:
		if !strings.HasPrefix(v, "@") {
			return nil, errcode.New(errcode.ArgsInvalid, "invoke",
				fmt.Sprintf("args string must start with '@' to reference a file, got %q", v)).
				WithHint("sofarpc_describe", nil, "send a JSON array inline or use @<path>")
		}
		path := strings.TrimPrefix(v, "@")
		if path == "" {
			return nil, errcode.New(errcode.ArgsInvalid, "invoke",
				"args '@' prefix requires a file path").
				WithHint("sofarpc_describe", nil, "use @<absolute-or-relative-path>")
		}
		return readArgsFile(path)
	default:
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("args must be a JSON array or '@<path>' string, got %T", raw)).
			WithHint("sofarpc_describe", nil, "see the method's paramTypes")
	}
}

// readArgsFile loads path and decodes it as a JSON array. Errors are
// wrapped into input.args-invalid so the agent sees one shape regardless
// of whether args came inline or from disk.
func readArgsFile(path string) ([]any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("read args file %q: %v", path, err)).
			WithHint("sofarpc_doctor", nil, "check the path and that the mcp process can read it")
	}
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("parse args file %q as JSON: %v", path, err)).
			WithHint("sofarpc_describe", nil, "the file must contain a JSON array matching paramTypes")
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("args file %q must contain a JSON array, got %T", path, parsed)).
			WithHint("sofarpc_describe", nil, "wrap the value in [] so it matches paramTypes")
	}
	return list, nil
}

func errorInvokeResult(err error) *sdkmcp.CallToolResult {
	text := "invoke failed"
	var ecerr *errcode.Error
	if errors.As(err, &ecerr) {
		text = fmt.Sprintf("%s: %s", ecerr.Code, ecerr.Message)
	} else if err != nil {
		text = err.Error()
	}
	return &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
	}
}

func summarizeInvokePlan(plan invoke.Plan, dryRun bool) string {
	prefix := "plan"
	if dryRun {
		prefix = "dry-run plan"
	}
	return fmt.Sprintf("%s: %s.%s target=%s overload=%d/%d argSource=%s",
		prefix, plan.Service, plan.Method, targetAddr(plan.Target),
		plan.Selected+1, len(plan.Overloads), plan.ArgSource)
}

func targetAddr(cfg target.Config) string {
	if cfg.DirectURL != "" {
		return cfg.DirectURL
	}
	if cfg.RegistryAddress != "" {
		return cfg.RegistryAddress
	}
	return string(cfg.Mode)
}
