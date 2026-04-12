package com.hex1n.sofarpcctl;

import java.util.LinkedHashMap;
import java.util.Map;

public class RpcCtlConfig {

    private String metadataPath = "config/metadata.yaml";
    private Map<String, EnvironmentConfig> envs = new LinkedHashMap<String, EnvironmentConfig>();

    public String getMetadataPath() {
        return metadataPath;
    }

    public void setMetadataPath(String metadataPath) {
        this.metadataPath = metadataPath;
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
            throw new RpcCtlApplication.CliException(
                RpcCtlApplication.ExitCodes.PARAMETER_ERROR,
                "Unknown environment: " + name
            );
        }
        return environmentConfig;
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
    }
}
