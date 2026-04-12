package com.hex1n.sofarpcctl;

import java.util.ArrayList;
import java.util.List;

public class RuntimeInvocationRequest {

    private String environmentName;
    private RpcCtlConfig.EnvironmentConfig environmentConfig;
    private String serviceName;
    private String uniqueId;
    private String methodName;
    private List<String> paramTypes = new ArrayList<String>();
    private String argsJson = "[]";

    public String getEnvironmentName() {
        return environmentName;
    }

    public void setEnvironmentName(String environmentName) {
        this.environmentName = environmentName;
    }

    public RpcCtlConfig.EnvironmentConfig getEnvironmentConfig() {
        return environmentConfig;
    }

    public void setEnvironmentConfig(RpcCtlConfig.EnvironmentConfig environmentConfig) {
        this.environmentConfig = environmentConfig;
    }

    public String getServiceName() {
        return serviceName;
    }

    public void setServiceName(String serviceName) {
        this.serviceName = serviceName;
    }

    public String getUniqueId() {
        return uniqueId;
    }

    public void setUniqueId(String uniqueId) {
        this.uniqueId = uniqueId;
    }

    public String getMethodName() {
        return methodName;
    }

    public void setMethodName(String methodName) {
        this.methodName = methodName;
    }

    public List<String> getParamTypes() {
        return paramTypes;
    }

    public void setParamTypes(List<String> paramTypes) {
        this.paramTypes = paramTypes;
    }

    public String getArgsJson() {
        return argsJson;
    }

    public void setArgsJson(String argsJson) {
        this.argsJson = argsJson;
    }
}
