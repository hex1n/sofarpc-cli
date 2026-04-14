package com.hex1n.sofarpc.worker;

import java.io.IOException;
import java.io.InputStream;
import java.util.Properties;

final class RuntimeMetadata {
    private static final String VERSION_RESOURCE = "/runtime.properties";
    private static final String VERSION_KEY = "runtime.version";
    private static final String FALLBACK_VERSION = "unknown";
    private static final String runtimeVersion = loadRuntimeVersion();

    private RuntimeMetadata() {
    }

    static String runtimeVersion() {
        return runtimeVersion;
    }

    private static String loadRuntimeVersion() {
        Properties properties = new Properties();
        try (InputStream input = RuntimeMetadata.class.getResourceAsStream(VERSION_RESOURCE)) {
            if (input == null) {
                return FALLBACK_VERSION;
            }
            properties.load(input);
            String version = properties.getProperty(VERSION_KEY);
            if (version == null || version.trim().isEmpty()) {
                return FALLBACK_VERSION;
            }
            return version.trim();
        } catch (IOException ex) {
            return FALLBACK_VERSION;
        }
    }
}
