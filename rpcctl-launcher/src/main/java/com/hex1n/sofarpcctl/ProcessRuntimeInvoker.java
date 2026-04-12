package com.hex1n.sofarpcctl;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Base64;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class ProcessRuntimeInvoker {

    private static final int INLINE_REQUEST_LIMIT = 8192;
    private static final String RESULT_MARKER = "RPCCTL_RESULT_JSON:";
    private final RuntimeLocator runtimeLocator = new RuntimeLocator();
    private static final Pattern PROVIDER_ADDRESS_PATTERN = Pattern.compile(
        "(?:Try connect to|Create connection to)\\s+([^\\s!]+)"
    );

    public RuntimeInvocationResult invoke(
        String sofaRpcVersion,
        RuntimeInvocationRequest request,
        RuntimeAccessOptions accessOptions
    ) {
        File runtimeJar = runtimeLocator.requireRuntimeJar(sofaRpcVersion, accessOptions);
        if ("1".equals(System.getenv("RPCCTL_DEBUG_RUNTIME"))) {
            System.err.println("rpcctl runtime: " + runtimeJar.getAbsolutePath());
        }
        String encodedRequest = encodeRequest(request);
        InvocationCommand invocationCommand = buildJavaCommand(runtimeJar, request, encodedRequest);

        Process process;
        try {
            process = new ProcessBuilder(invocationCommand.command).start();
        } catch (Exception exception) {
            invocationCommand.cleanup();
            throw new CliException(
                ExitCodes.RPC_ERROR,
                "Failed to start SOFARPC runtime for version " + sofaRpcVersion,
                exception
            );
        }

        RuntimeProcessOutput output = waitFor(process, request);
        if (output.getStdout().trim().isEmpty()) {
            invocationCommand.cleanup();
            throw new CliException(
                output.getExitCode() == 0 ? ExitCodes.RPC_ERROR : output.getExitCode(),
                normalizeNoResponseMessage(output.getStderr())
            );
        }

        try {
            RuntimeInvocationResult result = ConfigLoader.json().readValue(
                extractJsonPayload(output.getStdout()),
                RuntimeInvocationResult.class
            );
            String resolvedTarget = extractProviderAddress(output.getStderr());
            if (resolvedTarget != null && !resolvedTarget.trim().isEmpty()) {
                result.setResolvedTarget(resolvedTarget);
            }
            enrichFailure(result, output.getStderr());
            return result;
        } catch (Exception exception) {
            throw new CliException(
                ExitCodes.RPC_ERROR,
                "Failed to parse SOFARPC runtime response.",
                exception
            );
        } finally {
            invocationCommand.cleanup();
        }
    }

    private String encodeRequest(RuntimeInvocationRequest request) {
        try {
            byte[] bytes = ConfigLoader.json().writeValueAsBytes(request);
            return Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);
        } catch (Exception exception) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Failed to encode runtime invocation request.",
                exception
            );
        }
    }

    private String resolveJavaBinary() {
        return new File(System.getProperty("java.home"), "bin/java").getAbsolutePath();
    }

    private InvocationCommand buildJavaCommand(File runtimeJar, RuntimeInvocationRequest request, String encodedRequest) {
        if (encodedRequest != null && encodedRequest.length() > INLINE_REQUEST_LIMIT) {
            return buildInvocationCommandWithRequestFile(runtimeJar, request, encodedRequest);
        }
        return buildInvocationCommandByArgument(runtimeJar, request, encodedRequest);
    }

    private InvocationCommand buildInvocationCommandByArgument(File runtimeJar, RuntimeInvocationRequest request, String encodedRequest) {
        List<String> command = new ArrayList<String>();
        command.add(resolveJavaBinary());
        command.add("-Dorg.slf4j.simpleLogger.defaultLogLevel=error");
        if (request.getStubPaths() != null && !request.getStubPaths().isEmpty()) {
            command.add("-cp");
            command.add(buildClassPath(runtimeJar, request.getStubPaths()));
            command.add("com.hex1n.sofarpcctl.RuntimeMain");
        } else {
            command.add("-jar");
            command.add(runtimeJar.getAbsolutePath());
        }
        command.add("invoke");
        command.add(encodedRequest);
        return new InvocationCommand(command, null);
    }

    private InvocationCommand buildInvocationCommandWithRequestFile(
        File runtimeJar,
        RuntimeInvocationRequest request,
        String encodedRequest
    ) {
        File requestFile;
        try {
            requestFile = File.createTempFile("rpcctl-runtime-request-", ".txt");
            requestFile.deleteOnExit();
            java.nio.file.Files.write(
                requestFile.toPath(),
                encodedRequest.getBytes(StandardCharsets.UTF_8)
            );
        } catch (IOException exception) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Failed to prepare runtime request file.",
                exception
            );
        }

        List<String> command = new ArrayList<String>();
        command.add(resolveJavaBinary());
        command.add("-Dorg.slf4j.simpleLogger.defaultLogLevel=error");
        if (request.getStubPaths() != null && !request.getStubPaths().isEmpty()) {
            command.add("-cp");
            command.add(buildClassPath(runtimeJar, request.getStubPaths()));
            command.add("com.hex1n.sofarpcctl.RuntimeMain");
        } else {
            command.add("-jar");
            command.add(runtimeJar.getAbsolutePath());
        }
        command.add("invoke");
        command.add("--request-file");
        command.add(requestFile.getAbsolutePath());
        return new InvocationCommand(command, requestFile);
    }

    private String buildClassPath(File runtimeJar, List<String> stubPaths) {
        StringBuilder classPath = new StringBuilder(runtimeJar.getAbsolutePath());
        for (String stubPath : stubPaths) {
            if (stubPath == null || stubPath.trim().isEmpty()) {
                continue;
            }
            classPath.append(File.pathSeparator).append(stubPath.trim());
        }
        return classPath.toString();
    }

    private RuntimeProcessOutput waitFor(Process process, RuntimeInvocationRequest request) {
        StreamCollector stdoutCollector = new StreamCollector(process.getInputStream());
        StreamCollector stderrCollector = new StreamCollector(process.getErrorStream());
        stdoutCollector.start();
        stderrCollector.start();
        try {
            long timeoutMs = resolveProcessTimeoutMs(request);
            if (!process.waitFor(timeoutMs, TimeUnit.MILLISECONDS)) {
                process.destroyForcibly();
                throw new CliException(
                    ExitCodes.RPC_ERROR,
                    "SOFARPC runtime timed out after " + timeoutMs + "ms."
                );
            }
            stdoutCollector.join();
            stderrCollector.join();
            return new RuntimeProcessOutput(
                process.exitValue(),
                stdoutCollector.output(),
                stderrCollector.output()
            );
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            throw new CliException(
                ExitCodes.RPC_ERROR,
                "Interrupted while waiting for SOFARPC runtime.",
                exception
            );
        }
    }

    private long resolveProcessTimeoutMs(RuntimeInvocationRequest request) {
        Integer configuredTimeout = request.getEnvironmentConfig() == null ? null : request.getEnvironmentConfig().getTimeoutMs();
        long base = configuredTimeout == null ? 3000L : configuredTimeout.longValue();
        return Math.max(base + 3000L, 5000L);
    }

    private String extractJsonPayload(String stdout) {
        String markedPayload = extractPayloadFromMarker(stdout);
        if (markedPayload != null && !markedPayload.isEmpty()) {
            return markedPayload;
        }

        String trimmed = stdout.trim();
        if (trimmed.startsWith("{") && trimmed.endsWith("}")) {
            return trimmed;
        }

        int start = trimmed.indexOf('{');
        int end = trimmed.lastIndexOf('}');
        if (start >= 0 && end > start) {
            return trimmed.substring(start, end + 1);
        }
        return trimmed;
    }

    private String extractPayloadFromMarker(String stdout) {
        if (stdout == null || stdout.isEmpty()) {
            return null;
        }
        int index = stdout.indexOf(RESULT_MARKER);
        if (index < 0) {
            return null;
        }
        int start = index + RESULT_MARKER.length();
        int lineEnd = stdout.indexOf('\n', start);
        String afterMarker = (lineEnd < 0) ? stdout.substring(start) : stdout.substring(start, lineEnd);
        String sameLinePayload = afterMarker.trim();
        if (sameLinePayload.startsWith("{") && sameLinePayload.endsWith("}")) {
            return sameLinePayload;
        }

        String candidate = stdout.substring(start).trim();
        return extractBalancedJson(candidate);
    }

    private String extractBalancedJson(String content) {
        int start = content.indexOf('{');
        if (start < 0) {
            return content.isEmpty() ? null : content;
        }
        int depth = 0;
        boolean inString = false;
        boolean escaped = false;
        for (int i = start; i < content.length(); i++) {
            char ch = content.charAt(i);
            if (escaped) {
                escaped = false;
                continue;
            }
            if (ch == '\\') {
                escaped = true;
                continue;
            }
            if (ch == '\"') {
                inString = !inString;
                continue;
            }
            if (inString) {
                continue;
            }
            if (ch == '{') {
                depth++;
            } else if (ch == '}') {
                depth--;
                if (depth == 0) {
                    return content.substring(start, i + 1);
                }
            }
        }
        return content.substring(start);
    }

    private void enrichFailure(RuntimeInvocationResult result, String stderr) {
        if (result == null || result.isSuccess() || stderr == null || stderr.trim().isEmpty()) {
            return;
        }
        String compact = compact(stderr);
        result.setDetails(compact);
        String combined = ((result.getMessage() == null ? "" : result.getMessage()) + " " + compact).toLowerCase();
        String providerAddress = extractProviderAddress(stderr);
        if (providerAddress != null && (result.getResolvedTarget() == null || result.getResolvedTarget().trim().isEmpty())) {
            result.setResolvedTarget(providerAddress);
        }
        if ((result.getErrorCode() == null || "RPC_ERROR".equals(result.getErrorCode()))
            && combined.contains("deserialization")) {
            result.setErrorCode("RPC_DESERIALIZATION_ERROR");
        } else if ((result.getErrorCode() == null || "RPC_ROUTE_ERROR".equals(result.getErrorCode()))
            && combined.contains("没有获得服务")) {
            if (combined.contains("add provider of") || providerAddress != null) {
                result.setErrorCode("RPC_PROVIDER_UNREACHABLE");
            } else {
                result.setErrorCode("RPC_PROVIDER_NOT_FOUND");
            }
        } else if ((result.getErrorCode() == null || "RPC_ERROR".equals(result.getErrorCode()))
            && (combined.contains("create connection") || combined.contains("connection refused"))) {
            result.setErrorCode("RPC_PROVIDER_UNREACHABLE");
        } else if ((result.getErrorCode() == null || "RPC_ERROR".equals(result.getErrorCode()))
            && (combined.contains("未找到需要调用的方法") || combined.contains("no such method"))) {
            result.setErrorCode("RPC_METHOD_NOT_FOUND");
        }

        if ("RPC_PROVIDER_UNREACHABLE".equals(result.getErrorCode())) {
            if (providerAddress != null) {
                result.setHint("Registry returned provider " + providerAddress
                    + " but it was unreachable. Check provider virtualHost/virtualPort and network reachability.");
            } else {
                result.setHint("A provider address was discovered but could not be reached. Check provider virtualHost/virtualPort and network reachability.");
            }
            if (result.getResolvedTarget() == null || result.getResolvedTarget().trim().isEmpty()) {
                result.setResolvedTarget("registry://" + result.getUniqueId() + "/providers");
            }
            result.setTransportHint("Check provider registration address and whether target instance is accessible from this client.");
        } else if ("RPC_PROVIDER_NOT_FOUND".equals(result.getErrorCode())) {
            result.setHint("No provider was discovered. Check registry address, service name, uniqueId, version, and whether the service is exported.");
            result.setTransportHint("The registry query returned zero providers.");
        } else if (compact.contains("Unable to open socket")) {
            result.setHint("Check the registry/provider address and whether your machine can reach it.");
            result.setTransportHint("Socket-level connect error when opening provider channel.");
        } else if (combined.contains("timeout")) {
            result.setHint("Check network reachability, uniqueId, and timeout settings.");
            result.setTransportHint("The invocation timed out before completing a successful transport-level response.");
        } else if (combined.contains("classnotfound")) {
            result.setHint("The selected runtime version may not match the provider stack.");
        }
    }

    private String normalizeNoResponseMessage(String stderr) {
        if (stderr == null || stderr.trim().isEmpty()) {
            return "SOFARPC runtime produced no JSON response.";
        }
        return "SOFARPC runtime failed before producing JSON. " + compact(stderr);
    }

    private String compact(String raw) {
        String normalized = raw.replace('\n', ' ').replace('\r', ' ').trim();
        return normalized.length() > 500 ? normalized.substring(0, 500) + "..." : normalized;
    }

    private String extractProviderAddress(String stderr) {
        Matcher matcher = PROVIDER_ADDRESS_PATTERN.matcher(stderr);
        if (matcher.find()) {
            return matcher.group(1);
        }
        return null;
    }

    static final class StreamCollector extends Thread {
        private final InputStream inputStream;
        private final ByteArrayOutputStream outputStream = new ByteArrayOutputStream();

        StreamCollector(InputStream inputStream) {
            this.inputStream = inputStream;
            setDaemon(true);
        }

        @Override
        public void run() {
            try {
                byte[] buffer = new byte[4096];
                int read;
                while ((read = inputStream.read(buffer)) >= 0) {
                    outputStream.write(buffer, 0, read);
                }
            } catch (Exception ignored) {
            }
        }

        String output() {
            return new String(outputStream.toByteArray(), StandardCharsets.UTF_8);
        }
    }

    static final class InvocationCommand {
        private final List<String> command;
        private final File requestFile;

        InvocationCommand(List<String> command, File requestFile) {
            this.command = command;
            this.requestFile = requestFile;
        }

        void cleanup() {
            if (requestFile != null && requestFile.isFile()) {
                requestFile.delete();
            }
        }
    }
}
