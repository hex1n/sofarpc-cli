package com.hex1n.sofarpcctl;

import java.io.File;
import java.net.URISyntaxException;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.CodeSource;
import java.util.ArrayList;
import java.util.List;

public final class RuntimeLocator {

    private final RuntimeDownloader runtimeDownloader = new RuntimeDownloader();

    public File requireRuntimeJar(String sofaRpcVersion, RuntimeAccessOptions accessOptions) {
        String fileName = "rpcctl-runtime-sofa-" + sofaRpcVersion + ".jar";
        for (Path candidate : candidates(sofaRpcVersion, fileName, accessOptions)) {
            if (candidate.toFile().isFile()) {
                return candidate.toFile();
            }
        }
        if (accessOptions != null && accessOptions.isAutoDownloadEnabled()) {
            File downloaded = runtimeDownloader.download(sofaRpcVersion, fileName, accessOptions);
            if (downloaded != null && downloaded.isFile()) {
                return downloaded;
            }
        }
        throw new CliException(
            ExitCodes.PARAMETER_ERROR,
            "No SOFARPC runtime found for version " + sofaRpcVersion
                + ". Build it with ./scripts/build.sh " + sofaRpcVersion
                + ", install a release that includes this runtime, or configure runtimeBaseUrl for auto-download."
        );
    }

    private List<Path> candidates(String sofaRpcVersion, String fileName, RuntimeAccessOptions accessOptions) {
        List<Path> candidates = new ArrayList<Path>();
        String runtimeHome = System.getenv("RPCCTL_RUNTIME_HOME");
        if ((runtimeHome == null || runtimeHome.trim().isEmpty()) && accessOptions != null) {
            runtimeHome = accessOptions.getRuntimeHome();
        }
        if (runtimeHome != null && !runtimeHome.trim().isEmpty()) {
            Path runtimeRoot = Paths.get(runtimeHome.trim()).toAbsolutePath().normalize();
            candidates.add(runtimeRoot.resolve("sofa-rpc").resolve(sofaRpcVersion).resolve(fileName));
            candidates.add(runtimeRoot.resolve(sofaRpcVersion).resolve(fileName));
            candidates.add(runtimeRoot.resolve(fileName));
        }

        Path launcherPath = resolveLauncherPath();
        if (launcherPath != null) {
            Path launcherDir = launcherPath.toFile().isDirectory() ? launcherPath : launcherPath.getParent();
            if (launcherDir != null) {
                candidates.add(launcherDir.resolve("runtimes").resolve("sofa-rpc").resolve(sofaRpcVersion).resolve(fileName));
                Path parent = launcherDir.getParent();
                if (parent != null) {
                    candidates.add(parent.resolve("runtimes").resolve("sofa-rpc").resolve(sofaRpcVersion).resolve(fileName));
                }
            }
        }

        if (accessOptions != null && accessOptions.getRuntimeCacheDir() != null && !accessOptions.getRuntimeCacheDir().trim().isEmpty()) {
            Path cacheRoot = Paths.get(accessOptions.getRuntimeCacheDir().trim()).toAbsolutePath().normalize();
            candidates.add(cacheRoot.resolve("sofa-rpc").resolve(sofaRpcVersion).resolve(fileName));
        } else {
            candidates.add(
                ConfigLoader.resolveXdgCacheRoot()
                    .resolve("sofa-rpcctl")
                    .resolve("runtimes")
                    .resolve("sofa-rpc")
                    .resolve(sofaRpcVersion)
                    .resolve(fileName)
            );
        }
        candidates.add(Paths.get("target", "runtimes", "sofa-rpc", sofaRpcVersion, fileName).toAbsolutePath().normalize());
        return candidates;
    }

    private Path resolveLauncherPath() {
        try {
            CodeSource codeSource = RpcCtlApplication.class.getProtectionDomain().getCodeSource();
            if (codeSource == null || codeSource.getLocation() == null) {
                return null;
            }
            return Paths.get(codeSource.getLocation().toURI()).toAbsolutePath().normalize();
        } catch (URISyntaxException exception) {
            return null;
        }
    }
}
