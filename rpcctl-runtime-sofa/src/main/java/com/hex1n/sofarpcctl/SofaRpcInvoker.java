package com.hex1n.sofarpcctl;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.config.ConsumerConfig;
import com.alipay.sofa.rpc.config.RegistryConfig;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class SofaRpcInvoker {

    private static final Pattern PROVIDER_ADDRESS_PATTERN = Pattern.compile(
        "(?:Try connect to|Create connection to|add provider of)\\s+([^\\s!]+)"
    );

    public RuntimeInvocationResult invoke(RuntimeInvocationRequest request) {
        long startTime = System.currentTimeMillis();
        InvocationPayloads.ResolvedPayloads payloads = null;
        try {
            payloads = InvocationPayloads.resolve(
                request.getParamTypes(),
                request.getArgsJson(),
                request.getStubPaths() != null && !request.getStubPaths().isEmpty()
            );
            GenericService genericService = buildConsumer(
                request.getEnvironmentConfig(),
                request.getServiceName(),
                request.getUniqueId()
            ).refer();
            Object rawResult;
            if (payloads.isGenericCallRequired()) {
                rawResult = genericService.$genericInvoke(
                    request.getMethodName(),
                    payloads.getParamTypesArray(),
                    payloads.getArguments()
                );
            } else {
                rawResult = genericService.$invoke(
                    request.getMethodName(),
                    payloads.getParamTypesArray(),
                    payloads.getArguments()
                );
            }
            RuntimeInvocationResult result = RuntimeInvocationResult.success(
                request.getEnvironmentName(),
                request.getEnvironmentConfig().getMode(),
                request.getServiceName(),
                request.getUniqueId(),
                request.getMethodName(),
                payloads.getParamTypes(),
                payloads.isGenericCallRequired(),
                System.currentTimeMillis() - startTime,
                InvocationPayloads.normalizeResult(rawResult)
            );
            result.setInvokeStyle(payloads.isGenericCallRequired() ? "$genericInvoke" : "$invoke");
            return result;
        } catch (Throwable throwable) {
            boolean genericCall = payloads != null && payloads.isGenericCallRequired();
            StructuredFailure failure = classifyFailure(throwable, request, genericCall);
            RuntimeInvocationResult result = RuntimeInvocationResult.failure(
                request.getEnvironmentName(),
                request.getEnvironmentConfig().getMode(),
                request.getServiceName(),
                request.getUniqueId(),
                request.getMethodName(),
                payloads == null ? request.getParamTypes() : payloads.getParamTypes(),
                genericCall,
                System.currentTimeMillis() - startTime,
                failure.errorCode,
                failure.message
            );
            result.setInvokeStyle(genericCall ? "$genericInvoke" : "$invoke");
            result.setErrorPhase(failure.errorPhase);
            result.setRetriable(failure.retriable);
            result.setDetails(failure.details);
            result.setHint(failure.hint);
            result.setTransportHint(failure.transportHint);
            result.setResolvedTarget(failure.resolvedTarget);
            result.setDiagnostics(failure.diagnostics);
            return result;
        }
    }

    private ConsumerConfig<GenericService> buildConsumer(
        RpcCtlConfig.EnvironmentConfig environmentConfig,
        String serviceName,
        String uniqueId
    ) {
        ConsumerConfig<GenericService> consumerConfig = new ConsumerConfig<GenericService>()
            .setInterfaceId(serviceName)
            .setProtocol(environmentConfig.getProtocol())
            .setGeneric(true)
            .setRegister(false)
            .setTimeout(environmentConfig.getTimeoutMs().intValue());

        if (environmentConfig.getSerialization() != null && !environmentConfig.getSerialization().trim().isEmpty()) {
            consumerConfig.setSerialization(environmentConfig.getSerialization());
        }
        if (uniqueId != null && !uniqueId.trim().isEmpty()) {
            consumerConfig.setUniqueId(uniqueId);
        }

        String mode = environmentConfig.getMode() == null ? "registry" : environmentConfig.getMode().trim().toLowerCase();
        if ("direct".equals(mode)) {
            if (environmentConfig.getDirectUrl() == null || environmentConfig.getDirectUrl().trim().isEmpty()) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "direct mode requires directUrl in the selected environment."
                );
            }
            consumerConfig.setSubscribe(false);
            consumerConfig.setDirectUrl(environmentConfig.getDirectUrl());
            return consumerConfig;
        }
        if ("registry".equals(mode)) {
            consumerConfig.setSubscribe(true);
            consumerConfig.setRegistry(buildRegistry(environmentConfig));
            return consumerConfig;
        }
        throw new CliException(
            ExitCodes.PARAMETER_ERROR,
            "Unsupported environment mode: " + environmentConfig.getMode()
        );
    }

    private RegistryConfig buildRegistry(RpcCtlConfig.EnvironmentConfig environmentConfig) {
        String address = environmentConfig.getRegistryAddress();
        if (address == null || address.trim().isEmpty()) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "registry mode requires registryAddress in the selected environment."
            );
        }

        String protocol = environmentConfig.getRegistryProtocol();
        String normalizedAddress = address.trim();
        int separatorIndex = normalizedAddress.indexOf("://");
        if (separatorIndex > 0) {
            protocol = normalizedAddress.substring(0, separatorIndex);
            normalizedAddress = normalizedAddress.substring(separatorIndex + 3);
        }
        if (protocol == null || protocol.trim().isEmpty()) {
            protocol = "zookeeper";
        }

        return new RegistryConfig()
            .setProtocol(protocol)
            .setAddress(normalizedAddress);
    }

    private StructuredFailure classifyFailure(
        Throwable throwable,
        RuntimeInvocationRequest request,
        boolean genericCall
    ) {
        String summary = summarizeThrowable(throwable);
        String message = summary.toLowerCase();
        String targetMode = resolveTargetMode(request);
        String configuredTarget = resolveConfiguredTarget(request);
        String providerAddress = extractProviderAddress(summary);
        StructuredFailure failure = new StructuredFailure();
        failure.errorCode = "RPC_ERROR";
        failure.errorPhase = "invoke";
        failure.retriable = null;
        failure.message = summary;
        failure.details = summary;
        failure.hint = "Check request types, target reachability, and the selected SOFARPC runtime version.";
        failure.transportHint = "Unhandled runtime exception during the SOFARPC invocation flow.";
        failure.resolvedTarget = providerAddress == null ? configuredTarget : providerAddress;
        failure.diagnostics = buildDiagnostics(throwable, request, genericCall, summary, providerAddress);

        if (message.contains("deserializationexception")
            || message.contains("deserialize request exception")
            || message.contains("hessianprotocolexception")) {
            failure.errorCode = "RPC_DESERIALIZATION_ERROR";
            failure.errorPhase = "deserialize";
            failure.retriable = Boolean.FALSE;
            failure.hint = "Check DTO shape, stub classpath, and whether the selected SOFARPC version matches the provider stack.";
            failure.transportHint = "The provider rejected or could not decode the inbound payload.";
            return failure;
        }
        if (message.contains("serializationexception")) {
            failure.errorCode = "RPC_SERIALIZATION_ERROR";
            failure.errorPhase = "serialize";
            failure.retriable = Boolean.FALSE;
            failure.hint = "Check the argument payload shape and the interface-declared parameter types.";
            failure.transportHint = "The client failed while serializing the outbound SOFARPC request.";
            return failure;
        }
        if (message.contains("未找到需要调用的方法")
            || message.contains("no such method")
            || message.contains("method not found")) {
            failure.errorCode = "RPC_METHOD_NOT_FOUND";
            failure.errorPhase = "invoke";
            failure.retriable = Boolean.FALSE;
            failure.hint = "Check the method name, uniqueId, and parameter signature. Use --types when metadata is ambiguous or stale.";
            failure.transportHint = "The provider received the request but could not match it to an exported method signature.";
            return failure;
        }
        if (message.contains("没有获得服务") || message.contains("no available provider")) {
            if (providerAddress != null
                || message.contains("add provider of")
                || ("direct".equals(targetMode) && configuredTarget != null && !configuredTarget.trim().isEmpty())) {
                failure.errorCode = "RPC_PROVIDER_UNREACHABLE";
                failure.errorPhase = "connect";
                failure.retriable = Boolean.TRUE;
                if ("direct".equals(targetMode)) {
                    failure.hint = "The configured direct target could not be reached. Check directUrl, provider bind address, and network reachability.";
                    failure.transportHint = "The direct target did not yield a usable provider endpoint for the invocation.";
                } else {
                    failure.hint = "A provider was selected but it was unreachable. Check provider virtualHost/virtualPort and client network reachability.";
                    failure.transportHint = "Service discovery returned a provider address, but the client could not establish a transport connection.";
                }
            } else {
                failure.errorCode = "RPC_PROVIDER_NOT_FOUND";
                failure.errorPhase = "discovery";
                failure.retriable = Boolean.TRUE;
                failure.hint = "No provider was discovered. Check registry address, service name, uniqueId, version, and whether the service is exported.";
                failure.transportHint = "The registry lookup completed without any matching providers.";
            }
            return failure;
        }
        if (message.contains("timeout")) {
            failure.errorCode = "RPC_TIMEOUT";
            failure.errorPhase = isConnectTimeout(message) ? "connect" : "invoke";
            failure.retriable = Boolean.TRUE;
            if ("connect".equals(failure.errorPhase)) {
                failure.hint = "Check provider reachability, published address, and network latency.";
                failure.transportHint = "The client timed out while opening or reusing a provider transport connection.";
            } else {
                failure.hint = "Check timeout settings, provider load, and end-to-end network latency.";
                failure.transportHint = "The provider did not complete the invocation before the configured deadline.";
            }
            return failure;
        }
        if (message.contains("refused") || message.contains("connect")) {
            failure.errorCode = "RPC_PROVIDER_UNREACHABLE";
            failure.errorPhase = "connect";
            failure.retriable = Boolean.TRUE;
            failure.hint = "Check provider address publication, directUrl/registry target, and network reachability.";
            failure.transportHint = "The client failed while establishing a transport connection to the provider.";
            return failure;
        }
        return failure;
    }

    private String summarizeThrowable(Throwable throwable) {
        StringBuilder summary = new StringBuilder();
        Throwable cursor = throwable;
        int depth = 0;
        while (cursor != null && depth < 3) {
            if (summary.length() > 0) {
                summary.append(" <- ");
            }
            summary.append(cursor.getClass().getSimpleName());
            if (cursor.getMessage() != null && !cursor.getMessage().trim().isEmpty()) {
                summary.append(": ").append(cursor.getMessage().trim());
            }
            if (cursor.getCause() == cursor) {
                break;
            }
            cursor = cursor.getCause();
            depth++;
        }
        return summary.toString();
    }

    private Map<String, String> buildDiagnostics(
        Throwable throwable,
        RuntimeInvocationRequest request,
        boolean genericCall,
        String summary,
        String providerAddress
    ) {
        Map<String, String> diagnostics = new LinkedHashMap<String, String>();
        diagnostics.put("exceptionChain", summary);
        diagnostics.put("rootException", findRootExceptionName(throwable));
        diagnostics.put("targetMode", resolveTargetMode(request));
        String configuredTarget = resolveConfiguredTarget(request);
        if (configuredTarget != null) {
            diagnostics.put("configuredTarget", configuredTarget);
        }
        diagnostics.put("invokeStyle", genericCall ? "$genericInvoke" : "$invoke");
        if (providerAddress != null && !providerAddress.trim().isEmpty()) {
            diagnostics.put("providerAddress", providerAddress.trim());
        }
        return diagnostics;
    }

    private String findRootExceptionName(Throwable throwable) {
        Throwable cursor = throwable;
        Throwable last = throwable;
        while (cursor != null) {
            last = cursor;
            if (cursor.getCause() == null || cursor.getCause() == cursor) {
                break;
            }
            cursor = cursor.getCause();
        }
        return last == null ? "UnknownException" : last.getClass().getName();
    }

    private String resolveTargetMode(RuntimeInvocationRequest request) {
        if (request == null || request.getEnvironmentConfig() == null || request.getEnvironmentConfig().getMode() == null) {
            return "unknown";
        }
        return request.getEnvironmentConfig().getMode().trim().toLowerCase();
    }

    private String resolveConfiguredTarget(RuntimeInvocationRequest request) {
        if (request == null || request.getEnvironmentConfig() == null) {
            return null;
        }
        RpcCtlConfig.EnvironmentConfig environmentConfig = request.getEnvironmentConfig();
        String mode = resolveTargetMode(request);
        if ("direct".equals(mode)) {
            return environmentConfig.getDirectUrl();
        }
        if ("registry".equals(mode)) {
            if (environmentConfig.getRegistryProtocol() != null
                && !environmentConfig.getRegistryProtocol().trim().isEmpty()
                && environmentConfig.getRegistryAddress() != null
                && environmentConfig.getRegistryAddress().indexOf("://") < 0) {
                return environmentConfig.getRegistryProtocol().trim() + "://" + environmentConfig.getRegistryAddress();
            }
            return environmentConfig.getRegistryAddress();
        }
        return null;
    }

    private boolean isConnectTimeout(String message) {
        return message.contains("connect timeout")
            || message.contains("connection timed out")
            || message.contains("connectexception")
            || message.contains("sockettimeoutexception");
    }

    private String extractProviderAddress(String content) {
        if (content == null || content.trim().isEmpty()) {
            return null;
        }
        Matcher matcher = PROVIDER_ADDRESS_PATTERN.matcher(content);
        if (matcher.find()) {
            return matcher.group(1);
        }
        return null;
    }

    private static final class StructuredFailure {
        private String errorCode;
        private String errorPhase;
        private Boolean retriable;
        private String message;
        private String details;
        private String hint;
        private String transportHint;
        private String resolvedTarget;
        private Map<String, String> diagnostics;
    }
}
