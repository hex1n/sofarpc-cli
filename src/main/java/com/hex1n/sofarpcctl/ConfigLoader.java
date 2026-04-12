package com.hex1n.sofarpcctl;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;

import java.io.File;
import java.io.IOException;
import java.nio.file.Path;
import java.nio.file.Paths;

public final class ConfigLoader {

    private static final ObjectMapper JSON = buildJsonMapper();
    private static final ObjectMapper YAML = buildYamlMapper();

    private ConfigLoader() {
    }

    public static RpcCtlConfig loadConfig(String path) {
        File file = new File(path);
        if (!file.isFile()) {
            throw new RpcCtlApplication.CliException(
                RpcCtlApplication.ExitCodes.PARAMETER_ERROR,
                "Config file not found: " + path
            );
        }
        return readYaml(file, RpcCtlConfig.class, "config");
    }

    public static MetadataCatalog loadMetadata(String path, boolean optional) {
        File file = new File(path);
        if (!file.isFile()) {
            if (optional) {
                return new MetadataCatalog();
            }
            throw new RpcCtlApplication.CliException(
                RpcCtlApplication.ExitCodes.PARAMETER_ERROR,
                "Metadata file not found: " + path
            );
        }
        return readYaml(file, MetadataCatalog.class, "metadata");
    }

    public static ObjectMapper json() {
        return JSON;
    }

    public static String resolveDefaultConfigPath() {
        String explicitEnvPath = System.getenv("RPCCTL_CONFIG");
        if (explicitEnvPath != null && !explicitEnvPath.trim().isEmpty()) {
            return explicitEnvPath.trim();
        }

        Path workingDirConfig = Paths.get("config", "rpcctl.yaml").toAbsolutePath().normalize();
        if (workingDirConfig.toFile().isFile()) {
            return workingDirConfig.toString();
        }

        String userHome = System.getProperty("user.home");
        Path xdgConfig = Paths.get(
            System.getenv("XDG_CONFIG_HOME") != null && !System.getenv("XDG_CONFIG_HOME").trim().isEmpty()
                ? System.getenv("XDG_CONFIG_HOME").trim()
                : Paths.get(userHome, ".config").toString(),
            "sofa-rpcctl",
            "rpcctl.yaml"
        ).toAbsolutePath().normalize();
        if (xdgConfig.toFile().isFile()) {
            return xdgConfig.toString();
        }

        return workingDirConfig.toString();
    }

    public static String resolveOptionalPath(String configuredPath, String baseFilePath) {
        if (configuredPath == null || configuredPath.trim().isEmpty()) {
            return configuredPath;
        }
        Path candidate = Paths.get(configuredPath.trim());
        if (candidate.isAbsolute()) {
            return candidate.normalize().toString();
        }
        Path base = Paths.get(baseFilePath).toAbsolutePath().normalize();
        Path basePath = base.toFile().isDirectory() ? base : base.getParent();
        if (basePath == null) {
            return candidate.toAbsolutePath().normalize().toString();
        }
        return basePath.resolve(candidate).normalize().toString();
    }

    public static String toPrettyJson(Object value) {
        try {
            return JSON.writerWithDefaultPrettyPrinter().writeValueAsString(value);
        } catch (JsonProcessingException exception) {
            throw new IllegalStateException("Failed to render JSON", exception);
        }
    }

    private static <T> T readYaml(File file, Class<T> type, String label) {
        try {
            return YAML.readValue(file, type);
        } catch (IOException exception) {
            throw new RpcCtlApplication.CliException(
                RpcCtlApplication.ExitCodes.PARAMETER_ERROR,
                "Failed to read " + label + " file: " + file.getPath(),
                exception
            );
        }
    }

    private static ObjectMapper buildJsonMapper() {
        ObjectMapper objectMapper = new ObjectMapper();
        objectMapper.configure(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, false);
        objectMapper.configure(SerializationFeature.FAIL_ON_EMPTY_BEANS, false);
        return objectMapper;
    }

    private static ObjectMapper buildYamlMapper() {
        ObjectMapper objectMapper = new ObjectMapper(new YAMLFactory());
        objectMapper.configure(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, false);
        objectMapper.configure(SerializationFeature.FAIL_ON_EMPTY_BEANS, false);
        return objectMapper;
    }
}
