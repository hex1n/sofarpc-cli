package com.hex1n.sofarpc.worker;

import com.fasterxml.jackson.databind.ObjectMapper;

import java.io.BufferedReader;
import java.io.BufferedWriter;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStreamWriter;
import java.lang.management.ManagementFactory;
import java.lang.reflect.Method;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.time.Instant;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

public final class WorkerMain {
    private static final String RUNTIME_VERSION = "5.7.6";
    private static final String PROTOCOL_VERSION = "v1";
    private final ObjectMapper mapper = new ObjectMapper();
    private final SofaInvokeService invokeService = new SofaInvokeService(mapper);

    public static void main(String[] args) throws Exception {
        new WorkerMain().run(args);
    }

    private void run(String[] args) throws Exception {
        if (args.length == 0) {
            throw new IllegalArgumentException("subcommand required");
        }
        if ("serve".equalsIgnoreCase(args[0])) {
            Map<String, String> options = parseOptions(args, 1);
            serve(options);
            return;
        }
        if ("describe".equalsIgnoreCase(args[0])) {
            Map<String, String> options = parseOptions(args, 1);
            describe(options);
            return;
        }
        throw new IllegalArgumentException("unsupported subcommand: " + args[0]);
    }

    private void describe(Map<String, String> options) throws Exception {
        String serviceName = require(options, "--service");
        Class<?> clazz = Class.forName(serviceName, false, Thread.currentThread().getContextClassLoader());
        ServiceSchema schema = new ServiceSchema();
        schema.service = serviceName;
        for (Method method : clazz.getMethods()) {
            if (method.getDeclaringClass() == Object.class) {
                continue;
            }
            MethodSchema ms = new MethodSchema();
            ms.name = method.getName();
            for (Class<?> paramType : method.getParameterTypes()) {
                ms.paramTypes.add(paramType.getName());
            }
            ms.returnType = method.getReturnType().getName();
            schema.methods.add(ms);
        }
        System.out.println(mapper.writeValueAsString(schema));
    }

    private void serve(Map<String, String> options) throws Exception {
        String listen = require(options, "--listen");
        String metadataFile = require(options, "--metadata-file");
        String[] hostPort = splitHostPort(listen);
        ServerSocket server = new ServerSocket();
        server.bind(new InetSocketAddress(InetAddress.getByName(hostPort[0]), Integer.parseInt(hostPort[1])));
        writeMetadata(server, Paths.get(metadataFile));
        ExecutorService executor = Executors.newCachedThreadPool();
        ExecutorService invokeExecutor = Executors.newCachedThreadPool();
        Runtime.getRuntime().addShutdownHook(new Thread(new Runnable() {
            @Override
            public void run() {
                invokeService.close();
                try {
                    server.close();
                } catch (IOException ignored) {
                }
                executor.shutdownNow();
                invokeExecutor.shutdownNow();
            }
        }));
        while (!server.isClosed()) {
            Socket socket = server.accept();
            executor.submit(new Runnable() {
                @Override
                public void run() {
                    handle(socket, invokeExecutor);
                }
            });
        }
    }

    private void handle(Socket socket, ExecutorService invokeExecutor) {
        try {
            socket.setSoTimeout(30000);
            BufferedReader reader = new BufferedReader(new InputStreamReader(socket.getInputStream(), StandardCharsets.UTF_8));
            BufferedWriter writer = new BufferedWriter(new OutputStreamWriter(socket.getOutputStream(), StandardCharsets.UTF_8));
            String line = reader.readLine();
            if (line == null) {
                socket.close();
                return;
            }
            InvokeRequest request = mapper.readValue(line, InvokeRequest.class);
            InvokeResponse response = invokeWithBudget(request, invokeExecutor);
            writer.write(mapper.writeValueAsString(response));
            writer.newLine();
            writer.flush();
            socket.close();
        } catch (Exception ex) {
            ex.printStackTrace(System.err);
            try {
                socket.close();
            } catch (IOException ignored) {
            }
        }
    }

    private void writeMetadata(ServerSocket server, Path metadataFile) throws IOException {
        DaemonMetadata metadata = new DaemonMetadata();
        metadata.pid = currentPid();
        metadata.host = server.getInetAddress().getHostAddress();
        metadata.port = server.getLocalPort();
        metadata.startedAt = Instant.now().toString();
        metadata.runtimeVersion = RUNTIME_VERSION;
        metadata.protocolVersion = PROTOCOL_VERSION;
        Files.createDirectories(metadataFile.getParent());
        mapper.writerWithDefaultPrettyPrinter().writeValue(metadataFile.toFile(), metadata);
    }

    private int currentPid() {
        String runtimeName = ManagementFactory.getRuntimeMXBean().getName();
        int separator = runtimeName.indexOf('@');
        if (separator < 0) {
            return -1;
        }
        return Integer.parseInt(runtimeName.substring(0, separator));
    }

    private Map<String, String> parseOptions(String[] args, int startIndex) {
        Map<String, String> options = new HashMap<String, String>();
        for (int i = startIndex; i < args.length; i += 2) {
            if (i + 1 >= args.length) {
                throw new IllegalArgumentException("missing value for " + args[i]);
            }
            options.put(args[i], args[i + 1]);
        }
        return options;
    }

