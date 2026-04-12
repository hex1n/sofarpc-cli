package com.hex1n.sofarpcctl;

import java.io.File;

public final class RuntimeDefaults {

    private RuntimeDefaults() {
    }

    public static void prepare() {
        setSystemPropertyIfAbsent("org.slf4j.simpleLogger.defaultLogLevel", "error");
        setSystemPropertyIfAbsent("disable_middleware_digest_log", "true");
        setSystemPropertyIfAbsent("disable_digest_log", "true");
        setSystemPropertyIfAbsent("spring.application.name", "rpcctl");

        if (System.getProperty("logging.path") == null && System.getProperty("loggingRoot") == null) {
            File logDirectory = resolveDefaultLogDirectory();
            if (logDirectory.isDirectory() || logDirectory.mkdirs()) {
                System.setProperty("logging.path", logDirectory.getAbsolutePath());
            }
        }
    }

    private static File resolveDefaultLogDirectory() {
        String configuredStateHome = System.getenv("XDG_STATE_HOME");
        if (configuredStateHome != null && !configuredStateHome.trim().isEmpty()) {
            return new File(configuredStateHome.trim(), "sofa-rpcctl/logs");
        }
        String tempDirectory = System.getProperty("java.io.tmpdir");
        if (tempDirectory != null && !tempDirectory.trim().isEmpty()) {
            return new File(tempDirectory.trim(), "sofa-rpcctl/logs");
        }
        String userHome = System.getProperty("user.home");
        if (userHome != null && !userHome.trim().isEmpty()) {
            return new File(userHome.trim(), ".sofa-rpcctl/logs");
        }
        return new File(System.getProperty("java.io.tmpdir"), "sofa-rpcctl/logs");
    }

    private static void setSystemPropertyIfAbsent(String key, String value) {
        if (System.getProperty(key) == null || System.getProperty(key).trim().isEmpty()) {
            System.setProperty(key, value);
        }
    }
}
