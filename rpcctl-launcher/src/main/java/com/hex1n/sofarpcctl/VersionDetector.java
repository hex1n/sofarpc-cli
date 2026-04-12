package com.hex1n.sofarpcctl;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class VersionDetector {

    private static final String DEFAULT_SOFA_RPC_VERSION = "5.4.0";
    private static final Pattern[] MAVEN_PATTERNS = new Pattern[] {
        Pattern.compile("<sofa-rpc\\.version>\\s*([^<\\s]+)\\s*</sofa-rpc\\.version>"),
        Pattern.compile("<artifactId>\\s*sofa-rpc-(?:all|bom)\\s*</artifactId>[\\s\\S]{0,400}?<version>\\s*([^<\\s]+)\\s*</version>")
    };
    private static final Pattern[] GRADLE_PATTERNS = new Pattern[] {
        Pattern.compile("(?m)^\\s*sofaRpcVersion\\s*=\\s*['\"]([^'\"]+)['\"]"),
        Pattern.compile("(?m)^\\s*sofaRpcVersion\\s*=\\s*([^\\s]+)\\s*$"),
        Pattern.compile("com\\.alipay\\.sofa:sofa-rpc-(?:all|bom):([0-9A-Za-z_.\\-]+)")
    };

    public String resolve(String explicitVersion, RpcCtlConfig config, RpcCtlConfig.EnvironmentConfig environmentConfig) {
        if (explicitVersion != null && !explicitVersion.trim().isEmpty()) {
            return explicitVersion.trim();
        }
        if (environmentConfig != null
            && environmentConfig.getSofaRpcVersion() != null
            && !environmentConfig.getSofaRpcVersion().trim().isEmpty()) {
            return environmentConfig.getSofaRpcVersion().trim();
        }
        if (config != null
            && config.getSofaRpcVersion() != null
            && !config.getSofaRpcVersion().trim().isEmpty()) {
            return config.getSofaRpcVersion().trim();
        }
        String environmentVersion = System.getenv("RPCCTL_SOFA_RPC_VERSION");
        if (environmentVersion != null && !environmentVersion.trim().isEmpty()) {
            return environmentVersion.trim();
        }

        String discovered = detectFromWorkspace(Paths.get(System.getProperty("user.dir")).toAbsolutePath().normalize());
        return discovered == null ? DEFAULT_SOFA_RPC_VERSION : discovered;
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
}
