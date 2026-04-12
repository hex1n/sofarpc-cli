package com.hex1n.sofarpcctl;

public final class LoadedContext {

    private final RpcCtlConfig config;
    private final MetadataCatalog metadata;
    private final String configPath;
    private final String metadataPath;
    private final String manifestPath;
    private final String contextName;
    private final ContextCatalog.ContextEntry contextEntry;

    public LoadedContext(RpcCtlConfig config, MetadataCatalog metadata, String configPath, String metadataPath) {
        this(config, metadata, configPath, metadataPath, null, null, new ContextCatalog.ContextEntry());
    }

    public LoadedContext(
        RpcCtlConfig config,
        MetadataCatalog metadata,
        String configPath,
        String metadataPath,
        String manifestPath,
        String contextName,
        ContextCatalog.ContextEntry contextEntry
    ) {
        this.config = config;
        this.metadata = metadata;
        this.configPath = configPath;
        this.metadataPath = metadataPath;
        this.manifestPath = manifestPath;
        this.contextName = contextName;
        this.contextEntry = contextEntry == null ? new ContextCatalog.ContextEntry() : contextEntry;
    }

    public RpcCtlConfig getConfig() {
        return config;
    }

    public MetadataCatalog getMetadata() {
        return metadata;
    }

    public String getConfigPath() {
        return configPath;
    }

    public String getMetadataPath() {
        return metadataPath;
    }

    public String getManifestPath() {
        return manifestPath;
    }

    public String getContextName() {
        return contextName;
    }

    public ContextCatalog.ContextEntry getContextEntry() {
        return contextEntry;
    }
}
