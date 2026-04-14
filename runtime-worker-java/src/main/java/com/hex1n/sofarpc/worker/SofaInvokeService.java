package com.hex1n.sofarpc.worker;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.config.ConsumerConfig;
import com.alipay.sofa.rpc.config.RegistryConfig;
import com.alipay.sofa.rpc.core.exception.RpcErrorType;
import com.alipay.sofa.rpc.core.exception.SofaRpcException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;

public final class SofaInvokeService {
    private final ObjectMapper mapper;
    private final PayloadConverter payloadConverter;
    private final ResponseTreeWriter responseTreeWriter;
    private final ConcurrentMap<String, CachedConsumer> consumers = new ConcurrentHashMap<String, CachedConsumer>();

    public SofaInvokeService(ObjectMapper mapper) {
        this.mapper = mapper;
        this.payloadConverter = new PayloadConverter(mapper);
        this.responseTreeWriter = new ResponseTreeWriter(mapper);
    }

    public InvokeResponse invoke(InvokeRequest request) {
        PayloadMode mode = PayloadMode.fromValue(request.payloadMode);
        DiagnosticInfo diagnostics = diagnostics(request, mode);
        try {
            validate(request);
            CachedConsumer consumer = getOrCreateConsumer(request);
            Object[] args = payloadConverter.convertArguments(
                mode,
                normalizeParamTypes(request.paramTypes),
                normalizeParamTypes(request.paramTypeSignatures),
                request.args
            );
            String[] paramTypes = normalizeParamTypes(request.paramTypes).toArray(new String[0]);
            Object result;
            if (mode == PayloadMode.GENERIC) {
                result = consumer.service.$genericInvoke(request.method, paramTypes, args);
            } else {
                result = consumer.service.$invoke(request.method, paramTypes, args);
            }
            JsonNode jsonResult;
            try {
                jsonResult = responseTreeWriter.write(result);
            } catch (Exception mapEx) {
                return InvokeResponse.failure(request.requestId, responseMappingError(mapEx, diagnostics), diagnostics);
            }
            return InvokeResponse.success(request.requestId, jsonResult, diagnostics);
        } catch (IllegalArgumentException ex) {
            return InvokeResponse.failure(request.requestId, invalidArguments(ex, diagnostics), diagnostics);
        } catch (SofaRpcException ex) {
            return InvokeResponse.failure(request.requestId, mapRpcError(ex, diagnostics), diagnostics);
        } catch (Exception ex) {
            return InvokeResponse.failure(request.requestId, internalError(ex, diagnostics), diagnostics);
        }
    }

    public void close() {
        for (Map.Entry<String, CachedConsumer> entry : consumers.entrySet()) {
            entry.getValue().config.unRefer();
        }
        consumers.clear();
    }

    private CachedConsumer getOrCreateConsumer(InvokeRequest request) {
        String key = consumerKey(request);
        CachedConsumer existing = consumers.get(key);
        if (existing != null) {
            return existing;
        }
        CachedConsumer created = createConsumer(request);
        CachedConsumer previous = consumers.putIfAbsent(key, created);
        if (previous != null) {
            created.config.unRefer();
            return previous;
        }
        return created;
    }

    private CachedConsumer createConsumer(InvokeRequest request) {
        ConsumerConfig<GenericService> config = new ConsumerConfig<GenericService>()
            .setInterfaceId(request.service)
            .setProtocol(nonEmpty(request.target.protocol, "bolt"))
            .setSerialization(nonEmpty(request.target.serialization, "hessian2"))
            .setTimeout(request.target.timeoutMs > 0 ? request.target.timeoutMs : 3000)
            .setConnectTimeout(request.target.connectTimeoutMs > 0 ? request.target.connectTimeoutMs : 1000)
            .setGeneric(true)
            .setCheck(false);
        if (request.target.uniqueId != null && !request.target.uniqueId.trim().isEmpty()) {
            config.setUniqueId(request.target.uniqueId);
        }
        if ("direct".equalsIgnoreCase(request.target.mode)) {
            config.setDirectUrl(request.target.directUrl);
        } else if ("registry".equalsIgnoreCase(request.target.mode)) {
            RegistryConfig registry = new RegistryConfig()
                .setProtocol(nonEmpty(request.target.registryProtocol, "zookeeper"))
                .setAddress(request.target.registryAddress)
                .setRegister(false)
                .setSubscribe(true)
                .setTimeout(request.target.timeoutMs > 0 ? request.target.timeoutMs : 3000)
                .setConnectTimeout(request.target.connectTimeoutMs > 0 ? request.target.connectTimeoutMs : 1000);
            config.setRegistry(registry);
        } else {
            throw new IllegalArgumentException("unsupported target mode: " + request.target.mode);
        }
        return new CachedConsumer(config, config.refer());
    }

