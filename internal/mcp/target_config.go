package mcp

import (
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
)

func targetHintArgs(sources target.Sources) map[string]any {
	args := map[string]any{"explain": true}
	if strings.TrimSpace(sources.ProjectRoot) != "" {
		args["project"] = sources.ProjectRoot
	}
	return args
}

func targetConfigDiagnostics(sources target.Sources) map[string]any {
	if len(sources.ConfigErrors) == 0 {
		return nil
	}
	return map[string]any{
		"projectRoot":  sources.ProjectRoot,
		"configErrors": append([]target.ConfigError(nil), sources.ConfigErrors...),
	}
}

func formatConfigErrors(errors []target.ConfigError) string {
	parts := make([]string, 0, len(errors))
	for _, item := range errors {
		path := strings.TrimSpace(item.Path)
		msg := strings.TrimSpace(item.Error)
		switch {
		case path != "" && msg != "":
			parts = append(parts, path+": "+msg)
		case path != "":
			parts = append(parts, path)
		case msg != "":
			parts = append(parts, msg)
		}
	}
	return strings.Join(parts, "; ")
}
