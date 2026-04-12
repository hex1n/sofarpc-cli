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
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
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
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Metadata file not found: " + path
            );
        }
        return readYaml(file, MetadataCatalog.class, "metadata");
    }

    public static RpcCtlManifest loadManifest(String path) {
        File file = new File(path);
        if (!file.isFile()) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Manifest file not found: " + path
            );
        }
        return readYaml(file, RpcCtlManifest.class, "manifest");
    }

    public static ContextCatalog loadContexts(String path, boolean optional) {
        File file = new File(path);
        if (!file.isFile()) {
            if (optional) {
                return new ContextCatalog();
            }
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Context file not found: " + path
            );
        }
        return readYaml(file, ContextCatalog.class, "contexts");
    }

    public static ObjectMapper json() {
        return JSON;
    }

    public static String resolveDefaultManifestPath() {
        String explicitEnvPath = System.getenv("RPCCTL_MANIFEST");
        if (explicitEnvPath != null && !explicitEnvPath.trim().isEmpty()) {
            return Paths.get(explicitEnvPath.trim()).toAbsolutePath().normalize().toString();
        }
        Path workingDirectory = Paths.get(System.getProperty("user.dir")).toAbsolutePath().normalize();
        Path found = findUpwards(workingDirectory, "rpcctl-manifest.yaml");
        if (found != null) {
            return found.toString();
        }
        found = findUpwards(workingDirectory, "rpcctl-manifest.yml");
        if (found != null) {
            return found.toString();
        }

        Path xdgRoot = resolveXdgConfigRoot();
        Path xdgYaml = xdgRoot.resolve("sofa-rpcctl").resolve("rpcctl-manifest.yaml").normalize();
        if (xdgYaml.toFile().isFile()) {
            return xdgYaml.toString();
        }
        Path xdgYml = xdgRoot.resolve("sofa-rpcctl").resolve("rpcctl-manifest.yml").normalize();
        if (xdgYml.toFile().isFile()) {
            return xdgYml.toString();
        }
        return null;
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
        Path xdgConfig = resolveXdgConfigRoot()
            .resolve("sofa-rpcctl")
            .resolve("rpcctl.yaml")
            .toAbsolutePath()
            .normalize();
        if (xdgConfig.toFile().isFile()) {
            return xdgConfig.toString();
        }

        return workingDirConfig.toString();
    }

    public static String resolveDefaultContextsPath() {
        String explicitEnvPath = System.getenv("RPCCTL_CONTEXTS");
        if (explicitEnvPath != null && !explicitEnvPath.trim().isEmpty()) {
            return Paths.get(explicitEnvPath.trim()).toAbsolutePath().normalize().toString();
        }
        return resolveXdgConfigRoot()
            .resolve("sofa-rpcctl")
            .resolve("contexts.yaml")
            .toAbsolutePath()
            .normalize()
            .toString();
    }

    public static Path resolveXdgConfigRoot() {
        String explicitXdg = System.getenv("XDG_CONFIG_HOME");
        if (explicitXdg != null && !explicitXdg.trim().isEmpty()) {
            return Paths.get(explicitXdg.trim()).toAbsolutePath().normalize();
        }
        String userHome = System.getProperty("user.home");
        return Paths.get(userHome, ".config").toAbsolutePath().normalize();
    }

    public static Path resolveXdgCacheRoot() {
        String explicitXdg = System.getenv("XDG_CACHE_HOME");
        if (explicitXdg != null && !explicitXdg.trim().isEmpty()) {
            return Paths.get(explicitXdg.trim()).toAbsolutePath().normalize();
        }
        String userHome = System.getProperty("user.home");
        return Paths.get(userHome, ".cache").toAbsolutePath().normalize();
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

    public static void writeYaml(File file, Object value, String label) {
        try {
            File parent = file.getParentFile();
            if (parent != null && !parent.isDirectory() && !parent.mkdirs()) {
                throw new IOException("Failed to create directory: " + parent.getPath());
            }
            YAML.writerWithDefaultPrettyPrinter().writeValue(file, value);
        } catch (IOException exception) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Failed to write " + label + " file: " + file.getPath(),
                exception
            );
        }
    }

    private static <T> T readYaml(File file, Class<T> type, String label) {
        try {
            return YAML.readValue(file, type);
        } catch (IOException exception) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
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

    private static Path findUpwards(Path start, String fileName) {
        Path cursor = start;
        while (cursor != null) {
            Path candidate = cursor.resolve(fileName);
            if (candidate.toFile().isFile()) {
                return candidate.normalize();
            }
            cursor = cursor.getParent();
        }
        return null;
    }

}
