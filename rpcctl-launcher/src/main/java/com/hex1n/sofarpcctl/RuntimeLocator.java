package com.hex1n.sofarpcctl;

import java.io.File;
import java.net.URISyntaxException;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.CodeSource;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

public final class RuntimeLocator {

    private final RuntimeDownloader runtimeDownloader = new RuntimeDownloader();
    private final RuntimeVersionMatrix runtimeVersionMatrix = new RuntimeVersionMatrix();

    public RuntimeResolutionProbe probeRuntimeJar(String sofaRpcVersion, RuntimeAccessOptions accessOptions) {
        String fileName = "rpcctl-runtime-sofa-" + sofaRpcVersion + ".jar";
        List<Path> candidatePaths = candidates(sofaRpcVersion, fileName, accessOptions);
        File foundFile = null;
        for (Path candidate : candidatePaths) {
            if (candidate.toFile().isFile()) {
                foundFile = candidate.toFile();
                break;
            }
        }
        List<String> renderedCandidates = new ArrayList<String>(candidatePaths.size());
        for (Path candidatePath : candidatePaths) {
            renderedCandidates.add(candidatePath.toString());
        }
        return new RuntimeResolutionProbe(
            sofaRpcVersion,
            fileName,
            Collections.unmodifiableList(renderedCandidates),
            foundFile == null ? null : foundFile.getAbsolutePath(),
            accessOptions != null && accessOptions.isAutoDownloadEnabled(),
            accessOptions == null ? null : accessOptions.getRuntimeBaseUrl()
        );
    }

    public File requireRuntimeJar(String sofaRpcVersion, RuntimeAccessOptions accessOptions) {
        String fileName = "rpcctl-runtime-sofa-" + sofaRpcVersion + ".jar";
        for (Path candidate : candidates(sofaRpcVersion, fileName, accessOptions)) {
            if (candidate.toFile().isFile()) {
                return candidate.toFile();
            }
        }
        if (accessOptions != null && accessOptions.isAutoDownloadEnabled()) {
            RuntimeDownloader.DownloadResult downloadResult = runtimeDownloader.download(sofaRpcVersion, fileName, accessOptions);
            if (downloadResult.isSuccess()) {
                return downloadResult.getFile();
            }
            String failureSummary = downloadResult.summarizeFailures();
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "No SOFARPC runtime found for version " + sofaRpcVersion
                    + ". Checked local runtime locations and auto-download candidates."
                    + (failureSummary.isEmpty() ? "" : " Download attempts: " + failureSummary + ".")
                    + " Build it with ./scripts/build.sh " + sofaRpcVersion
                    + ", install a release that includes this runtime, or configure runtimeBaseUrl for auto-download."
                    + declaredSupportSuffix(sofaRpcVersion)
            );
        }
        throw new CliException(
            ExitCodes.PARAMETER_ERROR,
            "No SOFARPC runtime found for version " + sofaRpcVersion
                + ". Build it with ./scripts/build.sh " + sofaRpcVersion
                + ", install a release that includes this runtime, or configure runtimeBaseUrl for auto-download."
                + declaredSupportSuffix(sofaRpcVersion)
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

    private String declaredSupportSuffix(String sofaRpcVersion) {
        if (runtimeVersionMatrix.isDeclaredSupported(sofaRpcVersion)) {
            return "";
        }
        String supportedVersions = runtimeVersionMatrix.describeSupportedVersions();
        if (supportedVersions == null || supportedVersions.trim().isEmpty() || "unknown".equals(supportedVersions)) {
            return "";
        }
        return " Declared supported versions: " + supportedVersions + ".";
    }

    public static final class RuntimeResolutionProbe {
        private final String sofaRpcVersion;
        private final String fileName;
        private final List<String> candidates;
        private final String resolvedPath;
        private final boolean autoDownloadEnabled;
        private final String runtimeBaseUrl;

        RuntimeResolutionProbe(
            String sofaRpcVersion,
            String fileName,
            List<String> candidates,
            String resolvedPath,
            boolean autoDownloadEnabled,
            String runtimeBaseUrl
        ) {
            this.sofaRpcVersion = sofaRpcVersion;
            this.fileName = fileName;
            this.candidates = candidates;
            this.resolvedPath = resolvedPath;
            this.autoDownloadEnabled = autoDownloadEnabled;
            this.runtimeBaseUrl = runtimeBaseUrl;
        }

        public String getSofaRpcVersion() {
            return sofaRpcVersion;
        }

        public String getFileName() {
            return fileName;
        }

        public List<String> getCandidates() {
            return candidates;
        }

        public String getResolvedPath() {
            return resolvedPath;
        }

        public boolean isResolved() {
            return resolvedPath != null && !resolvedPath.trim().isEmpty();
        }

        public boolean isAutoDownloadEnabled() {
            return autoDownloadEnabled;
        }

        public String getRuntimeBaseUrl() {
            return runtimeBaseUrl;
        }
    }
}
