package com.hex1n.sofarpcctl;

import java.util.List;

public class RuntimeInvocationResult {

    private boolean success;
    private String env;
    private String mode;
    private String service;
    private String uniqueId;
    private String method;
    private List<String> paramTypes;
    private boolean genericCall;
    private long elapsedMs;
    private Object result;
    private String errorCode;
    private String message;
    private String details;
    private String hint;

    public static RuntimeInvocationResult success(
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
        RuntimeInvocationResult invocationResult = new RuntimeInvocationResult();
        invocationResult.success = true;
        invocationResult.env = env;
        invocationResult.mode = mode;
        invocationResult.service = service;
        invocationResult.uniqueId = uniqueId;
        invocationResult.method = method;
        invocationResult.paramTypes = paramTypes;
        invocationResult.genericCall = genericCall;
        invocationResult.elapsedMs = elapsedMs;
        invocationResult.result = result;
        return invocationResult;
    }

    public static RuntimeInvocationResult failure(
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
        RuntimeInvocationResult invocationResult = new RuntimeInvocationResult();
        invocationResult.success = false;
        invocationResult.env = env;
        invocationResult.mode = mode;
        invocationResult.service = service;
        invocationResult.uniqueId = uniqueId;
        invocationResult.method = method;
        invocationResult.paramTypes = paramTypes;
        invocationResult.genericCall = genericCall;
        invocationResult.elapsedMs = elapsedMs;
        invocationResult.errorCode = errorCode;
        invocationResult.message = message;
        return invocationResult;
    }

    public boolean isSuccess() {
        return success;
    }

    public void setSuccess(boolean success) {
        this.success = success;
    }

    public String getEnv() {
        return env;
    }

    public void setEnv(String env) {
        this.env = env;
    }

    public String getMode() {
        return mode;
    }

    public void setMode(String mode) {
        this.mode = mode;
    }

    public String getService() {
        return service;
    }

    public void setService(String service) {
        this.service = service;
    }

    public String getUniqueId() {
        return uniqueId;
    }

    public void setUniqueId(String uniqueId) {
        this.uniqueId = uniqueId;
    }

    public String getMethod() {
        return method;
    }

    public void setMethod(String method) {
        this.method = method;
    }

    public List<String> getParamTypes() {
        return paramTypes;
    }

    public void setParamTypes(List<String> paramTypes) {
        this.paramTypes = paramTypes;
    }

    public boolean isGenericCall() {
        return genericCall;
    }

    public void setGenericCall(boolean genericCall) {
        this.genericCall = genericCall;
    }

    public long getElapsedMs() {
        return elapsedMs;
    }

    public void setElapsedMs(long elapsedMs) {
        this.elapsedMs = elapsedMs;
    }

    public Object getResult() {
        return result;
    }

    public void setResult(Object result) {
        this.result = result;
    }

    public String getErrorCode() {
        return errorCode;
    }

    public void setErrorCode(String errorCode) {
        this.errorCode = errorCode;
    }

    public String getMessage() {
        return message;
    }

    public void setMessage(String message) {
        this.message = message;
    }

    public String getDetails() {
        return details;
    }

    public void setDetails(String details) {
        this.details = details;
    }

    public String getHint() {
        return hint;
    }

    public void setHint(String hint) {
        this.hint = hint;
    }
}
