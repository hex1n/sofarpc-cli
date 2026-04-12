package com.hex1n.sofarpcctl;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.concurrent.TimeUnit;

public final class ProcessRuntimeInvoker {

    private final RuntimeLocator runtimeLocator = new RuntimeLocator();

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

        Process process;
        try {
            process = new ProcessBuilder(
                resolveJavaBinary(),
                "-jar",
                runtimeJar.getAbsolutePath(),
                "invoke",
                encodedRequest
            ).start();
        } catch (Exception exception) {
            throw new CliException(
                ExitCodes.RPC_ERROR,
                "Failed to start SOFARPC runtime for version " + sofaRpcVersion,
                exception
            );
        }

        RuntimeProcessOutput output = waitFor(process, request);
        if (output.getStdout().trim().isEmpty()) {
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
            enrichFailure(result, output.getStderr());
            return result;
        } catch (Exception exception) {
            throw new CliException(
                ExitCodes.RPC_ERROR,
                "Failed to parse SOFARPC runtime response.",
                exception
            );
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

    private void enrichFailure(RuntimeInvocationResult result, String stderr) {
        if (result == null || result.isSuccess() || stderr == null || stderr.trim().isEmpty()) {
            return;
        }
        String compact = compact(stderr);
        result.setDetails(compact);
        if (compact.contains("Unable to open socket")) {
            result.setHint("Check the registry/provider address and whether your machine can reach it.");
        } else if (compact.toLowerCase().contains("timeout")) {
            result.setHint("Check network reachability, uniqueId, and timeout settings.");
        } else if (compact.toLowerCase().contains("classnotfound")) {
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
        return normalized.length() > 300 ? normalized.substring(0, 300) + "..." : normalized;
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
}
