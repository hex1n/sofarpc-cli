package com.hex1n.sofarpcctl;

final class EnvironmentTargetResolver {

    ResolvedEnvironment resolve(
        LoadedContext loadedContext,
        String environmentName,
        String directUrl,
        String registryAddress,
        String registryProtocol,
        String protocol,
        String serialization,
        Integer timeoutMs,
        boolean allowNoTarget
    ) {
        boolean hasInlineDirect = directUrl != null && !directUrl.trim().isEmpty();
        boolean hasInlineRegistry = registryAddress != null && !registryAddress.trim().isEmpty();
        boolean hasInlineTarget = hasInlineDirect || hasInlineRegistry;

        if (environmentName != null && !environmentName.trim().isEmpty()) {
            if (hasInlineTarget) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Use either --env or inline target flags, not both."
                );
            }
            return new ResolvedEnvironment(
                loadedContext.getConfig().requireEnv(environmentName.trim()),
                "explicit-env",
                environmentName.trim()
            );
        }

        if (!hasInlineTarget) {
            String contextEnv = loadedContext.getContextEntry() == null ? null : loadedContext.getContextEntry().getEnv();
            if (contextEnv != null && !contextEnv.trim().isEmpty()) {
                return new ResolvedEnvironment(
                    loadedContext.getConfig().requireEnv(contextEnv.trim()),
                    "context-env",
                    contextEnv.trim()
                );
            }

            String defaultEnv = loadedContext.getConfig().getDefaultEnv();
            if (defaultEnv != null && !defaultEnv.trim().isEmpty()) {
                return new ResolvedEnvironment(
                    loadedContext.getConfig().requireEnv(defaultEnv.trim()),
                    "default-env",
                    defaultEnv.trim()
                );
            }

            RpcCtlConfig.EnvironmentConfig contextTarget = resolveContextTarget(loadedContext);
            if (contextTarget != null) {
                String source = "direct".equals(contextTarget.getMode()) ? "context-direct" : "context-registry";
                return new ResolvedEnvironment(contextTarget, source, null);
            }

            if (allowNoTarget) {
                return new ResolvedEnvironment(null, "none", null);
            }
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Missing target. Use --env, rely on defaultEnv from manifest/config/context, or pass --direct-url / --registry inline."
            );
        }

        if (hasInlineDirect && hasInlineRegistry) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Use either --direct-url or --registry, not both."
            );
        }

        RpcCtlConfig.EnvironmentConfig environmentConfig = new RpcCtlConfig.EnvironmentConfig();
        environmentConfig.setProtocol(protocol);
        environmentConfig.setSerialization(serialization);
        environmentConfig.setTimeoutMs(timeoutMs);
        if (hasInlineDirect) {
            environmentConfig.setMode("direct");
            environmentConfig.setDirectUrl(directUrl.trim());
            return new ResolvedEnvironment(
                loadedContext.getConfig().applyDefaults(environmentConfig),
                "inline-direct",
                null
            );
        }

        environmentConfig.setMode("registry");
        environmentConfig.setRegistryAddress(registryAddress.trim());
        if (registryProtocol != null && !registryProtocol.trim().isEmpty()) {
            environmentConfig.setRegistryProtocol(registryProtocol.trim());
        }
        return new ResolvedEnvironment(
            loadedContext.getConfig().applyDefaults(environmentConfig),
            "inline-registry",
            null
        );
    }

    private RpcCtlConfig.EnvironmentConfig resolveContextTarget(LoadedContext loadedContext) {
        ContextCatalog.ContextEntry contextEntry = loadedContext.getContextEntry();
        if (contextEntry == null || contextEntry.isEmpty()) {
            return null;
        }
        if (contextEntry.getDirectUrl() != null && !contextEntry.getDirectUrl().trim().isEmpty()) {
            RpcCtlConfig.EnvironmentConfig environmentConfig = new RpcCtlConfig.EnvironmentConfig();
            environmentConfig.setMode("direct");
            environmentConfig.setDirectUrl(contextEntry.getDirectUrl());
            environmentConfig.setProtocol(contextEntry.getProtocol());
            environmentConfig.setSerialization(contextEntry.getSerialization());
            environmentConfig.setTimeoutMs(contextEntry.getTimeoutMs());
            environmentConfig.setSofaRpcVersion(contextEntry.getSofaRpcVersion());
            return loadedContext.getConfig().applyDefaults(environmentConfig);
        }
        if (contextEntry.getRegistryAddress() != null && !contextEntry.getRegistryAddress().trim().isEmpty()) {
            RpcCtlConfig.EnvironmentConfig environmentConfig = new RpcCtlConfig.EnvironmentConfig();
            environmentConfig.setMode("registry");
            environmentConfig.setRegistryAddress(contextEntry.getRegistryAddress());
            environmentConfig.setRegistryProtocol(contextEntry.getRegistryProtocol());
            environmentConfig.setProtocol(contextEntry.getProtocol());
            environmentConfig.setSerialization(contextEntry.getSerialization());
            environmentConfig.setTimeoutMs(contextEntry.getTimeoutMs());
            environmentConfig.setSofaRpcVersion(contextEntry.getSofaRpcVersion());
            return loadedContext.getConfig().applyDefaults(environmentConfig);
        }
        return null;
    }
}
