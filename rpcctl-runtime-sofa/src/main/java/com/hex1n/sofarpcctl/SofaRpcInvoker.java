package com.hex1n.sofarpcctl;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.config.ConsumerConfig;
import com.alipay.sofa.rpc.config.RegistryConfig;

public final class SofaRpcInvoker {

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
            RuntimeInvocationResult result = RuntimeInvocationResult.failure(
                request.getEnvironmentName(),
                request.getEnvironmentConfig().getMode(),
                request.getServiceName(),
                request.getUniqueId(),
                request.getMethodName(),
                payloads == null ? request.getParamTypes() : payloads.getParamTypes(),
                genericCall,
                System.currentTimeMillis() - startTime,
                classifyError(throwable),
                summarizeThrowable(throwable)
            );
            result.setInvokeStyle(genericCall ? "$genericInvoke" : "$invoke");
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

    private String classifyError(Throwable throwable) {
        String message = summarizeThrowable(throwable).toLowerCase();
        if (message.contains("timeout")) {
            return "RPC_TIMEOUT";
        }
        if (message.contains("deserializationexception")
            || message.contains("deserialize request exception")
            || message.contains("hessianprotocolexception")) {
            return "RPC_DESERIALIZATION_ERROR";
        }
        if (message.contains("serializationexception")) {
            return "RPC_SERIALIZATION_ERROR";
        }
        if (message.contains("未找到需要调用的方法")
            || message.contains("no such method")
            || message.contains("method not found")) {
            return "RPC_METHOD_NOT_FOUND";
        }
        if (message.contains("没有获得服务") || message.contains("no available provider")) {
            return "RPC_ROUTE_ERROR";
        }
        if (message.contains("refused") || message.contains("connect")) {
            return "RPC_PROVIDER_UNREACHABLE";
        }
        return "RPC_ERROR";
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
}