    private List<String> normalizeParamTypes(List<String> values) {
        if (values == null) {
            return new ArrayList<String>();
        }
        return values;
    }

    private void validate(InvokeRequest request) {
        if (isBlank(request.service)) {
            throw new IllegalArgumentException("service is required");
        }
        if (isBlank(request.method)) {
            throw new IllegalArgumentException("method is required");
        }
        if (request.target == null || isBlank(request.target.mode)) {
            throw new IllegalArgumentException("target mode is required");
        }
    }

    private String consumerKey(InvokeRequest request) {
        return String.join("|",
            nonEmpty(request.target.mode, ""),
            nonEmpty(request.target.directUrl, ""),
            nonEmpty(request.target.registryProtocol, ""),
            nonEmpty(request.target.registryAddress, ""),
            nonEmpty(request.target.protocol, ""),
            nonEmpty(request.target.serialization, ""),
            nonEmpty(request.service, ""),
            nonEmpty(request.target.uniqueId, ""),
            String.valueOf(request.target.timeoutMs),
            String.valueOf(request.target.connectTimeoutMs)
        );
    }

    private DiagnosticInfo diagnostics(InvokeRequest request, PayloadMode mode) {
        DiagnosticInfo diagnostics = new DiagnosticInfo();
        diagnostics.phase = "invoke";
        diagnostics.targetMode = request.target != null ? request.target.mode : null;
        diagnostics.configuredTarget = configuredTarget(request.target);
        diagnostics.resolvedTarget = diagnostics.configuredTarget;
        diagnostics.invokeStyle = mode == PayloadMode.GENERIC ? "$genericInvoke" : "$invoke";
        diagnostics.payloadMode = mode.value();
        return diagnostics;
    }

    private RuntimeError invalidArguments(Exception ex, DiagnosticInfo diagnostics) {
        RuntimeError error = baseError(diagnostics);
        error.code = "INVALID_ARGUMENTS";
        error.message = ex.getMessage();
        error.phase = "prepare";
        error.hint = hintForMappingFailure(ex.getMessage(),
            "Check method signature, payload mode, and argument JSON.");
        return error;
    }

    private RuntimeError responseMappingError(Exception ex, DiagnosticInfo diagnostics) {
        RuntimeError error = baseError(diagnostics);
        error.code = "RESPONSE_MAPPING_ERROR";
        error.message = ex.getMessage();
        error.phase = "response";
        error.hint = hintForMappingFailure(ex.getMessage(),
            "Retry with -payload-mode generic to bypass strict response DTO mapping.");
        return error;
    }

    static String hintForMappingFailure(String message, String fallback) {
        if (message == null) {
            return fallback;
        }
        String low = message.toLowerCase();
        if (low.contains("optional") && low.contains("not supported")) {
            return "Response contains java.util.Optional getters. Worker already pre-registers Jdk8Module; "
                + "if this still fires, the facade jar ships an older Jackson — retry with -payload-mode generic.";
        }
        if (low.contains("jackson-datatype") || low.contains("jsr310") || low.contains("add module")) {
            return "Jackson is missing a datatype module for the response type. "
                + "Retry with -payload-mode generic, or rebuild the worker with the required Jackson module.";
        }
        if (low.contains("no suitable constructor") || low.contains("cannot construct instance")) {
            return "DTO has no default constructor for raw mode. Retry with -payload-mode generic.";
        }
        if (low.contains("cannot deserialize") || low.contains("cannot serialize")) {
            return "Payload shape mismatch. Verify field types/annotations or retry with -payload-mode generic.";
        }
        return fallback;
    }

