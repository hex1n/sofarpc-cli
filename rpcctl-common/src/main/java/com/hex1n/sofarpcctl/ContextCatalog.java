package com.hex1n.sofarpcctl;

import com.fasterxml.jackson.annotation.JsonIgnore;

import java.util.LinkedHashMap;
import java.util.Map;

public class ContextCatalog {

    private String currentContext;
    private String runtimeBaseUrl;
    private String runtimeHome;
    private String runtimeCacheDir;
    private Boolean autoDownloadRuntimes = Boolean.TRUE;
    private Map<String, ContextEntry> contexts = new LinkedHashMap<String, ContextEntry>();

    public String getCurrentContext() {
        return currentContext;
    }

    public void setCurrentContext(String currentContext) {
        this.currentContext = currentContext;
    }

    public String getRuntimeBaseUrl() {
        return runtimeBaseUrl;
    }

    public void setRuntimeBaseUrl(String runtimeBaseUrl) {
        this.runtimeBaseUrl = runtimeBaseUrl;
    }

    public String getRuntimeHome() {
        return runtimeHome;
    }

    public void setRuntimeHome(String runtimeHome) {
        this.runtimeHome = runtimeHome;
    }

    public String getRuntimeCacheDir() {
        return runtimeCacheDir;
    }

    public void setRuntimeCacheDir(String runtimeCacheDir) {
        this.runtimeCacheDir = runtimeCacheDir;
    }

    public Boolean getAutoDownloadRuntimes() {
        return autoDownloadRuntimes;
    }

    public void setAutoDownloadRuntimes(Boolean autoDownloadRuntimes) {
        this.autoDownloadRuntimes = autoDownloadRuntimes;
    }

    public Map<String, ContextEntry> getContexts() {
        return contexts;
    }

    public void setContexts(Map<String, ContextEntry> contexts) {
        this.contexts = contexts;
    }

    public boolean isEmpty() {
        return contexts == null || contexts.isEmpty();
    }

    public ResolvedContext resolveSelected(String explicitName) {
        String selectedName = firstNonBlank(explicitName, currentContext);
        if (selectedName == null) {
            return new ResolvedContext(null, new ContextEntry());
        }
        ContextEntry entry = contexts == null ? null : contexts.get(selectedName);
        if (entry == null) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Unknown context: " + selectedName
            );
        }

        ContextEntry merged = new ContextEntry();
        merged.setDescription(entry.getDescription());
        merged.setManifestPath(entry.getManifestPath());
        merged.setEnv(entry.getEnv());
        merged.setDirectUrl(entry.getDirectUrl());
        merged.setRegistryProtocol(entry.getRegistryProtocol());
        merged.setRegistryAddress(entry.getRegistryAddress());
        merged.setProtocol(entry.getProtocol());
        merged.setSerialization(entry.getSerialization());
        merged.setTimeoutMs(entry.getTimeoutMs());
        merged.setSofaRpcVersion(entry.getSofaRpcVersion());
        merged.setRuntimeBaseUrl(firstNonBlank(entry.getRuntimeBaseUrl(), runtimeBaseUrl));
        merged.setRuntimeHome(firstNonBlank(entry.getRuntimeHome(), runtimeHome));
        merged.setRuntimeCacheDir(firstNonBlank(entry.getRuntimeCacheDir(), runtimeCacheDir));
        merged.setAutoDownloadRuntimes(entry.getAutoDownloadRuntimes() == null
            ? autoDownloadRuntimes
            : entry.getAutoDownloadRuntimes());
        return new ResolvedContext(selectedName, merged);
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

    public static final class ResolvedContext {
        private final String name;
        private final ContextEntry entry;

        public ResolvedContext(String name, ContextEntry entry) {
            this.name = name;
            this.entry = entry;
        }

        public String getName() {
            return name;
        }

        public ContextEntry getEntry() {
            return entry;
        }
    }

    public static class ContextEntry {
        private String description;
        private String manifestPath;
        private String env;
        private String directUrl;
        private String registryProtocol;
        private String registryAddress;
        private String protocol;
        private String serialization;
        private Integer timeoutMs;
        private String sofaRpcVersion;
        private String runtimeBaseUrl;
        private String runtimeHome;
        private String runtimeCacheDir;
        private Boolean autoDownloadRuntimes;

        public String getDescription() {
            return description;
        }

        public void setDescription(String description) {
            this.description = description;
        }

        public String getManifestPath() {
            return manifestPath;
        }

        public void setManifestPath(String manifestPath) {
            this.manifestPath = manifestPath;
        }

        public String getEnv() {
            return env;
        }

        public void setEnv(String env) {
            this.env = env;
        }

        public String getDirectUrl() {
            return directUrl;
        }

        public void setDirectUrl(String directUrl) {
            this.directUrl = directUrl;
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

        public String getRuntimeBaseUrl() {
            return runtimeBaseUrl;
        }

        public void setRuntimeBaseUrl(String runtimeBaseUrl) {
            this.runtimeBaseUrl = runtimeBaseUrl;
        }

        public String getRuntimeHome() {
            return runtimeHome;
        }

        public void setRuntimeHome(String runtimeHome) {
            this.runtimeHome = runtimeHome;
        }

        public String getRuntimeCacheDir() {
            return runtimeCacheDir;
        }

        public void setRuntimeCacheDir(String runtimeCacheDir) {
            this.runtimeCacheDir = runtimeCacheDir;
        }

        public Boolean getAutoDownloadRuntimes() {
            return autoDownloadRuntimes;
        }

        public void setAutoDownloadRuntimes(Boolean autoDownloadRuntimes) {
            this.autoDownloadRuntimes = autoDownloadRuntimes;
        }

        @JsonIgnore
        public boolean isEmpty() {
            return isBlank(description)
                && isBlank(manifestPath)
                && isBlank(env)
                && isBlank(directUrl)
                && isBlank(registryProtocol)
                && isBlank(registryAddress)
                && isBlank(protocol)
                && isBlank(serialization)
                && timeoutMs == null
                && isBlank(sofaRpcVersion)
                && isBlank(runtimeBaseUrl)
                && isBlank(runtimeHome)
                && isBlank(runtimeCacheDir)
                && autoDownloadRuntimes == null;
        }

        private boolean isBlank(String value) {
            return value == null || value.trim().isEmpty();
        }
    }
}
