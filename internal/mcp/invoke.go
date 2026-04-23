package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// InvokeOutput is the structured payload for sofarpc_invoke. Ok=true
// means the invocation (dry-run or real) produced a usable outcome —
// either a Plan to inspect or a Result from the direct transport.
type InvokeOutput struct {
	Ok          bool           `json:"ok"`
	Plan        *invoke.Plan   `json:"plan,omitempty"`
	Result      any            `json:"result,omitempty"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
	Error       *errcode.Error `json:"error,omitempty"`
}

func registerInvoke(server *sdkmcp.Server, opts Options, holder *contractHolder) {
	sources := opts.TargetSources
	sessions := opts.Sessions
	inputSchema, err := jsonschema.For[InvokeInput](nil)
	if err != nil {
		panic(fmt.Sprintf("infer invoke input schema: %v", err))
	}
	server.AddTool(&sdkmcp.Tool{
		Name:        "sofarpc_invoke",
		Description: "Plan and execute a SOFARPC generic invocation. args accepts inline JSON or a string \"@<path>\" that references a JSON file (relative paths resolve against the MCP server's CWD). Single-parameter methods may pass the value directly; multi-parameter methods must pass a JSON array. dryRun=true returns the plan without executing the request.",
		InputSchema: inputSchema,
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		store := holder.Get()
		decoded, args, err := decodeInvokeInput(req)
		if err != nil {
			return invokeToolResult(InvokeOutput{Error: asErrcodeError(err)}, errorText("invoke failed", err), true), nil
		}
		args, err = normalizeArgs(decoded.Service, decoded.Method, args)
		if err != nil {
			return invokeToolResult(InvokeOutput{Error: asErrcodeError(err)}, errorText("invoke failed", err), true), nil
		}

		plan, err := invoke.BuildPlan(invoke.Input{
			Service:       decoded.Service,
			Method:        decoded.Method,
			ParamTypes:    decoded.Types,
			Args:          args,
			Version:       decoded.Version,
			TargetAppName: decoded.TargetAppName,
			Target: target.Input{
				Service:          decoded.Service,
				DirectURL:        decoded.DirectURL,
				RegistryAddress:  decoded.RegistryAddress,
				RegistryProtocol: decoded.RegistryProtocol,
				TimeoutMS:        decoded.TimeoutMS,
			},
		}, store, sources)
		if err != nil {
			out := InvokeOutput{Error: asErrcodeError(err)}
			return invokeToolResult(out, errorText("invoke failed", err), true), nil
		}

		if sessions != nil && decoded.SessionID != "" {
			sessions.UpdatePlan(decoded.SessionID, plan)
		}

		if decoded.DryRun {
			out := InvokeOutput{Ok: true, Plan: &plan}
			return invokeToolResult(out, summarizeInvokePlan(plan, true), false), nil
		}

		outcome, execErr := invoke.Execute(ctx, plan, "invoke")
		if execErr != nil {
			out := InvokeOutput{Plan: &plan, Diagnostics: outcome.Diagnostics, Error: asErrcodeError(execErr)}
			return invokeToolResult(out, errorText("invoke failed", execErr), true), nil
		}
		out := InvokeOutput{
			Ok:          true,
			Plan:        &plan,
			Result:      outcome.Result,
			Diagnostics: outcome.Diagnostics,
		}
		return invokeToolResult(out, summarizeInvokePlan(plan, false), false), nil
	})
}

type rawInvokeInput struct {
	Service          string          `json:"service,omitempty"`
	Method           string          `json:"method,omitempty"`
	Types            []string        `json:"types,omitempty"`
	Args             json.RawMessage `json:"args,omitempty"`
	Version          string          `json:"version,omitempty"`
	TargetAppName    string          `json:"targetAppName,omitempty"`
	DirectURL        string          `json:"directUrl,omitempty"`
	RegistryAddress  string          `json:"registryAddress,omitempty"`
	RegistryProtocol string          `json:"registryProtocol,omitempty"`
	TimeoutMS        int             `json:"timeoutMs,omitempty"`
	DryRun           bool            `json:"dryRun,omitempty"`
	SessionID        string          `json:"sessionId,omitempty"`
}

func decodeInvokeInput(req *sdkmcp.CallToolRequest) (InvokeInput, any, error) {
	if req == nil || len(req.Params.Arguments) == 0 {
		return InvokeInput{}, nil, nil
	}
	var raw rawInvokeInput
	dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return InvokeInput{}, nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("parse tool arguments: %v", err))
	}
	args, err := decodeJSONValue(raw.Args)
	if err != nil {
		return InvokeInput{}, nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("parse args as JSON: %v", err)).
			WithHint("sofarpc_describe", describeHintArgs(raw.Service, raw.Method),
				"send args as valid JSON or use @<path>")
	}
	return InvokeInput{
		Service:          raw.Service,
		Method:           raw.Method,
		Types:            raw.Types,
		Version:          raw.Version,
		TargetAppName:    raw.TargetAppName,
		DirectURL:        raw.DirectURL,
		RegistryAddress:  raw.RegistryAddress,
		RegistryProtocol: raw.RegistryProtocol,
		TimeoutMS:        raw.TimeoutMS,
		DryRun:           raw.DryRun,
		SessionID:        raw.SessionID,
	}, args, nil
}

func decodeJSONValue(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var parsed any
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// normalizeArgs normalizes the loosely-typed args field:
//   - nil              → nil (plan renders a skeleton).
//   - "@<path>" string → read the file, parse as JSON with UseNumber.
//   - anything else    → pass through verbatim for BuildPlan to shape-check.
//
// Relative paths are resolved against the MCP server process's CWD. The
// file may contain either a single JSON value or an array. Single-value
// payloads are accepted so one-arg methods do not need an extra wrapper.
//
// service/method are threaded in only to pre-fill the describe hint on
// failure — empty values are dropped so the agent never sees a hint it
// can't follow verbatim.
func normalizeArgs(service, method string, raw any) (any, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		if !strings.HasPrefix(v, "@") {
			return v, nil
		}
		path := strings.TrimPrefix(v, "@")
		if path == "" {
			return nil, errcode.New(errcode.ArgsInvalid, "invoke",
				"args '@' prefix requires a file path").
				WithHint("sofarpc_describe", describeHintArgs(service, method),
					"use @<absolute-or-relative-path>")
		}
		return readArgsFile(service, method, path)
	default:
		return raw, nil
	}
}

// readArgsFile loads path and decodes it as JSON with UseNumber. Errors are
// wrapped into input.args-invalid so the agent sees one shape regardless
// of whether args came inline or from disk.
func readArgsFile(service, method, path string) (any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("read args file %q: %v", path, err)).
				WithHint("sofarpc_doctor", nil, "check the path and that the mcp process can read it")
	}
	parsed, err := decodeJSONValue(body)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("parse args file %q as JSON: %v", path, err)).
			WithHint("sofarpc_describe", describeHintArgs(service, method),
				"the file must contain valid JSON matching paramTypes")
	}
	return parsed, nil
}

// describeHintArgs builds the NextArgs payload for a describe hint. We
// only include fields that are non-empty so the agent never receives a
// hint it can't follow verbatim (an empty required field is worse than a
// nil NextArgs — it looks runnable but isn't).
func describeHintArgs(service, method string) map[string]any {
	if service == "" && method == "" {
		return nil
	}
	args := map[string]any{}
	if service != "" {
		args["service"] = service
	}
	if method != "" {
		args["method"] = method
	}
	return args
}

func invokeToolResult(out any, text string, isError bool) *sdkmcp.CallToolResult {
	result := &sdkmcp.CallToolResult{
		IsError: isError,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
	}
	if body, err := json.Marshal(out); err == nil {
		result.StructuredContent = json.RawMessage(body)
	}
	return result
}

func errorText(prefix string, err error) string {
	text := prefix
	var ecerr *errcode.Error
	if errors.As(err, &ecerr) {
		return fmt.Sprintf("%s: %s", ecerr.Code, ecerr.Message)
	}
	if err != nil {
		return err.Error()
	}
	return text
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
