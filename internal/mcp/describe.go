package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
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

// DescribeOutput is the structured payload for sofarpc_describe. For a full
// service+method describe, Overloads + Selected + Skeleton are populated.
// When method is omitted, Methods lists the service's callable methods; when
// service is also omitted, Services lists the method-bearing interfaces in
// the contract index. On failure, Error is set and the CallToolResult
// reports IsError=true.
type DescribeOutput struct {
	Service     string              `json:"service,omitempty"`
	Method      string              `json:"method,omitempty"`
	Overloads   []javamodel.Method  `json:"overloads,omitempty"`
	Selected    int                 `json:"selected,omitempty"`
	Skeleton    []any               `json:"skeleton,omitempty"`
	Methods     []javamodel.Method  `json:"methods,omitempty"`
	Services    []string            `json:"services,omitempty"`
	Diagnostics DescribeDiagnostics `json:"diagnostics,omitempty"`
	Error       *errcode.Error      `json:"error,omitempty"`
}

// DescribeDiagnostics surfaces contract-source metadata so agents can
// understand whether the answer came from attached contract guidance.
type DescribeDiagnostics struct {
	ContractSource string         `json:"contractSource,omitempty"`
	CacheHit       bool           `json:"cacheHit,omitempty"`
	Contract       ContractBanner `json:"contract,omitempty"`
}

func registerDescribe(server *sdkmcp.Server, opts Options, holder *contractHolder) {
	sources := opts.TargetSources
	sessions := opts.Sessions
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_describe",
		Title:       "Describe SOFARPC Method",
		Description: "Describe a service method: resolve overloads, list param/return types, and return a JSON skeleton when contract information is available. Omit method to list the service's callable methods; omit service and method to list known services.",
		Annotations: localReadOnlyAnnotations("Describe SOFARPC Method"),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in DescribeInput) (*sdkmcp.CallToolResult, DescribeOutput, error) {
		notifyToolProgress(ctx, req, 0, 3, "loading contract context")
		toolCtx, err := resolveToolContext(sources, sessions, holder, in.SessionID, in.Cwd, in.Project)
		if err != nil {
			out := DescribeOutput{Service: in.Service, Method: in.Method, Error: errcode.New(errcode.ArgsInvalid, "describe", err.Error())}
			return errorResult(out), out, nil
		}
		store := toolCtx.Contract.store
		if store == nil {
			out := DescribeOutput{
				Service: in.Service,
				Method:  in.Method,
				Error:   contractNotConfiguredError(),
			}
			return errorResult(out), out, nil
		}

		if strings.TrimSpace(in.Method) == "" {
			out := describeListing(in, store, toolCtx.Contract)
			if out.Error != nil {
				return errorResult(out), out, nil
			}
			notifyToolProgress(ctx, req, 3, 3, "listing complete")
			return toolResult(out, summarizeDescribeListing(out), false), out, nil
		}

		notifyToolProgress(ctx, req, 1, 3, "resolving method")
		result, err := contract.ResolveMethod(store, in.Service, in.Method, in.Types)
		if err != nil {
			out := DescribeOutput{Service: in.Service, Method: in.Method, Error: asErrcodeError(err)}
			return errorResult(out), out, nil
		}

		notifyToolProgress(ctx, req, 2, 3, "building argument skeleton")
		skeleton := contract.BuildSkeleton(result.Method.ParamTypes, store)
		contractBanner := buildContractBannerForSnapshot(toolCtx.Contract)
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
		notifyToolProgress(ctx, req, 3, 3, "method described")
		return toolResult(out, summarizeDescribe(out), false), out, nil
	})
}

// describeListing handles the two method-less describe modes: with a service
// it lists that service's callable methods, without one it lists the
// method-bearing interfaces known to the contract index.
func describeListing(in DescribeInput, store contract.Store, snapshot contractSnapshot) DescribeOutput {
	banner := buildContractBannerForSnapshot(snapshot)
	diagnostics := DescribeDiagnostics{
		ContractSource: banner.Source,
		Contract:       banner,
	}
	if strings.TrimSpace(in.Service) == "" {
		discovery := contract.DiscoverServiceInterfaces(store, contract.ServiceDiscoveryOptions{
			Suffixes: []string{"*"},
		})
		return DescribeOutput{
			Services:    discovery.CandidateServices,
			Diagnostics: diagnostics,
		}
	}
	cls, methods, err := contract.ListMethods(store, in.Service)
	if err != nil {
		return DescribeOutput{Service: in.Service, Error: asErrcodeError(err)}
	}
	return DescribeOutput{
		Service:     cls.FQN,
		Methods:     methods,
		Diagnostics: diagnostics,
	}
}

func summarizeDescribeListing(out DescribeOutput) string {
	if out.Service != "" {
		return fmt.Sprintf("%s: %d methods", out.Service, len(out.Methods))
	}
	return fmt.Sprintf("%d services in contract index", len(out.Services))
}

func errorResult(out DescribeOutput) *sdkmcp.CallToolResult {
	text := "describe failed"
	if out.Error != nil {
		text = fmt.Sprintf("%s: %s", out.Error.Code, out.Error.Message)
	}
	return toolResult(out, text, true)
}

func contractNotConfiguredError() *errcode.Error {
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
