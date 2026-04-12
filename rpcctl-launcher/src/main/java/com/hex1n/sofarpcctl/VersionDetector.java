package com.hex1n.sofarpcctl;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class VersionDetector {

    private static final String DEFAULT_SOFA_RPC_VERSION = "5.4.0";
    private final RuntimeVersionMatrix runtimeVersionMatrix = new RuntimeVersionMatrix();
    private static final Pattern[] MAVEN_PATTERNS = new Pattern[] {
        Pattern.compile("<sofa-rpc\\.version>\\s*([^<\\s]+)\\s*</sofa-rpc\\.version>"),
        Pattern.compile("<artifactId>\\s*sofa-rpc-(?:all|bom)\\s*</artifactId>[\\s\\S]{0,400}?<version>\\s*([^<\\s]+)\\s*</version>")
    };
    private static final Pattern[] GRADLE_PATTERNS = new Pattern[] {
        Pattern.compile("(?m)^\\s*sofaRpcVersion\\s*=\\s*['\"]([^'\"]+)['\"]"),
        Pattern.compile("(?m)^\\s*sofaRpcVersion\\s*=\\s*([^\\s]+)\\s*$"),
        Pattern.compile("com\\.alipay\\.sofa:sofa-rpc-(?:all|bom):([0-9A-Za-z_.\\-]+)")
    };

    public VersionResolution resolve(String explicitVersion, RpcCtlConfig config, RpcCtlConfig.EnvironmentConfig environmentConfig) {
        if (explicitVersion != null && !explicitVersion.trim().isEmpty()) {
            return buildResolution(explicitVersion.trim(), "cli", false);
        }
        if (environmentConfig != null
            && environmentConfig.getSofaRpcVersion() != null
            && !environmentConfig.getSofaRpcVersion().trim().isEmpty()) {
            return buildResolution(environmentConfig.getSofaRpcVersion().trim(), "env-config", false);
        }
        if (config != null
            && config.getSofaRpcVersion() != null
            && !config.getSofaRpcVersion().trim().isEmpty()) {
            return buildResolution(config.getSofaRpcVersion().trim(), "config", false);
        }
        String environmentVersion = System.getenv("RPCCTL_SOFA_RPC_VERSION");
        if (environmentVersion != null && !environmentVersion.trim().isEmpty()) {
            return buildResolution(environmentVersion.trim(), "system-env", false);
        }

        String discovered = detectFromWorkspace(Paths.get(System.getProperty("user.dir")).toAbsolutePath().normalize());
        if (discovered != null) {
            return buildResolution(discovered, "workspace-detected", false);
        }
        return buildResolution(DEFAULT_SOFA_RPC_VERSION, "default-fallback", true);
    }

    private String detectFromWorkspace(Path start) {
        Path cursor = start;
        while (cursor != null) {
            String mavenVersion = detectFromFile(cursor.resolve("pom.xml"), MAVEN_PATTERNS);
            if (mavenVersion != null) {
                return mavenVersion;
            }

            String gradlePropertiesVersion = detectFromFile(cursor.resolve("gradle.properties"), GRADLE_PATTERNS);
            if (gradlePropertiesVersion != null) {
                return gradlePropertiesVersion;
            }

            String gradleVersion = detectFromFile(cursor.resolve("build.gradle"), GRADLE_PATTERNS);
            if (gradleVersion != null) {
                return gradleVersion;
            }

            String gradleKtsVersion = detectFromFile(cursor.resolve("build.gradle.kts"), GRADLE_PATTERNS);
            if (gradleKtsVersion != null) {
                return gradleKtsVersion;
            }
            cursor = cursor.getParent();
        }
        return null;
    }

    private String detectFromFile(Path path, Pattern[] patterns) {
        if (!Files.isRegularFile(path)) {
            return null;
        }
        try {
            String content = new String(Files.readAllBytes(path), StandardCharsets.UTF_8);
            for (Pattern pattern : patterns) {
                Matcher matcher = pattern.matcher(content);
                if (matcher.find()) {
                    return matcher.group(1).trim();
                }
            }
        } catch (IOException ignored) {
            return null;
        }
        return null;
    }

    private VersionResolution buildResolution(String resolvedVersion, String source, boolean fallbackUsed) {
        List<String> supportedVersions = runtimeVersionMatrix.listSupportedVersions();
        return new VersionResolution(
            resolvedVersion,
            source,
            fallbackUsed,
            runtimeVersionMatrix.isDeclaredSupported(resolvedVersion),
            supportedVersions
        );
    }

    public static final class VersionResolution {
        private final String resolvedVersion;
        private final String source;
        private final boolean fallbackUsed;
        private final boolean declaredSupported;
        private final List<String> supportedVersions;

        VersionResolution(
            String resolvedVersion,
            String source,
            boolean fallbackUsed,
            boolean declaredSupported,
            List<String> supportedVersions
        ) {
            this.resolvedVersion = resolvedVersion;
            this.source = source;
            this.fallbackUsed = fallbackUsed;
            this.declaredSupported = declaredSupported;
            this.supportedVersions = Collections.unmodifiableList(new ArrayList<String>(supportedVersions));
        }

        public String getResolvedVersion() {
            return resolvedVersion;
        }

        public String getSource() {
            return source;
        }

        public boolean isFallbackUsed() {
            return fallbackUsed;
        }

        public boolean isDeclaredSupported() {
            return declaredSupported;
        }

        public List<String> getSupportedVersions() {
            return supportedVersions;
        }
    }
}
