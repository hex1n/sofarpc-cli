package com.hex1n.sofarpcctl;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

final class InvocationResultAnnotator {

    String classifyLauncherError(CliException exception) {
        if (exception.getExitCode() == ExitCodes.PARAMETER_ERROR) {
            return "RUNTIME_SETUP_ERROR";
        }
        if (exception.getExitCode() == ExitCodes.POLICY_DENIED) {
            return "POLICY_DENIED";
        }
        return "RUNTIME_ERROR";
    }

    void annotateInvocationResult(
        RuntimeInvocationResult result,
        String paramTypeSource,
        List<String> paramTypes
    ) {
        if (result == null) {
            return;
        }
        result.setParamTypeSource(paramTypeSource);
        if (result.getInvokeStyle() == null || result.getInvokeStyle().trim().isEmpty()) {
            result.setInvokeStyle(result.isGenericCall() ? "$genericInvoke" : "$invoke");
        }
        result.setPayloadMode(resolvePayloadMode(paramTypeSource, paramTypes));
    }

    void annotateLauncherFailure(
        RuntimeInvocationResult result,
        CliException exception,
        String launcherErrorCode
    ) {
        if (result == null) {
            return;
        }
        if ("RUNTIME_SETUP_ERROR".equals(launcherErrorCode)) {
            result.setErrorPhase("setup");
            result.setRetriable(Boolean.FALSE);
        } else if ("POLICY_DENIED".equals(launcherErrorCode)) {
            result.setErrorPhase("policy");
            result.setRetriable(Boolean.FALSE);
        } else {
            result.setErrorPhase("runtime");
            result.setRetriable(null);
        }
        Map<String, String> diagnostics = new LinkedHashMap<String, String>();
        diagnostics.put("launcherException", exception.getClass().getName());
        if (exception.getCause() != null) {
            diagnostics.put("launcherCause", exception.getCause().getClass().getName());
        }
        result.setDiagnostics(diagnostics);
    }

    void annotateVersionResolution(
        RuntimeInvocationResult result,
        VersionDetector.VersionResolution versionResolution
    ) {
        if (result == null || versionResolution == null) {
            return;
        }
        Map<String, String> diagnostics = result.getDiagnostics();
        if (diagnostics == null) {
            diagnostics = new LinkedHashMap<String, String>();
            result.setDiagnostics(diagnostics);
        }
        diagnostics.put("resolvedSofaRpcVersion", versionResolution.getResolvedVersion());
        diagnostics.put("sofaRpcVersionSource", versionResolution.getSource());
        diagnostics.put("sofaRpcVersionDeclaredSupported", String.valueOf(versionResolution.isDeclaredSupported()));
        if (versionResolution.isFallbackUsed()) {
            diagnostics.put("sofaRpcVersionFallback", "true");
        }
        if (versionResolution.isFallbackUsed() || !versionResolution.isDeclaredSupported()) {
            diagnostics.put("supportedSofaRpcVersions", joinValues(versionResolution.getSupportedVersions()));
        }
    }

    void applyFailureHints(RuntimeInvocationResult result, CliException exception) {
        if (result == null || exception == null) {
            return;
        }
        String message = exception.getMessage() == null ? "" : exception.getMessage();
        if (message.contains("No SOFARPC runtime found")) {
            String resolvedVersion = result.getDiagnostics() == null ? null : result.getDiagnostics().get("resolvedSofaRpcVersion");
            String versionSource = result.getDiagnostics() == null ? null : result.getDiagnostics().get("sofaRpcVersionSource");
            String fallback = result.getDiagnostics() == null ? null : result.getDiagnostics().get("sofaRpcVersionFallback");
            String declaredSupported = result.getDiagnostics() == null ? null : result.getDiagnostics().get("sofaRpcVersionDeclaredSupported");
            String supportedVersions = result.getDiagnostics() == null ? null : result.getDiagnostics().get("supportedSofaRpcVersions");
            if ("true".equals(fallback)) {
                result.setHint("Version detection fell back to " + resolvedVersion
                    + " because no workspace version could be inferred. Pass --sofa-rpc-version explicitly, configure it in context/config, or install the matching runtime."
                );
            } else if ("false".equals(declaredSupported) && supportedVersions != null && !supportedVersions.trim().isEmpty()) {
                result.setHint("Resolved SOFARPC version " + resolvedVersion + " from " + versionSource
                    + ", but the declared support matrix only includes: " + supportedVersions
                    + ". Build/add the matching runtime or pass a supported version explicitly."
                );
            } else {
                result.setHint("Set --sofa-rpc-version, configure runtimeBaseUrl via context, or build/install the matching runtime.");
            }
        } else if (message.toLowerCase().contains("timed out")) {
            result.setHint("Check registry/provider reachability, uniqueId, and timeout settings.");
        } else if (message.contains("Missing target")) {
            result.setHint("Use a context/defaultEnv, or pass --direct-url / --registry inline.");
        }
    }

