package mcp

import (
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
)

func targetHintArgs(sources target.Sources) map[string]any {
	return invoke.TargetHintArgs(sources)
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
	return target.FormatConfigErrors(errors)
}
