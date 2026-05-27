package mcp

import sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

func localReadOnlyAnnotations(title string) *sdkmcp.ToolAnnotations {
	return &sdkmcp.ToolAnnotations{
		Title:         title,
		ReadOnlyHint:  true,
		OpenWorldHint: boolRef(false),
	}
}

func networkReadOnlyAnnotations(title string) *sdkmcp.ToolAnnotations {
	return &sdkmcp.ToolAnnotations{
		Title:         title,
		ReadOnlyHint:  true,
		OpenWorldHint: boolRef(true),
	}
}

func localWriteAnnotations(title string) *sdkmcp.ToolAnnotations {
	return &sdkmcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: boolRef(true),
		OpenWorldHint:   boolRef(false),
	}
}

func remoteInvokeAnnotations(title string) *sdkmcp.ToolAnnotations {
	return &sdkmcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: boolRef(true),
		OpenWorldHint:   boolRef(true),
	}
}

func boolRef(value bool) *bool {
	return &value
}