    void applyInvocationFailureHints(
        RuntimeInvocationResult result,
        String paramTypeSource,
        String rawTypes,
        List<String> resolvedStubPaths
    ) {
        if (result == null || result.isSuccess()) {
            return;
        }
        String errorCode = result.getErrorCode() == null ? "" : result.getErrorCode();
        if ("RPC_METHOD_NOT_FOUND".equals(errorCode)
            && "cli".equals(paramTypeSource)
            && containsConcreteCollectionTypes(TypeNameUtils.parseTypes(rawTypes))) {
            result.setHint("Use interface-declared parameter types such as java.util.Map / java.util.List instead of concrete implementations.");
            return;
        }
        if ("RPC_DESERIALIZATION_ERROR".equals(errorCode)
            && ("schema".equals(result.getPayloadMode()) || "generic".equals(result.getPayloadMode()))) {
            if (resolvedStubPaths != null && !resolvedStubPaths.isEmpty()) {
                result.setHint("Stub-aware DTO deserialization still failed. Check the stub classpath, DTO field names, and the selected SOFARPC version.");
            } else {
                result.setHint(
                    "Complex DTO payloads should use schema/stub mode or GenericObject-compatible payloads. "
                        + "Add --stub-path for local business classes, or prefer raw Map/List mode when no DTO schema is available."
                );
            }
            return;
        }
        if ("none".equals(paramTypeSource)
            && result.getMessage() != null
            && result.getMessage().toLowerCase().contains("parameter count")) {
            result.setHint("Provide --types explicitly, or add metadata/manifest so rpcctl can resolve the method signature.");
        }
    }

    private boolean containsConcreteCollectionTypes(List<String> rawParamTypes) {
        for (String rawParamType : rawParamTypes) {
            String normalized = TypeNameUtils.normalizeForRpc(rawParamType);
            if (isConcreteCollectionType(normalized)) {
                return true;
            }
        }
        return false;
    }

    private boolean isConcreteCollectionType(String normalizedType) {
        switch (normalizedType) {
            case "java.util.ArrayList":
            case "java.util.LinkedList":
            case "java.util.HashSet":
            case "java.util.LinkedHashSet":
            case "java.util.HashMap":
            case "java.util.LinkedHashMap":
            case "java.util.TreeMap":
                return true;
            default:
                return false;
        }
    }

    private String resolvePayloadMode(String paramTypeSource, List<String> types) {
        if ("metadata".equals(paramTypeSource)) {
            return "schema";
        }
        if (types == null) {
            return "raw";
        }
        if (types.isEmpty()) {
            return "raw";
        }
        for (String type : types) {
            if (!TypeNameUtils.isRawFriendlyType(type)) {
                return "generic";
            }
        }
        return "raw";
    }

    private String joinValues(List<String> values) {
        if (values == null || values.isEmpty()) {
            return "";
        }
        StringBuilder builder = new StringBuilder();
        for (int i = 0; i < values.size(); i++) {
            if (i > 0) {
                builder.append(", ");
            }
            builder.append(values.get(i));
        }
        return builder.toString();
    }
}