    private RuntimeError internalError(Exception ex, DiagnosticInfo diagnostics) {
        RuntimeError error = baseError(diagnostics);
        error.code = "INTERNAL_ERROR";
        error.message = ex.getMessage();
        error.phase = "invoke";
        error.hint = "Inspect worker stderr for more details.";
        return error;
    }

    RuntimeError mapRpcError(SofaRpcException ex, DiagnosticInfo diagnostics) {
        RuntimeError error = baseError(diagnostics);
        error.message = ex.getMessage();
        switch (ex.getErrorType()) {
            case RpcErrorType.CLIENT_TIMEOUT:
                error.code = "TIMEOUT_INVOKE";
                error.phase = "invoke";
                error.retriable = true;
                error.hint = "Increase timeout or verify provider latency.";
                return error;
            case RpcErrorType.CLIENT_NETWORK:
                error.code = "PROVIDER_UNREACHABLE";
                error.phase = "connect";
                error.retriable = true;
                error.hint = "Check direct target or registry connectivity.";
                return error;
            case RpcErrorType.CLIENT_SERIALIZE:
            case RpcErrorType.SERVER_SERIALIZE:
                error.code = "SERIALIZATION_ERROR";
                error.phase = "serialize";
                error.hint = "Switch payload mode or verify DTO compatibility.";
                return error;
            case RpcErrorType.CLIENT_DESERIALIZE:
            case RpcErrorType.SERVER_DESERIALIZE:
                error.code = "DESERIALIZATION_ERROR";
                error.phase = "deserialize";
                error.hint = "Verify return type and payload schema.";
                return error;
            case RpcErrorType.SERVER_NOT_FOUND_INVOKER:
                error.code = "METHOD_NOT_FOUND";
                error.phase = "invoke";
                error.hint = "Verify service name, method name, and param types.";
                return error;
            default:
                if (containsIgnoreCase(ex.getMessage(), "unavailable")
                    || containsIgnoreCase(ex.getMessage(), "不可用")
                    || containsIgnoreCase(ex.getMessage(), "address")) {
                    error.code = "PROVIDER_UNREACHABLE";
                    error.phase = "connect";
                    error.retriable = true;
                    error.hint = "Check direct target or registry connectivity.";
                    return error;
                }
                if (containsIgnoreCase(ex.getMessage(), "provider")) {
                    error.code = "PROVIDER_NOT_FOUND";
                    error.phase = "discover";
                    error.hint = "Check registry data or direct target.";
                    return error;
                }
                error.code = "INVOKE_FAILED";
                error.phase = "invoke";
                error.hint = "Inspect provider logs and worker stderr.";
                return error;
        }
    }

    private RuntimeError baseError(DiagnosticInfo diagnostics) {
        RuntimeError error = new RuntimeError();
        error.targetMode = diagnostics.targetMode;
        error.configuredTarget = diagnostics.configuredTarget;
        error.resolvedTarget = diagnostics.resolvedTarget;
        error.invokeStyle = diagnostics.invokeStyle;
        error.payloadMode = diagnostics.payloadMode;
        return error;
    }

    private String configuredTarget(TargetConfig target) {
        if (target == null) {
            return "";
        }
        if ("direct".equalsIgnoreCase(target.mode)) {
            return target.directUrl;
        }
        if ("registry".equalsIgnoreCase(target.mode)) {
            return nonEmpty(target.registryProtocol, "zookeeper") + "://" + nonEmpty(target.registryAddress, "");
        }
        return "";
    }

    private String nonEmpty(String value, String fallback) {
        return isBlank(value) ? fallback : value;
    }

    private boolean isBlank(String value) {
        return value == null || value.trim().isEmpty();
    }

    private boolean containsIgnoreCase(String text, String fragment) {
        return text != null && text.toLowerCase().contains(fragment.toLowerCase());
    }

    private static final class CachedConsumer {
        private final ConsumerConfig<GenericService> config;
        private final GenericService service;

        private CachedConsumer(ConsumerConfig<GenericService> config, GenericService service) {
            this.config = config;
            this.service = service;
        }
    }
}
