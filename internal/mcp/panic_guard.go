package mcp

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func addTypedTool[In, Out any](server *sdkmcp.Server, tool *sdkmcp.Tool, phase string, panicOutput func(*errcode.Error) Out, handler sdkmcp.ToolHandlerFor[In, Out]) {
	sdkmcp.AddTool(server, tool, recoverTypedTool(tool.Name, phase, panicOutput, handler))
}

func recoverTypedTool[In, Out any](toolName, phase string, panicOutput func(*errcode.Error) Out, handler sdkmcp.ToolHandlerFor[In, Out]) sdkmcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest, in In) (result *sdkmcp.CallToolResult, output Out, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				ecerr := toolPanicError(toolName, phase, recovered)
				if panicOutput != nil {
					output = panicOutput(ecerr)
				}
				result = toolResult(output, panicToolText(phase, ecerr), true)
				err = nil
			}
		}()
		return handler(ctx, req, in)
	}
}

func addRawTool(server *sdkmcp.Server, tool *sdkmcp.Tool, phase string, panicResult func(*errcode.Error) *sdkmcp.CallToolResult, handler sdkmcp.ToolHandler) {
	server.AddTool(tool, recoverRawTool(tool.Name, phase, panicResult, handler))
}

func recoverRawTool(toolName, phase string, panicResult func(*errcode.Error) *sdkmcp.CallToolResult, handler sdkmcp.ToolHandler) sdkmcp.ToolHandler {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (result *sdkmcp.CallToolResult, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				ecerr := toolPanicError(toolName, phase, recovered)
				if panicResult != nil {
					result = panicResult(ecerr)
				} else {
					result = toolResult(map[string]any{"error": ecerr}, panicToolText(phase, ecerr), true)
				}
				err = nil
			}
		}()
		return handler(ctx, req)
	}
}

func toolPanicError(toolName, phase string, recovered any) *errcode.Error {
	fmt.Fprintf(os.Stderr, "sofarpc-mcp recovered panic in tool %s: %v\n%s", toolName, recovered, debug.Stack())
	return errcode.New(errcode.InternalFailure, phase,
		fmt.Sprintf("internal MCP tool panic in %s", toolName))
}

func panicToolText(phase string, err *errcode.Error) string {
	return errorText(phase+" failed", err)
}
