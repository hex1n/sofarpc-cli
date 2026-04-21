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
// understand whether the answer came from attached contract guidance.
type DescribeDiagnostics struct {
	ContractSource string         `json:"contractSource,omitempty"`
	CacheHit       bool           `json:"cacheHit,omitempty"`
	Contract       ContractBanner `json:"contract,omitempty"`
}

func registerDescribe(server *sdkmcp.Server, opts Options, holder *facadeHolder) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_describe",
		Description: "Describe a service method: resolve overloads, list param/return types, and return a JSON skeleton when contract information is available.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in DescribeInput) (*sdkmcp.CallToolResult, DescribeOutput, error) {
		facade := holder.Get()
		if facade == nil {
			out := DescribeOutput{
				Service: in.Service,
				Method:  in.Method,
				Error:   facadeNotConfiguredError(),
			}
			return errorResult(out), out, nil
		}

		result, err := contract.ResolveMethod(facade, in.Service, in.Method, in.Types)
		if err != nil {
			out := DescribeOutput{Service: in.Service, Method: in.Method, Error: asErrcodeError(err)}
			return errorResult(out), out, nil
		}

		skeleton := contract.BuildSkeleton(result.Method.ParamTypes, facade)
		contractBanner := buildContractBanner(facade)
		out := DescribeOutput{
			Service:   in.Service,
			Method:    in.Method,
			Overloads: result.Overloads,
			Selected:  result.Selected,
			Skeleton:  decodedSkeleton(skeleton),
			Diagnostics: DescribeDiagnostics{
				ContractSource: contractBanner.Source,
				Contract:       contractBanner,
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

func facadeNotConfiguredError() *errcode.Error {
	err := errcode.New(errcode.FacadeNotConfigured, "describe",
		"contract information is not available for this workspace")
	return err.WithHint("sofarpc_doctor", nil,
		"doctor reports whether this workspace can describe methods or must use trusted-mode invoke")
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
