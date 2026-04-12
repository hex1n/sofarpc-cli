package com.hex1n.sofarpcctl;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.config.ConsumerConfig;
import com.alipay.sofa.rpc.config.RegistryConfig;

public final class SofaRpcInvoker {

    public RuntimeInvocationResult invoke(RuntimeInvocationRequest request) {
        long startTime = System.currentTimeMillis();
        try {
            InvocationPayloads.ResolvedPayloads payloads = InvocationPayloads.resolve(
                request.getParamTypes(),
                request.getArgsJson()
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
            return RuntimeInvocationResult.success(
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
        } catch (Throwable throwable) {
            return RuntimeInvocationResult.failure(
                request.getEnvironmentName(),
                request.getEnvironmentConfig().getMode(),
                request.getServiceName(),
                request.getUniqueId(),
                request.getMethodName(),
                request.getParamTypes(),
                false,
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
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
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
}
