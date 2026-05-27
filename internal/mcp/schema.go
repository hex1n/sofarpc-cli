package mcp

func invokeInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"cwd":              stringSchema("Optional current working directory used to resolve project-relative paths."),
		"project":          stringSchema("Optional project root. When set, project-scoped target config and contract information are loaded for this root."),
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
		"trusted":          booleanSchema("When true, force trusted mode and require service, method, types, and args from the caller."),
		"contractMode":     stringSchema("Contract behavior: auto (default), strict, or trusted."),
		"sessionId":        stringSchema("Optional session id used for project/session-scoped contract loading and plan capture."),
	})
}

func replayInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"cwd":       stringSchema("Optional current working directory used as replay safety context when payload is supplied."),
		"project":   stringSchema("Optional project root used as replay safety context when payload is supplied."),
		"sessionId": stringSchema("Replay this session's captured plan, or when payload is supplied, use this session as project/safety context."),
		"payload":   objectPropertySchema("Plan payload returned by sofarpc_invoke dryRun. May be combined with sessionId, cwd, or project for safety context."),
		"dryRun":    booleanSchema("When true, validate and summarize the replay plan without executing it."),
	})
}

func initProjectInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"cwd":                 stringSchema("Optional current working directory used to resolve the project root. When omitted with project/sessionId, init_project attempts safe Java project auto-discovery."),
		"project":             stringSchema("Optional project root to initialize. Preferred for write operations when the caller already knows the workspace root."),
		"sessionId":           stringSchema("Optional session id whose project root should be initialized."),
		"config":              stringSchema("Project config file to write: local (default) or shared."),
		"force":               booleanSchema("When true, overwrite an existing .sofarpc config file."),
		"dryRun":              booleanSchema("When true, preview the config and gitignore changes without writing files."),
		"services":            stringArraySchema("Explicit service allowlist. When omitted, facade services are discovered from source contracts."),
		"allowAllServices":    booleanSchema("When true, intentionally write allowedServices=[\"*\"] instead of discovering or passing a narrower allowlist."),
		"serviceNameSuffixes": stringArraySchema("Simple-name suffixes used for service discovery. Defaults to Facade. Use * to include all method-bearing interfaces."),
		"directUrl":           stringSchema("Optional direct BOLT URL to persist, for example bolt://host:12200."),
		"registryAddress":     stringSchema("Optional registry address to persist."),
		"registryProtocol":    stringSchema("Optional registry protocol to persist."),
		"protocol":            stringSchema("Optional wire protocol to persist."),
		"serialization":       stringSchema("Optional wire serialization to persist."),
		"uniqueId":            stringSchema("Optional SOFA service uniqueId to persist."),
		"timeoutMs":           integerSchema("Optional request timeout in milliseconds."),
		"connectTimeoutMs":    integerSchema("Optional connect timeout in milliseconds."),
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
