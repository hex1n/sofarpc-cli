package com.hex1n.sofarpcctl;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.config.ConsumerConfig;
import com.alipay.sofa.rpc.config.RegistryConfig;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

public final class SofaRpcInvoker {

    public InvocationResult invoke(
        RpcCtlConfig.EnvironmentConfig environmentConfig,
        String environmentName,
        String serviceName,
        String uniqueId,
        String methodName,
        InvocationPayloads.ResolvedPayloads payloads
    ) {
        long startTime = System.currentTimeMillis();
        try {
            GenericService genericService = buildConsumer(environmentConfig, serviceName, uniqueId).refer();
            Object rawResult;
            if (payloads.isGenericCallRequired()) {
                rawResult = genericService.$genericInvoke(
                    methodName,
                    payloads.getParamTypesArray(),
                    payloads.getArguments()
                );
            } else {
                rawResult = genericService.$invoke(
                    methodName,
                    payloads.getParamTypesArray(),
                    payloads.getArguments()
                );
            }
            return InvocationResult.success(
                environmentName,
                environmentConfig.getMode(),
                serviceName,
                uniqueId,
                methodName,
                payloads.getParamTypes(),
                payloads.isGenericCallRequired(),
                System.currentTimeMillis() - startTime,
                InvocationPayloads.normalizeResult(rawResult)
            );
        } catch (Throwable throwable) {
            return InvocationResult.failure(
                environmentName,
                environmentConfig.getMode(),
                serviceName,
                uniqueId,
                methodName,
                payloads.getParamTypes(),
                payloads.isGenericCallRequired(),
                System.currentTimeMillis() - startTime,
                classifyError(throwable),
                rootCauseMessage(throwable)
            );
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
                throw new RpcCtlApplication.CliException(
                    RpcCtlApplication.ExitCodes.PARAMETER_ERROR,
                    "direct mode requires directUrl in the selected environment."
                );
            }
            consumerConfig.setDirectUrl(environmentConfig.getDirectUrl());
            return consumerConfig;
        }
        if ("registry".equals(mode)) {
            consumerConfig.setRegistry(buildRegistry(environmentConfig));
            return consumerConfig;
        }
        throw new RpcCtlApplication.CliException(
            RpcCtlApplication.ExitCodes.PARAMETER_ERROR,
            "Unsupported environment mode: " + environmentConfig.getMode()
        );
    }

    private RegistryConfig buildRegistry(RpcCtlConfig.EnvironmentConfig environmentConfig) {
        String address = environmentConfig.getRegistryAddress();
        if (address == null || address.trim().isEmpty()) {
            throw new RpcCtlApplication.CliException(
                RpcCtlApplication.ExitCodes.PARAMETER_ERROR,
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

    private String classifyError(Throwable throwable) {
        String message = rootCauseMessage(throwable).toLowerCase();
        if (message.contains("timeout")) {
            return "RPC_TIMEOUT";
        }
        if (message.contains("not found") || message.contains("no such method")) {
            return "RPC_NOT_FOUND";
        }
        if (message.contains("refused") || message.contains("connect")) {
            return "RPC_CONNECT_ERROR";
        }
        return "RPC_ERROR";
    }

    private String rootCauseMessage(Throwable throwable) {
        Throwable cursor = throwable;
        while (cursor.getCause() != null && cursor.getCause() != cursor) {
            cursor = cursor.getCause();
        }
        return cursor.getClass().getSimpleName() + ": " + cursor.getMessage();
    }

    public static final class InvocationResult {
        private final boolean success;
        private final String env;
        private final String mode;
        private final String service;
        private final String uniqueId;
        private final String method;
        private final List<String> paramTypes;
        private final boolean genericCall;
        private final long elapsedMs;
        private final Object result;
        private final String errorCode;
        private final String message;

        private InvocationResult(
            boolean success,
            String env,
            String mode,
            String service,
            String uniqueId,
            String method,
            List<String> paramTypes,
            boolean genericCall,
            long elapsedMs,
            Object result,
            String errorCode,
            String message
        ) {
            this.success = success;
            this.env = env;
            this.mode = mode;
            this.service = service;
            this.uniqueId = uniqueId;
            this.method = method;
            this.paramTypes = paramTypes;
            this.genericCall = genericCall;
            this.elapsedMs = elapsedMs;
            this.result = result;
            this.errorCode = errorCode;
            this.message = message;
        }

        public static InvocationResult success(
            String env,
            String mode,
            String service,
            String uniqueId,
            String method,
            List<String> paramTypes,
            boolean genericCall,
            long elapsedMs,
            Object result
        ) {
            return new InvocationResult(
                true,
                env,
                mode,
                service,
                uniqueId,
                method,
                paramTypes,
                genericCall,
                elapsedMs,
                result,
                null,
                null
            );
        }

        public static InvocationResult failure(
            String env,
            String mode,
            String service,
            String uniqueId,
            String method,
            List<String> paramTypes,
            boolean genericCall,
            long elapsedMs,
            String errorCode,
            String message
        ) {
            return new InvocationResult(
                false,
                env,
                mode,
                service,
                uniqueId,
                method,
                paramTypes,
                genericCall,
                elapsedMs,
                null,
                errorCode,
                message
            );
        }

        public boolean isSuccess() {
            return success;
        }

        public Map<String, Object> asMap() {
            Map<String, Object> payload = new LinkedHashMap<String, Object>();
            payload.put("success", success);
            payload.put("env", env);
            payload.put("mode", mode);
            payload.put("service", service);
            payload.put("uniqueId", uniqueId);
            payload.put("method", method);
            payload.put("paramTypes", paramTypes);
            payload.put("genericCall", genericCall);
            payload.put("elapsedMs", elapsedMs);
            if (success) {
                payload.put("result", result);
            } else {
                payload.put("errorCode", errorCode);
                payload.put("message", message);
            }
            return payload;
        }
    }
}
