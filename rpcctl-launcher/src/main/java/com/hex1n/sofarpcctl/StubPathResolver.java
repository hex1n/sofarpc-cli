package com.hex1n.sofarpcctl;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

final class StubPathResolver {

    StubPathResolution resolve(List<String> explicitStubPaths, LoadedContext loadedContext) {
        List<String> resolvedExplicitStubPaths = resolvePathList(
            explicitStubPaths,
            PathsHolder.workingDirectorySentinel()
        );
        if (!resolvedExplicitStubPaths.isEmpty()) {
            return new StubPathResolution(resolvedExplicitStubPaths, "cli");
        }

        String envStubPath = System.getenv("RPCCTL_STUB_PATH");
        if (envStubPath != null && !envStubPath.trim().isEmpty()) {
            List<String> envPaths = new ArrayList<String>();
            String[] chunks = envStubPath.split(java.io.File.pathSeparator);
            for (String chunk : chunks) {
                if (chunk != null && !chunk.trim().isEmpty()) {
                    envPaths.add(chunk.trim());
                }
            }
            return new StubPathResolution(
                resolvePathList(envPaths, PathsHolder.workingDirectorySentinel()),
                "system-env"
            );
        }

        List<String> configuredStubPaths = loadedContext.getConfig().getStubPaths();
        if (configuredStubPaths == null || configuredStubPaths.isEmpty()) {
            return new StubPathResolution(Collections.<String>emptyList(), "none");
        }
        String basePath = loadedContext.getManifestPath() != null
            ? loadedContext.getManifestPath()
            : (loadedContext.getConfigPath() != null
                ? loadedContext.getConfigPath()
                : PathsHolder.workingDirectorySentinel());
        return new StubPathResolution(resolvePathList(configuredStubPaths, basePath), "config");
    }

    private List<String> resolvePathList(List<String> configuredPaths, String baseFilePath) {
        if (configuredPaths == null || configuredPaths.isEmpty()) {
            return Collections.emptyList();
        }
        List<String> resolved = new ArrayList<String>(configuredPaths.size());
        for (String configuredPath : configuredPaths) {
            if (configuredPath == null || configuredPath.trim().isEmpty()) {
                continue;
            }
            resolved.add(ConfigLoader.resolveOptionalPath(configuredPath, baseFilePath));
        }
        return resolved;
    }
}
