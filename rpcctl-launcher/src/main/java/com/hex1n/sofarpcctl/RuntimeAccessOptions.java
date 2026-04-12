package com.hex1n.sofarpcctl;

public final class RuntimeAccessOptions {

    private final String runtimeHome;
    private final String runtimeBaseUrl;
    private final String runtimeCacheDir;
    private final boolean autoDownloadEnabled;

    public RuntimeAccessOptions(
        String runtimeHome,
        String runtimeBaseUrl,
        String runtimeCacheDir,
        boolean autoDownloadEnabled
    ) {
        this.runtimeHome = runtimeHome;
        this.runtimeBaseUrl = runtimeBaseUrl;
        this.runtimeCacheDir = runtimeCacheDir;
        this.autoDownloadEnabled = autoDownloadEnabled;
    }

    public String getRuntimeHome() {
        return runtimeHome;
    }

    public String getRuntimeBaseUrl() {
        return runtimeBaseUrl;
    }

    public String getRuntimeCacheDir() {
        return runtimeCacheDir;
    }

    public boolean isAutoDownloadEnabled() {
        return autoDownloadEnabled;
    }
}
