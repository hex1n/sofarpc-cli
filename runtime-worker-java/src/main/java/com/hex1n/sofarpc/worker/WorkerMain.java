package com.hex1n.sofarpc.worker;

import com.fasterxml.jackson.databind.ObjectMapper;

import java.io.BufferedReader;
import java.io.BufferedWriter;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStreamWriter;
import java.lang.management.ManagementFactory;
import java.lang.reflect.Method;
import java.lang.reflect.Type;
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
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

public final class WorkerMain {
    private static final String RUNTIME_VERSION = RuntimeMetadata.runtimeVersion();
    private static final String PROTOCOL_VERSION = "v1";
    private final ObjectMapper mapper = WorkerMappers.create();
    private final SofaInvokeService invokeService = new SofaInvokeService(mapper);
    private final Map<String, ServiceSchema> describeCache = new ConcurrentHashMap<String, ServiceSchema>();

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
        ServiceSchema schema = buildSchema(serviceName);
        System.out.println(mapper.writeValueAsString(schema));
    }

    private void serve(Map<String, String> options) throws Exception {
        String listen = require(options, "--listen");
        String metadataFile = require(options, "--metadata-file");
        String runtimeProfile = options.get("--runtime-profile");
        String runtimeDigest = options.get("--runtime-digest");
        String javaMajor = options.get("--java-major");
        String[] hostPort = splitHostPort(listen);
        ServerSocket server = new ServerSocket();
        server.bind(new InetSocketAddress(InetAddress.getByName(hostPort[0]), Integer.parseInt(hostPort[1])));
        writeMetadata(server, Paths.get(metadataFile), runtimeProfile, runtimeDigest, javaMajor);
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
            InvokeResponse response;
            if (request != null && "describe".equalsIgnoreCase(request.action)) {
                response = describeWithCache(request);
            } else {
                response = invokeWithBudget(request, invokeExecutor);
            }
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

    private void writeMetadata(ServerSocket server, Path metadataFile, String runtimeProfile, String runtimeDigest, String javaMajor) throws IOException {
        DaemonMetadata metadata = new DaemonMetadata();
        metadata.pid = currentPid();
        metadata.host = server.getInetAddress().getHostAddress();
        metadata.port = server.getLocalPort();
        metadata.startedAt = Instant.now().toString();
        metadata.runtimeVersion = RUNTIME_VERSION;
        metadata.daemonProfile = runtimeProfile;
        metadata.runtimeDigest = runtimeDigest;
        metadata.javaMajor = javaMajor;
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

    private InvokeResponse describeWithCache(InvokeRequest request) {
        String service = request != null ? request.service : null;
        if (service == null || service.trim().isEmpty()) {
            RuntimeError error = new RuntimeError();
            error.code = "DESCRIBE_INVALID_REQUEST";
            error.message = "service is required for describe requests";
            error.phase = "describe";
            error.payloadMode = defaultValue(request != null ? request.payloadMode : null, "raw");
            error.hint = "Pass --service for the legacy CLI command and `service` in the request payload for daemon action.";
            return InvokeResponse.failure(request != null ? request.requestId : null, error, describeDiagnostics(request));
        }
        String serviceName = service.trim();
        if (request != null && request.refresh) {
            describeCache.remove(serviceName);
        }
        ServiceSchema schema = describeCache.get(serviceName);
        if (schema == null) {
            try {
                schema = buildSchema(serviceName);
                describeCache.put(serviceName, schema);
            } catch (Exception ex) {
                RuntimeError error = new RuntimeError();
                error.code = ex instanceof ClassNotFoundException ? "DESCRIBE_SERVICE_NOT_FOUND" : "DESCRIBE_FAILURE";
                error.message = ex != null ? ex.getMessage() : "failed to build service schema";
                error.phase = "describe";
                error.payloadMode = defaultValue(request != null ? request.payloadMode : null, "raw");
                error.retriable = !(ex instanceof ClassNotFoundException);
                if (ex instanceof ClassNotFoundException) {
                    error.hint = "Verify that stub paths include the service interface jar and service name is correct.";
                } else {
                    error.hint = "Check worker logs; retry with --refresh if schema cache needs to be rebuilt.";
                }
                return InvokeResponse.failure(request != null ? request.requestId : null, error, describeDiagnostics(request));
            }
        }
        return InvokeResponse.success(request != null ? request.requestId : null, mapper.valueToTree(schema), describeDiagnostics(request));
    }

    private ServiceSchema buildSchema(String serviceName) throws Exception {
        Class<?> clazz = Class.forName(serviceName, false, Thread.currentThread().getContextClassLoader());
        ServiceSchema schema = new ServiceSchema();
        schema.service = serviceName;
        for (Method method : clazz.getMethods()) {
            if (method.getDeclaringClass() == Object.class) {
                continue;
            }
            MethodSchema ms = new MethodSchema();
            ms.name = method.getName();
            Type[] genericParamTypes = method.getGenericParameterTypes();
            for (Class<?> paramType : method.getParameterTypes()) {
                ms.paramTypes.add(paramType.getName());
            }
            for (Type genericParamType : genericParamTypes) {
                ms.paramTypeSignatures.add(genericParamType.getTypeName());
            }
            ms.returnType = method.getReturnType().getName();
            schema.methods.add(ms);
        }
        return schema;
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
        String payloadMode = request != null ? request.payloadMode : null;
        diagnostics.phase = "invoke";
        diagnostics.targetMode = request != null && request.target != null ? request.target.mode : null;
        diagnostics.configuredTarget = configuredTarget(request);
        diagnostics.resolvedTarget = diagnostics.configuredTarget;
        diagnostics.invokeStyle = "generic".equalsIgnoreCase(payloadMode) ? "$genericInvoke" : "$invoke";
        diagnostics.payloadMode = defaultValue(payloadMode, "raw");
        return diagnostics;
    }

    private DiagnosticInfo describeDiagnostics(InvokeRequest request) {
        DiagnosticInfo diagnostics = diagnostics(request);
        diagnostics.phase = "describe";
        diagnostics.invokeStyle = "$describe";
        diagnostics.targetMode = "";
        diagnostics.configuredTarget = "";
        diagnostics.resolvedTarget = "";
        diagnostics.payloadMode = defaultValue(request != null ? request.payloadMode : null, "raw");
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
