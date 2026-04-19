package cli

import (
	"context"

	"github.com/hex1n/sofarpc-cli/internal/adapters/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpOpenWorkspaceSessionInput = mcpserver.OpenWorkspaceSessionInput
type mcpInspectSessionInput = mcpserver.InspectSessionInput
type mcpResumeContextInput = mcpserver.ResumeContextInput
type mcpResolveTargetInput = mcpserver.ResolveTargetInput
type mcpDescribeMethodInput = mcpserver.DescribeMethodInput
type mcpPlanInvocationInput = mcpserver.PlanInvocationInput
type mcpInvokeRPCInput = mcpserver.InvokeRPCInput
type mcpListFacadeServicesInput = mcpserver.ListFacadeServicesInput
type mcpMethodOverload = mcpserver.MethodOverload
type mcpDescribeMethodOutput = mcpserver.DescribeMethodOutput
type mcpInvokeRPCOutput = mcpserver.InvokeRPCOutput
type mcpPlanInvocationOutput = mcpserver.PlanInvocationOutput
type mcpFacadeService = mcpserver.FacadeService
type mcpListFacadeServicesOutput = mcpserver.ListFacadeServicesOutput
type mcpResumeContextOutput = mcpserver.ResumeContextOutput

func (a *App) MCPServer() *mcp.Server {
	return mcpserver.New(a)
}

func (a *App) RunMCP(ctx context.Context) error {
	return mcpserver.Run(ctx, a)
}