    private String require(Map<String, String> options, String name) {
        String value = options.get(name);
        if (value == null || value.trim().isEmpty()) {
            throw new IllegalArgumentException(name + " is required");
        }
        return value;
    }

    private String[] splitHostPort(String value) {
        int separator = value.lastIndexOf(':');
        if (separator < 0) {
            throw new IllegalArgumentException("listen address must be host:port");
        }
        return new String[]{value.substring(0, separator), value.substring(separator + 1)};
    }

    private InvokeResponse invokeWithBudget(InvokeRequest request, ExecutorService invokeExecutor) {
        Future<InvokeResponse> future = invokeExecutor.submit(new java.util.concurrent.Callable<InvokeResponse>() {
            @Override
            public InvokeResponse call() {
                return invokeService.invoke(request);
            }
        });
        try {
            return future.get(invokeBudgetMs(request), TimeUnit.MILLISECONDS);
        } catch (TimeoutException ex) {
            future.cancel(true);
            return timeoutResponse(request);
        } catch (InterruptedException ex) {
            Thread.currentThread().interrupt();
            future.cancel(true);
            return interruptedResponse(request);
        } catch (ExecutionException ex) {
            future.cancel(true);
            return internalFailure(request, ex.getCause() != null ? ex.getCause() : ex);
        }
    }

    private long invokeBudgetMs(InvokeRequest request) {
        int invokeMs = 3000;
        int connectMs = 1000;
        if (request != null && request.target != null) {
            if (request.target.timeoutMs > 0) {
                invokeMs = request.target.timeoutMs;
            }
            if (request.target.connectTimeoutMs > 0) {
                connectMs = request.target.connectTimeoutMs;
            }
        }
        return invokeMs + connectMs + 2000L;
    }

    private InvokeResponse timeoutResponse(InvokeRequest request) {
        DiagnosticInfo diagnostics = diagnostics(request);
        RuntimeError error = new RuntimeError();
        error.code = "TARGET_TIMEOUT";
        error.message = "Timed out while initializing provider lookup or waiting for invoke completion.";
        error.phase = "discover";
        error.targetMode = diagnostics.targetMode;
        error.configuredTarget = diagnostics.configuredTarget;
        error.resolvedTarget = diagnostics.resolvedTarget;
        error.invokeStyle = diagnostics.invokeStyle;
        error.payloadMode = diagnostics.payloadMode;
        error.retriable = true;
        error.hint = "Check registry session health, provider availability, and timeout settings.";
        return InvokeResponse.failure(request.requestId, error, diagnostics);
    }

    private InvokeResponse interruptedResponse(InvokeRequest request) {
        DiagnosticInfo diagnostics = diagnostics(request);
        RuntimeError error = new RuntimeError();
        error.code = "WORKER_INTERRUPTED";
        error.message = "Worker thread was interrupted while handling the request.";
        error.phase = "invoke";
        error.targetMode = diagnostics.targetMode;
        error.configuredTarget = diagnostics.configuredTarget;
        error.resolvedTarget = diagnostics.resolvedTarget;
        error.invokeStyle = diagnostics.invokeStyle;
        error.payloadMode = diagnostics.payloadMode;
        error.hint = "Retry the request. If it repeats, inspect worker lifecycle logs.";
        return InvokeResponse.failure(request.requestId, error, diagnostics);
    }

    private InvokeResponse internalFailure(InvokeRequest request, Throwable throwable) {
        DiagnosticInfo diagnostics = diagnostics(request);
        RuntimeError error = new RuntimeError();
        error.code = "INTERNAL_ERROR";
        error.message = throwable != null ? throwable.getMessage() : "worker execution failed";
        error.phase = "invoke";
        error.targetMode = diagnostics.targetMode;
        error.configuredTarget = diagnostics.configuredTarget;
        error.resolvedTarget = diagnostics.resolvedTarget;
        error.invokeStyle = diagnostics.invokeStyle;
        error.payloadMode = diagnostics.payloadMode;
        error.hint = "Inspect worker stderr for stack traces.";
        return InvokeResponse.failure(request.requestId, error, diagnostics);
    }

    private DiagnosticInfo diagnostics(InvokeRequest request) {
        DiagnosticInfo diagnostics = new DiagnosticInfo();
        diagnostics.phase = "invoke";
        diagnostics.targetMode = request != null && request.target != null ? request.target.mode : null;
        diagnostics.configuredTarget = configuredTarget(request);
        diagnostics.resolvedTarget = diagnostics.configuredTarget;
        diagnostics.invokeStyle = "generic".equalsIgnoreCase(request.payloadMode) ? "$genericInvoke" : "$invoke";
        diagnostics.payloadMode = request.payloadMode;
        return diagnostics;
    }

    private String configuredTarget(InvokeRequest request) {
        if (request == null || request.target == null) {
            return "";
        }
        if ("direct".equalsIgnoreCase(request.target.mode)) {
            return request.target.directUrl;
        }
        if ("registry".equalsIgnoreCase(request.target.mode)) {
            return defaultValue(request.target.registryProtocol, "zookeeper") + "://" + defaultValue(request.target.registryAddress, "");
        }
        return "";
    }

    private String defaultValue(String value, String fallback) {
        return value == null || value.trim().isEmpty() ? fallback : value;
    }
}
