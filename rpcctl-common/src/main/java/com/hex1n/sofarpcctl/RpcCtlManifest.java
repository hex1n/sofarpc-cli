package com.hex1n.sofarpcctl;

import java.util.LinkedHashMap;
import java.util.Map;

public class RpcCtlManifest {

    private String defaultEnv;
    private String sofaRpcVersion;
    private String protocol = "bolt";
    private String serialization = "hessian2";
    private Integer timeoutMs = Integer.valueOf(3000);
    private Map<String, EnvironmentBinding> envs = new LinkedHashMap<String, EnvironmentBinding>();
    private Map<String, MetadataCatalog.ServiceMetadata> services = new LinkedHashMap<String, MetadataCatalog.ServiceMetadata>();

    public String getDefaultEnv() {
        return defaultEnv;
    }

    public void setDefaultEnv(String defaultEnv) {
        this.defaultEnv = defaultEnv;
    }

    public String getSofaRpcVersion() {
        return sofaRpcVersion;
    }

    public void setSofaRpcVersion(String sofaRpcVersion) {
        this.sofaRpcVersion = sofaRpcVersion;
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

    public Map<String, EnvironmentBinding> getEnvs() {
        return envs;
    }

    public void setEnvs(Map<String, EnvironmentBinding> envs) {
        this.envs = envs;
    }

    public Map<String, MetadataCatalog.ServiceMetadata> getServices() {
        return services;
    }

    public void setServices(Map<String, MetadataCatalog.ServiceMetadata> services) {
        this.services = services;
    }

    public RpcCtlConfig toConfig() {
        RpcCtlConfig config = new RpcCtlConfig();
        config.setDefaultEnv(defaultEnv);
        config.setProtocol(protocol);
        config.setSerialization(serialization);
        config.setTimeoutMs(timeoutMs);
        config.setSofaRpcVersion(sofaRpcVersion);

        Map<String, RpcCtlConfig.EnvironmentConfig> resolvedEnvs =
            new LinkedHashMap<String, RpcCtlConfig.EnvironmentConfig>();
        if (envs != null) {
            for (Map.Entry<String, EnvironmentBinding> entry : envs.entrySet()) {
                resolvedEnvs.put(entry.getKey(), entry.getValue().toEnvironmentConfig(config));
            }
        }
        config.setEnvs(resolvedEnvs);
        return config;
    }

    public MetadataCatalog toMetadata() {
        MetadataCatalog metadataCatalog = new MetadataCatalog();
        metadataCatalog.setServices(services == null
            ? new LinkedHashMap<String, MetadataCatalog.ServiceMetadata>()
            : new LinkedHashMap<String, MetadataCatalog.ServiceMetadata>(services));
        return metadataCatalog;
    }

    public static class EnvironmentBinding {
        private String mode;
        private String protocol;
        private String serialization;
        private String registryProtocol;
        private String registryAddress;
        private String directUrl;
        private Integer timeoutMs;
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

        private RpcCtlConfig.EnvironmentConfig toEnvironmentConfig(RpcCtlConfig defaults) {
            RpcCtlConfig.EnvironmentConfig environmentConfig = new RpcCtlConfig.EnvironmentConfig();
            environmentConfig.setMode(firstNonBlank(mode, "registry"));
            environmentConfig.setProtocol(firstNonBlank(protocol, defaults.getProtocol(), "bolt"));
            environmentConfig.setSerialization(firstNonBlank(serialization, defaults.getSerialization(), "hessian2"));
            environmentConfig.setRegistryProtocol(registryProtocol);
            environmentConfig.setRegistryAddress(registryAddress);
            environmentConfig.setDirectUrl(directUrl);
            environmentConfig.setTimeoutMs(timeoutMs == null
                ? (defaults.getTimeoutMs() == null ? Integer.valueOf(3000) : defaults.getTimeoutMs())
                : timeoutMs);
            environmentConfig.setUniqueId(uniqueId);
            environmentConfig.setSofaRpcVersion(firstNonBlank(sofaRpcVersion, defaults.getSofaRpcVersion()));
            return environmentConfig;
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
    }
}
