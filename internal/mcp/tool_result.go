package mcp

import (
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func toolResult(out any, text string, isError bool) *sdkmcp.CallToolResult {
	return toolResultWithLinks(out, text, isError)
}

func toolResultWithLinks(out any, text string, isError bool, links ...sdkmcp.Content) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		IsError: isError,
		Content: append(contentWithJSONText(out, text), links...),
	}
}

func structuredToolResult(out any, text string, isError bool) *sdkmcp.CallToolResult {
	return structuredToolResultWithLinks(out, text, isError)
}

func structuredToolResultWithLinks(out any, text string, isError bool, links ...sdkmcp.Content) *sdkmcp.CallToolResult {
	result := toolResultWithLinks(out, text, isError, links...)
	if body, err := json.Marshal(out); err == nil {
		result.StructuredContent = json.RawMessage(body)
	}
	return result
}

func contentWithJSONText(_ any, text string) []sdkmcp.Content {
	return []sdkmcp.Content{&sdkmcp.TextContent{Text: text}}
}
