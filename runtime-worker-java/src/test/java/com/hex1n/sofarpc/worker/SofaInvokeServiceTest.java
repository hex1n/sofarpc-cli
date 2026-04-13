package com.hex1n.sofarpc.worker;

import com.alipay.sofa.rpc.core.exception.RpcErrorType;
import com.alipay.sofa.rpc.core.exception.SofaRpcException;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

public class SofaInvokeServiceTest {
    private final SofaInvokeService service = new SofaInvokeService(new ObjectMapper());

    @Test
    void mapsClientTimeoutToTimeoutInvoke() {
        RuntimeError error = service.mapRpcError(
            new SofaRpcException(RpcErrorType.CLIENT_TIMEOUT, "wait timeout"),
            directDiagnostics()
        );
        assertEquals("TIMEOUT_INVOKE", error.code);
        assertEquals("invoke", error.phase);
        assertTrue(error.retriable);
        assertEquals("direct", error.targetMode);
        assertEquals("bolt://127.0.0.1:12200", error.configuredTarget);
        assertEquals("$invoke", error.invokeStyle);
    }

    @Test
    void mapsClientNetworkToProviderUnreachable() {
        RuntimeError error = service.mapRpcError(
            new SofaRpcException(RpcErrorType.CLIENT_NETWORK, "connection refused"),
            directDiagnostics()
        );
        assertEquals("PROVIDER_UNREACHABLE", error.code);
        assertEquals("connect", error.phase);
        assertTrue(error.retriable);
    }

    @Test
    void mapsSerializeFamilyToSerializationError() {
        for (int errorType : new int[]{RpcErrorType.CLIENT_SERIALIZE, RpcErrorType.SERVER_SERIALIZE}) {
            RuntimeError error = service.mapRpcError(
                new SofaRpcException(errorType, "boom"),
                directDiagnostics()
            );
            assertEquals("SERIALIZATION_ERROR", error.code);
            assertEquals("serialize", error.phase);
            assertFalse(error.retriable);
        }
    }

    @Test
    void mapsDeserializeFamilyToDeserializationError() {
        for (int errorType : new int[]{RpcErrorType.CLIENT_DESERIALIZE, RpcErrorType.SERVER_DESERIALIZE}) {
            RuntimeError error = service.mapRpcError(
                new SofaRpcException(errorType, "boom"),
                directDiagnostics()
            );
            assertEquals("DESERIALIZATION_ERROR", error.code);
            assertEquals("deserialize", error.phase);
        }
    }

    @Test
    void mapsServerNotFoundInvokerToMethodNotFound() {
        RuntimeError error = service.mapRpcError(
            new SofaRpcException(RpcErrorType.SERVER_NOT_FOUND_INVOKER, "no such method"),
            directDiagnostics()
        );
        assertEquals("METHOD_NOT_FOUND", error.code);
        assertEquals("invoke", error.phase);
    }

    @Test
    void unknownWithUnavailableMessageInfersProviderUnreachable() {
        RuntimeError error = service.mapRpcError(
            new SofaRpcException(RpcErrorType.UNKNOWN, "service unavailable"),
            directDiagnostics()
        );
        assertEquals("PROVIDER_UNREACHABLE", error.code);
        assertEquals("connect", error.phase);
        assertTrue(error.retriable);
    }

    @Test
    void unknownWithChineseUnavailableMessageInfersProviderUnreachable() {
        RuntimeError error = service.mapRpcError(
            new SofaRpcException(RpcErrorType.UNKNOWN, "服务不可用"),
            directDiagnostics()
        );
        assertEquals("PROVIDER_UNREACHABLE", error.code);
    }

    @Test
    void unknownWithAddressMessageInfersProviderUnreachable() {
        RuntimeError error = service.mapRpcError(
            new SofaRpcException(RpcErrorType.UNKNOWN, "no available address"),
            directDiagnostics()
        );
        assertEquals("PROVIDER_UNREACHABLE", error.code);
    }

    @Test
    void unknownWithProviderMessageInfersProviderNotFound() {
        RuntimeError error = service.mapRpcError(
            new SofaRpcException(RpcErrorType.UNKNOWN, "provider list is empty"),
            directDiagnostics()
        );
        assertEquals("PROVIDER_NOT_FOUND", error.code);
        assertEquals("discover", error.phase);
    }

    @Test
    void unknownGenericFailureFallsBackToInvokeFailed() {
        RuntimeError error = service.mapRpcError(
            new SofaRpcException(RpcErrorType.UNKNOWN, "boom"),
            directDiagnostics()
        );
        assertEquals("INVOKE_FAILED", error.code);
        assertEquals("invoke", error.phase);
    }

    @Test
    void registryDiagnosticsPropagateToError() {
        DiagnosticInfo diagnostics = new DiagnosticInfo();
        diagnostics.targetMode = "registry";
        diagnostics.configuredTarget = "zookeeper://127.0.0.1:2181";
        diagnostics.resolvedTarget = diagnostics.configuredTarget;
        diagnostics.invokeStyle = "$genericInvoke";
        diagnostics.payloadMode = "generic";

        RuntimeError error = service.mapRpcError(
            new SofaRpcException(RpcErrorType.CLIENT_TIMEOUT, "timeout"),
            diagnostics
        );

        assertEquals("registry", error.targetMode);
        assertEquals("zookeeper://127.0.0.1:2181", error.configuredTarget);
        assertEquals("zookeeper://127.0.0.1:2181", error.resolvedTarget);
        assertEquals("$genericInvoke", error.invokeStyle);
        assertEquals("generic", error.payloadMode);
    }

    private DiagnosticInfo directDiagnostics() {
        DiagnosticInfo diagnostics = new DiagnosticInfo();
        diagnostics.phase = "invoke";
        diagnostics.targetMode = "direct";
        diagnostics.configuredTarget = "bolt://127.0.0.1:12200";
        diagnostics.resolvedTarget = "bolt://127.0.0.1:12200";
        diagnostics.invokeStyle = "$invoke";
        diagnostics.payloadMode = "raw";
        return diagnostics;
    }
}
