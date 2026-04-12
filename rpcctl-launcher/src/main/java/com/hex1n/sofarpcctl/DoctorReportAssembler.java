package com.hex1n.sofarpcctl;

import java.io.File;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

final class DoctorReportAssembler {

    ContextCatalog.ResolvedContext collectContextSection(
        DoctorReport report,
        ContextCatalog catalog,
        String requestedContextName
    ) {
        Map<String, Object> contextPayload = new LinkedHashMap<String, Object>();
        contextPayload.put("contextsPath", ConfigLoader.resolveDefaultContextsPath());
        contextPayload.put("currentContext", catalog.getCurrentContext());
        contextPayload.put("requestedContext", requestedContextName);
        contextPayload.put("contextCount", catalog.getContexts() == null ? 0 : catalog.getContexts().size());

        ContextCatalog.ResolvedContext resolvedContext;
        try {
            resolvedContext = catalog.resolveSelected(requestedContextName);
            contextPayload.put("selectedContext", resolvedContext.getName());
            contextPayload.put("contextEntry", resolvedContext.getEntry());
            if (resolvedContext.getName() == null) {
                report.warn(
                    "context",
                    "No active context selected. Doctor will rely on explicit flags, project files, or defaults.",
                    null
                );
            } else {
                report.ok("context", "Using context " + resolvedContext.getName() + ".", null);
            }
        } catch (CliException exception) {
            contextPayload.put("selectedContext", null);
            report.error("context", exception.getMessage(), null);
            report.putSection("context", contextPayload);
            return null;
        }
        report.putSection("context", contextPayload);
        return resolvedContext;
    }

    void collectDiscoverySection(DoctorReport report, ContextLoadResolution contextLoad) {
        Map<String, Object> discoveryPayload = new LinkedHashMap<String, Object>();
        discoveryPayload.put("source", contextLoad.getSource());
        discoveryPayload.put("configPath", contextLoad.getLoadedContext().getConfigPath());
        discoveryPayload.put("metadataPath", contextLoad.getLoadedContext().getMetadataPath());
        discoveryPayload.put("manifestPath", contextLoad.getLoadedContext().getManifestPath());
        discoveryPayload.put("defaultEnv", contextLoad.getLoadedContext().getConfig().getDefaultEnv());
        discoveryPayload.put(
            "serviceCount",
            contextLoad.getLoadedContext().getMetadata().getServices() == null
                ? 0
                : contextLoad.getLoadedContext().getMetadata().getServices().size()
        );
        report.putSection("discovery", discoveryPayload);
        if ("empty-defaults".equals(contextLoad.getSource())) {
            report.warn("discovery", "No manifest or config was discovered. Doctor is using empty defaults.", discoveryPayload);
        } else {
            report.ok("discovery", "Loaded invocation context from " + contextLoad.getSource() + ".", discoveryPayload);
        }
    }

    void collectStubPathSection(DoctorReport report, StubPathResolution resolvedStubPaths) {
        Map<String, Object> stubPayload = new LinkedHashMap<String, Object>();
        List<String> existingStubPaths = new ArrayList<String>();
        List<String> missingStubPaths = new ArrayList<String>();
        for (String stubPath : resolvedStubPaths.getPaths()) {
            if (new File(stubPath).exists()) {
                existingStubPaths.add(stubPath);
            } else {
                missingStubPaths.add(stubPath);
            }
        }
        stubPayload.put("source", resolvedStubPaths.getSource());
        stubPayload.put("paths", resolvedStubPaths.getPaths());
        stubPayload.put("existingPaths", existingStubPaths);
        stubPayload.put("missingPaths", missingStubPaths);
        report.putSection("stubPaths", stubPayload);
        if (resolvedStubPaths.getPaths().isEmpty()) {
            report.warn("stub-paths", "No stub paths are configured.", stubPayload);
        } else if (!missingStubPaths.isEmpty()) {
            report.warn("stub-paths", "Some configured stub paths are missing.", stubPayload);
        } else {
            report.ok("stub-paths", "All configured stub paths exist.", stubPayload);
        }
    }

