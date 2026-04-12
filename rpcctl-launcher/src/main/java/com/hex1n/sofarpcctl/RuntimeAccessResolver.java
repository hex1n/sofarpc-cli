package com.hex1n.sofarpcctl;

final class RuntimeAccessResolver {

    RuntimeAccessOptions resolve(LoadedContext loadedContext) {
        ContextCatalog.ContextEntry contextEntry = loadedContext == null ? null : loadedContext.getContextEntry();
        String runtimeHome = StringValueResolver.firstNonBlank(
            System.getenv("RPCCTL_RUNTIME_HOME"),
            contextEntry == null ? null : contextEntry.getRuntimeHome()
        );
        String runtimeBaseUrl = StringValueResolver.firstNonBlank(
            System.getenv("RPCCTL_RUNTIME_BASE_URL"),
            contextEntry == null ? null : contextEntry.getRuntimeBaseUrl(),
            defaultRuntimeBaseUrl()
        );
        String runtimeCacheDir = StringValueResolver.firstNonBlank(
            System.getenv("RPCCTL_RUNTIME_CACHE_DIR"),
            contextEntry == null ? null : contextEntry.getRuntimeCacheDir(),
            ConfigLoader.resolveXdgCacheRoot().resolve("sofa-rpcctl").resolve("runtimes").toString()
        );
        boolean autoDownloadEnabled = contextEntry == null
            || contextEntry.getAutoDownloadRuntimes() == null
            || contextEntry.getAutoDownloadRuntimes().booleanValue();
        String explicitAutoDownload = System.getenv("RPCCTL_RUNTIME_AUTO_DOWNLOAD");
        if (explicitAutoDownload != null && !explicitAutoDownload.trim().isEmpty()) {
            autoDownloadEnabled = Boolean.parseBoolean(explicitAutoDownload.trim());
        }
        return new RuntimeAccessOptions(runtimeHome, runtimeBaseUrl, runtimeCacheDir, autoDownloadEnabled);
    }

    private String defaultRuntimeBaseUrl() {
        return "https://github.com/hex1n/sofa-rpcctl/releases/download/v" + RpcCtlApplication.VERSION;
    }
}
