package mcp

func invokeInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"service":          stringSchema("Fully-qualified Java facade interface name."),
		"method":           stringSchema("Java method name."),
		"types":            stringArraySchema("Optional Java parameter type list used to disambiguate overloads."),
		"args":             argsArraySchema(),
		"version":          stringSchema("Optional SOFARPC service version."),
		"targetAppName":    stringSchema("Optional target app name hint."),
		"directUrl":        stringSchema("Optional direct BOLT URL, for example bolt://host:12200."),
		"registryAddress":  stringSchema("Optional registry address."),
		"registryProtocol": stringSchema("Optional registry protocol."),
		"timeoutMs":        integerSchema("Optional invoke timeout in milliseconds."),
		"dryRun":           booleanSchema("When true, return the resolved plan without executing the request."),
		"sessionId":        stringSchema("Optional session id used to capture the invocation plan for replay."),
	})
}

func replayInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"sessionId": stringSchema("Replay the most recently captured plan for this session."),
		"payload":   objectPropertySchema("Plan payload returned by sofarpc_invoke dryRun. Mutually exclusive with sessionId."),
		"dryRun":    booleanSchema("When true, validate and summarize the replay plan without executing it."),
	})
}

func objectSchema(properties map[string]any) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": properties,
	}
}

func stringSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

func integerSchema(description string) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": description,
	}
}

func booleanSchema(description string) map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": description,
	}
}

func stringArraySchema(description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items": map[string]any{
			"type": "string",
		},
	}
}

func argsArraySchema() map[string]any {
	return map[string]any{
		"type":        "array",
		"description": "Argument vector. Provide one item per Java method parameter; single-parameter methods still use a one-item array.",
		"items": map[string]any{
			"description": "Any JSON value accepted by the target Java parameter.",
		},
	}
}

func objectPropertySchema(description string) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": description,
	}
}
