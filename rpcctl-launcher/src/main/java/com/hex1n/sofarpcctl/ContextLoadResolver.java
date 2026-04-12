package com.hex1n.sofarpcctl;

import java.io.File;

final class ContextLoadResolver {

    ContextLoadResolution resolveForInvoke(RpcCtlApplication.BaseCommand command) {
        return resolve(
            command,
            command.resolveActiveContext(),
            true
        );
    }

    ContextLoadResolution resolveForDoctor(
        RpcCtlApplication.BaseCommand command,
        ContextCatalog.ResolvedContext resolvedContext
    ) {
        return resolve(command, resolvedContext, false);
    }

    private ContextLoadResolution resolve(
        RpcCtlApplication.BaseCommand command,
        ContextCatalog.ResolvedContext resolvedContext,
        boolean explicitMetadataOptional
    ) {
        command.validateSourceOptions();
        RpcCtlApplication.SharedOptions sharedOptions = command.sharedOptions;

        if (sharedOptions.getManifestPath() != null && !sharedOptions.getManifestPath().trim().isEmpty()) {
            String manifestPath = ConfigLoader.resolveOptionalPath(
                sharedOptions.getManifestPath(),
                PathsHolder.workingDirectorySentinel()
            );
            return new ContextLoadResolution(
                command.loadManifestContext(manifestPath, resolvedContext),
                "explicit-manifest"
            );
        }

        if (sharedOptions.getConfigPath() != null) {
            return new ContextLoadResolution(
                command.loadContext(true, resolvedContext),
                "explicit-config"
            );
        }

        if (sharedOptions.getMetadataPath() != null) {
            String metadataPath = ConfigLoader.resolveOptionalPath(
                sharedOptions.getMetadataPath(),
                PathsHolder.workingDirectorySentinel()
            );
            MetadataCatalog metadata = ConfigLoader.loadMetadata(metadataPath, explicitMetadataOptional);
            return new ContextLoadResolution(
                new LoadedContext(
                    command.applyContextDefaults(new RpcCtlConfig(), resolvedContext.getEntry()),
                    metadata,
                    null,
                    metadataPath,
                    null,
                    resolvedContext.getName(),
                    resolvedContext.getEntry()
                ),
                "explicit-metadata"
            );
        }

        if (resolvedContext.getEntry() != null
            && resolvedContext.getEntry().getManifestPath() != null
            && !resolvedContext.getEntry().getManifestPath().trim().isEmpty()) {
            String manifestPath = ConfigLoader.resolveOptionalPath(
                resolvedContext.getEntry().getManifestPath(),
                PathsHolder.workingDirectorySentinel()
            );
            return new ContextLoadResolution(
                command.loadManifestContext(manifestPath, resolvedContext),
                "context-manifest"
            );
        }

        String discoveredManifestPath = ConfigLoader.resolveDefaultManifestPath();
        if (discoveredManifestPath != null && new File(discoveredManifestPath).isFile()) {
            return new ContextLoadResolution(
                command.loadManifestContext(discoveredManifestPath, resolvedContext),
                "discovered-manifest"
            );
        }

        String defaultConfigPath = ConfigLoader.resolveDefaultConfigPath();
        if (defaultConfigPath != null && new File(defaultConfigPath).isFile()) {
            return new ContextLoadResolution(
                command.loadContext(true, resolvedContext),
                "discovered-config"
            );
        }

        return new ContextLoadResolution(
            new LoadedContext(
                command.applyContextDefaults(new RpcCtlConfig(), resolvedContext.getEntry()),
                new MetadataCatalog(),
                null,
                null,
                null,
                resolvedContext.getName(),
                resolvedContext.getEntry()
            ),
            "empty-defaults"
        );
    }
}