    void collectJavaSection(DoctorReport report) {
        File javaBinary = new JavaBinaryResolver().resolve();
        Map<String, Object> javaPayload = new LinkedHashMap<String, Object>();
        javaPayload.put("javaHome", System.getProperty("java.home"));
        javaPayload.put("javaVersion", System.getProperty("java.version"));
        javaPayload.put("javaBinary", javaBinary.getAbsolutePath());
        javaPayload.put("exists", javaBinary.isFile());
        report.putSection("java", javaPayload);
        if (javaBinary.isFile()) {
            report.ok(
                "java",
                "Using launcher JVM " + System.getProperty("java.version") + " at " + javaBinary.getAbsolutePath() + ".",
                javaPayload
            );
        } else {
            report.error("java", "Launcher Java binary is missing: " + javaBinary.getAbsolutePath(), javaPayload);
        }
    }

    void collectEnvironmentSection(DoctorReport report, ResolvedEnvironment resolvedEnvironment) {
        Map<String, Object> environmentPayload = new LinkedHashMap<String, Object>();
        environmentPayload.put("source", resolvedEnvironment.getSource());
        environmentPayload.put("envName", resolvedEnvironment.getEnvName());
        environmentPayload.put("environment", toEnvironmentPayload(resolvedEnvironment.getEnvironmentConfig()));
        report.putSection("environment", environmentPayload);
        if (resolvedEnvironment.getEnvironmentConfig() == null) {
            report.warn("environment", "No target environment could be resolved.", environmentPayload);
            return;
        }
        String targetSummary = "direct".equals(resolvedEnvironment.getEnvironmentConfig().getMode())
            ? resolvedEnvironment.getEnvironmentConfig().getDirectUrl()
            : resolvedEnvironment.getEnvironmentConfig().getRegistryAddress();
        report.ok(
            "environment",
            "Resolved " + resolvedEnvironment.getEnvironmentConfig().getMode() + " target from "
                + resolvedEnvironment.getSource() + ": " + targetSummary,
            environmentPayload
        );
    }

    void collectVersionSection(DoctorReport report, VersionDetector.VersionResolution versionResolution) {
        Map<String, Object> versionPayload = new LinkedHashMap<String, Object>();
        versionPayload.put("resolvedVersion", versionResolution.getResolvedVersion());
        versionPayload.put("source", versionResolution.getSource());
        versionPayload.put("fallbackUsed", versionResolution.isFallbackUsed());
        versionPayload.put("declaredSupported", versionResolution.isDeclaredSupported());
        versionPayload.put("supportedVersions", versionResolution.getSupportedVersions());
        report.putSection("version", versionPayload);
        if (versionResolution.isFallbackUsed()) {
            report.warn(
                "version",
                "Version detection fell back to " + versionResolution.getResolvedVersion() + ".",
                versionPayload
            );
        } else if (!versionResolution.isDeclaredSupported()) {
            report.warn(
                "version",
                "Resolved SOFARPC version " + versionResolution.getResolvedVersion()
                    + " from " + versionResolution.getSource()
                    + ", but the declared support matrix only includes: "
                    + joinSupportedVersions(versionResolution.getSupportedVersions()),
                versionPayload
            );
        } else {
            report.ok(
                "version",
                "Resolved SOFARPC version " + versionResolution.getResolvedVersion() + " from " + versionResolution.getSource() + ".",
                versionPayload
            );
        }
    }

    private String joinSupportedVersions(List<String> versions) {
        if (versions == null || versions.isEmpty()) {
            return "";
        }
        StringBuilder builder = new StringBuilder();
        for (int i = 0; i < versions.size(); i++) {
            if (i > 0) {
                builder.append(", ");
            }
            builder.append(versions.get(i));
        }
        return builder.toString();
    }

