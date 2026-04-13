package com.hex1n.sofarpc.worker;

import com.fasterxml.jackson.databind.JsonNode;

public class InvokeResponse {
    public String requestId;
    public boolean ok;
    public JsonNode result;
    public RuntimeError error;
    public DiagnosticInfo diagnostics;

    public static InvokeResponse success(String requestId, JsonNode result, DiagnosticInfo diagnostics) {
        InvokeResponse response = new InvokeResponse();
        response.requestId = requestId;
        response.ok = true;
        response.result = result;
        response.diagnostics = diagnostics;
        return response;
    }

    public static InvokeResponse failure(String requestId, RuntimeError error, DiagnosticInfo diagnostics) {
        InvokeResponse response = new InvokeResponse();
        response.requestId = requestId;
        response.ok = false;
        response.error = error;
        response.diagnostics = diagnostics;
        return response;
    }
}
