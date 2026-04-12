package com.hex1n.sofarpcctl;

import java.util.LinkedHashMap;
import java.util.Map;

public class RpcCtlConfig {

    private String metadataPath = "config/metadata.yaml";
    private String defaultEnv;
    private String protocol = "bolt";
    private String serialization = "hessian2";
    private Integer timeoutMs = Integer.valueOf(3000);
    private String sofaRpcVersion;
    private Map<String, EnvironmentConfig> envs = new LinkedHashMap<String, EnvironmentConfig>();

    public String getMetadataPath() {
        return metadataPath;
    }

    public void setMetadataPath(String metadataPath) {
        this.metadataPath = metadataPath;
    }

    public String getDefaultEnv() {
        return defaultEnv;
    }

    public void setDefaultEnv(String defaultEnv) {
        this.defaultEnv = defaultEnv;
    }

    public String getProtocol() {
        return protocol;
    }

    public void setProtocol(String protocol) {
        this.protocol = protocol;
    }

    public String getSerialization() {
        return serialization;
    }

    public void setSerialization(String serialization) {
        this.serialization = serialization;
    }

    public Integer getTimeoutMs() {
        return timeoutMs;
    }

    public void setTimeoutMs(Integer timeoutMs) {
        this.timeoutMs = timeoutMs;
    }

    public String getSofaRpcVersion() {
        return sofaRpcVersion;
    }

    public void setSofaRpcVersion(String sofaRpcVersion) {
        this.sofaRpcVersion = sofaRpcVersion;
    }

    public Map<String, EnvironmentConfig> getEnvs() {
        return envs;
    }

    public void setEnvs(Map<String, EnvironmentConfig> envs) {
        this.envs = envs;
    }

    public EnvironmentConfig requireEnv(String name) {
        EnvironmentConfig environmentConfig = envs == null ? null : envs.get(name);
        if (environmentConfig == null) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Unknown environment: " + name
            );
        }
        return applyDefaults(environmentConfig);
    }

    public EnvironmentConfig applyDefaults(EnvironmentConfig environmentConfig) {
        EnvironmentConfig merged = new EnvironmentConfig();
        if (environmentConfig != null && environmentConfig.getMode() != null && !environmentConfig.getMode().trim().isEmpty()) {
            merged.setMode(environmentConfig.getMode());
        }
        merged.setProtocol(firstNonBlank(
            environmentConfig == null ? null : environmentConfig.getProtocol(),
            protocol,
            "bolt"
        ));
        merged.setSerialization(firstNonBlank(
            environmentConfig == null ? null : environmentConfig.getSerialization(),
            serialization,
            "hessian2"
        ));
        merged.setRegistryProtocol(environmentConfig == null ? null : environmentConfig.getRegistryProtocol());
        merged.setRegistryAddress(environmentConfig == null ? null : environmentConfig.getRegistryAddress());
        merged.setDirectUrl(environmentConfig == null ? null : environmentConfig.getDirectUrl());
        merged.setTimeoutMs(environmentConfig != null && environmentConfig.getTimeoutMs() != null
            ? environmentConfig.getTimeoutMs()
            : (timeoutMs == null ? Integer.valueOf(3000) : timeoutMs));
        merged.setUniqueId(environmentConfig == null ? null : environmentConfig.getUniqueId());
        merged.setSofaRpcVersion(firstNonBlank(
            environmentConfig == null ? null : environmentConfig.getSofaRpcVersion(),
            sofaRpcVersion
        ));
        return merged;
    }

    private static String firstNonBlank(String... values) {
        if (values == null) {
            return null;
        }
        for (String value : values) {
            if (value != null && !value.trim().isEmpty()) {
                return value.trim();
            }
        }
        return null;
    }

    public static class EnvironmentConfig {
        private String mode = "registry";
        private String protocol = "bolt";
        private String serialization = "hessian2";
        private String registryProtocol;
        private String registryAddress;
        private String directUrl;
        private Integer timeoutMs = Integer.valueOf(3000);
        private String uniqueId;
        private String sofaRpcVersion;

        public String getMode() {
            return mode;
        }

        public void setMode(String mode) {
            this.mode = mode;
        }

        public String getProtocol() {
            return protocol;
        }

        public void setProtocol(String protocol) {
            this.protocol = protocol;
        }

        public String getSerialization() {
            return serialization;
        }

        public void setSerialization(String serialization) {
            this.serialization = serialization;
        }

        public String getRegistryProtocol() {
            return registryProtocol;
        }

        public void setRegistryProtocol(String registryProtocol) {
            this.registryProtocol = registryProtocol;
        }

        public String getRegistryAddress() {
            return registryAddress;
        }

        public void setRegistryAddress(String registryAddress) {
            this.registryAddress = registryAddress;
        }

        public String getDirectUrl() {
            return directUrl;
        }

        public void setDirectUrl(String directUrl) {
            this.directUrl = directUrl;
        }

        public Integer getTimeoutMs() {
            return timeoutMs;
        }

        public void setTimeoutMs(Integer timeoutMs) {
            this.timeoutMs = timeoutMs;
        }

        public String getUniqueId() {
            return uniqueId;
        }

        public void setUniqueId(String uniqueId) {
            this.uniqueId = uniqueId;
        }

        public String getSofaRpcVersion() {
            return sofaRpcVersion;
        }

        public void setSofaRpcVersion(String sofaRpcVersion) {
            this.sofaRpcVersion = sofaRpcVersion;
        }
    }
}