    void collectRuntimeSection(
        DoctorReport report,
        VersionDetector.VersionResolution versionResolution,
        RuntimeLocator.RuntimeResolutionProbe runtimeProbe,
        RuntimeAccessOptions runtimeAccessOptions
    ) {
        Map<String, Object> runtimePayload = new LinkedHashMap<String, Object>();
        runtimePayload.put("resolvedPath", runtimeProbe.getResolvedPath());
        runtimePayload.put("fileName", runtimeProbe.getFileName());
        runtimePayload.put("candidates", runtimeProbe.getCandidates());
        runtimePayload.put("autoDownloadEnabled", runtimeProbe.isAutoDownloadEnabled());
        runtimePayload.put("runtimeBaseUrl", runtimeProbe.getRuntimeBaseUrl());
        runtimePayload.put("runtimeHome", runtimeAccessOptions.getRuntimeHome());
        runtimePayload.put("runtimeCacheDir", runtimeAccessOptions.getRuntimeCacheDir());
        report.putSection("runtime", runtimePayload);
        if (runtimeProbe.isResolved()) {
            report.ok("runtime", "Resolved runtime jar at " + runtimeProbe.getResolvedPath() + ".", runtimePayload);
        } else if (runtimeProbe.isAutoDownloadEnabled()) {
            report.warn(
                "runtime",
                "No local runtime jar was found for " + versionResolution.getResolvedVersion()
                    + ", but auto-download is enabled.",
                runtimePayload
            );
        } else {
            report.error(
                "runtime",
                "No local runtime jar was found for " + versionResolution.getResolvedVersion()
                    + " and auto-download is disabled.",
                runtimePayload
            );
        }
    }

    void collectNetworkSection(
        DoctorReport report,
        NetworkProbe networkProbe,
        ResolvedEnvironment resolvedEnvironment,
        int probeTimeoutMs
    ) {
        Map<String, Object> networkPayload = new LinkedHashMap<String, Object>();
        report.putSection("network", networkPayload);
        if (resolvedEnvironment.getEnvironmentConfig() == null) {
            report.warn("network", "Skipping reachability probe because no target environment was resolved.", null);
            return;
        }

        RpcCtlConfig.EnvironmentConfig environmentConfig = resolvedEnvironment.getEnvironmentConfig();
        try {
            NetworkProbe.ProbeSummary networkSummary;
            if ("direct".equals(environmentConfig.getMode())) {
                networkSummary = networkProbe.probe(environmentConfig.getDirectUrl(), "bolt", probeTimeoutMs);
            } else {
                networkSummary = networkProbe.probe(
                    environmentConfig.getRegistryAddress(),
                    StringValueResolver.firstNonBlank(environmentConfig.getRegistryProtocol(), "zookeeper"),
                    probeTimeoutMs
                );
            }
            networkPayload.put("mode", environmentConfig.getMode());
            networkPayload.put("target", networkSummary.getTarget());
            networkPayload.put("probeTimeoutMs", probeTimeoutMs);
            networkPayload.put("reachable", networkSummary.isReachable());
            networkPayload.put("endpoints", networkSummary.getEndpoints());
            if (networkSummary.isReachable()) {
                report.ok(
                    "network",
                    "At least one " + environmentConfig.getMode() + " endpoint is reachable.",
                    networkPayload
                );
            } else {
                report.error(
                    "network",
                    "No " + environmentConfig.getMode() + " endpoint was reachable.",
                    networkPayload
                );
            }
        } catch (CliException exception) {
            report.error("network", exception.getMessage(), networkPayload);
        }
    }

    private Map<String, Object> toEnvironmentPayload(RpcCtlConfig.EnvironmentConfig environmentConfig) {
        if (environmentConfig == null) {
            return null;
        }
        Map<String, Object> payload = new LinkedHashMap<String, Object>();
        payload.put("mode", environmentConfig.getMode());
        payload.put("protocol", environmentConfig.getProtocol());
        payload.put("serialization", environmentConfig.getSerialization());
        payload.put("registryProtocol", environmentConfig.getRegistryProtocol());
        payload.put("registryAddress", environmentConfig.getRegistryAddress());
        payload.put("directUrl", environmentConfig.getDirectUrl());
        payload.put("timeoutMs", environmentConfig.getTimeoutMs());
        payload.put("uniqueId", environmentConfig.getUniqueId());
        payload.put("sofaRpcVersion", environmentConfig.getSofaRpcVersion());
        return payload;
    }
}
