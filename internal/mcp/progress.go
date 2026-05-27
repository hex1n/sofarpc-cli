package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const toolLogLogger = "sofarpc-mcp"

func notifyToolProgress(ctx context.Context, req *sdkmcp.CallToolRequest, progress, total float64, message string) {
	notifyToolLog(ctx, req, "info", "tool.progress", map[string]any{
		"message":  message,
		"progress": progress,
		"total":    total,
	})
	if req == nil || req.Params == nil || req.Session == nil {
		return
	}
	token := req.Params.GetProgressToken()
	if token == nil {
		return
	}
	_ = req.Session.NotifyProgress(ctx, &sdkmcp.ProgressNotificationParams{
		ProgressToken: token,
		Progress:      progress,
		Total:         total,
		Message:       message,
	})
}

func notifyToolLog(ctx context.Context, req *sdkmcp.CallToolRequest, level sdkmcp.LoggingLevel, event string, data map[string]any) {
	if req == nil || req.Params == nil || req.Session == nil {
		return
	}
	payload := map[string]any{
		"event": event,
		"tool":  req.Params.Name,
	}
	for key, value := range data {
		payload[key] = value
	}
	_ = req.Session.Log(ctx, &sdkmcp.LoggingMessageParams{
		Logger: toolLogLogger,
		Level:  level,
		Data:   payload,
	})
}
