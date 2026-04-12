package com.hex1n.sofarpcctl;

import org.apache.maven.plugin.AbstractMojo;
import org.apache.maven.plugin.MojoExecutionException;
import org.apache.maven.plugin.MojoFailureException;
import org.apache.maven.plugins.annotations.LifecyclePhase;
import org.apache.maven.plugins.annotations.Mojo;
import org.apache.maven.plugins.annotations.Parameter;

import java.io.File;
import java.io.FileOutputStream;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

@Mojo(name = "generate", defaultPhase = LifecyclePhase.GENERATE_RESOURCES)
public final class GenerateManifestMojo extends AbstractMojo {

    @Parameter(property = "basedir", required = true, readonly = true)
    private File baseDir;

    @Parameter(property = "configPath", defaultValue = "${basedir}/config/rpcctl.yaml")
    private File configPath;

    @Parameter(property = "metadataPath", defaultValue = "${basedir}/config/metadata.yaml")
    private File metadataPath;

    @Parameter(property = "output", defaultValue = "${basedir}/rpcctl-manifest.yaml")
    private File output;

    @Parameter(property = "stubPath")
    private List<String> stubPaths;

    @Parameter(property = "serviceClass")
    private List<String> serviceClasses;

    @Parameter(property = "serviceUniqueId")
    private List<String> serviceUniqueIds;

    @Parameter(property = "defaultEnv")
    private String defaultEnv;

    @Parameter(property = "sofaRpcVersion")
    private String sofaRpcVersion;

    @Parameter(property = "protocol")
    private String protocol;

    @Parameter(property = "serialization")
    private String serialization;

    @Parameter(property = "timeoutMs")
    private Integer timeoutMs;

    @Parameter(property = "force", defaultValue = "false")
    private boolean force;

    @Override
    public void execute() throws MojoExecutionException, MojoFailureException {
        if (output == null) {
            throw new MojoFailureException("output is required for manifest generation.");
        }

        if (output.exists() && !force) {
            throw new MojoFailureException("Output manifest already exists. Use -Dforce=true to overwrite.");
        }

        try {
            RpcCtlManifest manifest = buildManifest();
            File outputParent = output.getParentFile();
            if (outputParent != null && !outputParent.mkdirs() && !outputParent.isDirectory()) {
                throw new MojoFailureException("Failed to create manifest parent dir: " + outputParent.getPath());
            }
            ConfigLoader.writeYaml(output, manifest, "manifest");

            Map<String, Object> summary = new LinkedHashMap<String, Object>();
            summary.put("output", output.getAbsolutePath());
            summary.put("defaultEnv", manifest.getDefaultEnv());
            summary.put("serviceCount", manifest.getServices() == null ? 0 : manifest.getServices().size());
            summary.put("envCount", manifest.getEnvs() == null ? 0 : manifest.getEnvs().size());
            summary.put("stubPathCount", manifest.getStubPaths() == null ? 0 : manifest.getStubPaths().size());
            getLog().info(ConfigLoader.toPrettyJson(summary));
        } catch (MojoFailureException exception) {
            throw exception;
        } catch (Exception exception) {
            throw new MojoFailureException("Manifest generation failed.", exception);
        }
    }

    private RpcCtlManifest buildManifest() {
        RpcCtlManifest manifest = new RpcCtlManifest();
        if (configPath != null && configPath.isFile()) {
            RpcCtlConfig config = ConfigLoader.loadConfig(configPath.getAbsolutePath());
            manifest.setDefaultEnv(config.getDefaultEnv());
            manifest.setProtocol(config.getProtocol());
            manifest.setSerialization(config.getSerialization());
            manifest.setTimeoutMs(config.getTimeoutMs());
            manifest.setSofaRpcVersion(config.getSofaRpcVersion());
            manifest.setStubPaths(config.getStubPaths());

            Map<String, RpcCtlManifest.EnvironmentBinding> envs = new LinkedHashMap<String, RpcCtlManifest.EnvironmentBinding>();
            for (Map.Entry<String, RpcCtlConfig.EnvironmentConfig> entry : config.getEnvs().entrySet()) {
                RpcCtlManifest.EnvironmentBinding binding = new RpcCtlManifest.EnvironmentBinding();
                RpcCtlConfig.EnvironmentConfig environmentConfig = entry.getValue();
                binding.setMode(environmentConfig.getMode());
                binding.setProtocol(environmentConfig.getProtocol());
                binding.setSerialization(environmentConfig.getSerialization());
                binding.setRegistryProtocol(environmentConfig.getRegistryProtocol());
                binding.setRegistryAddress(environmentConfig.getRegistryAddress());
                binding.setDirectUrl(environmentConfig.getDirectUrl());
                binding.setTimeoutMs(environmentConfig.getTimeoutMs());
                binding.setUniqueId(environmentConfig.getUniqueId());
                binding.setSofaRpcVersion(environmentConfig.getSofaRpcVersion());
                envs.put(entry.getKey(), binding);
            }
            manifest.setEnvs(envs);
        }

        if (metadataPath != null && metadataPath.isFile()) {
            manifest.setServices(ConfigLoader.loadMetadata(metadataPath.getAbsolutePath(), true).getServices());
        }

        if (serviceClasses != null && !serviceClasses.isEmpty()) {
            List<String> resolvedStubPaths = resolvePaths(stubPaths);
            StubMetadataImporter.ImportResult importResult = new StubMetadataImporter().importServices(
                resolvedStubPaths,
                serviceClasses,
                parseUniqueIdBindings()
            );
            if (manifest.getServices() == null) {
                manifest.setServices(new HashMap<String, MetadataCatalog.ServiceMetadata>());
            }
            manifest.getServices().putAll(importResult.getServices());
        }

        if (stubPaths != null && !stubPaths.isEmpty()) {
            manifest.setStubPaths(stubPaths);
        }

        if (defaultEnv != null && !defaultEnv.trim().isEmpty()) {
            manifest.setDefaultEnv(defaultEnv.trim());
        }
        if (sofaRpcVersion != null && !sofaRpcVersion.trim().isEmpty()) {
            manifest.setSofaRpcVersion(sofaRpcVersion.trim());
        }
        if (protocol != null && !protocol.trim().isEmpty()) {
            manifest.setProtocol(protocol.trim());
        }
        if (serialization != null && !serialization.trim().isEmpty()) {
            manifest.setSerialization(serialization.trim());
        }
        if (timeoutMs != null && timeoutMs > 0) {
            manifest.setTimeoutMs(timeoutMs);
        }

        return manifest;
    }

    private List<String> resolvePaths(List<String> configuredPaths) {
        if (configuredPaths == null || configuredPaths.isEmpty()) {
            return new ArrayList<String>(0);
        }
        List<String> paths = new ArrayList<String>(configuredPaths.size());
        for (String configuredPath : configuredPaths) {
            if (configuredPath == null || configuredPath.trim().isEmpty()) {
                continue;
            }
            String trimmed = configuredPath.trim();
            File candidate = new File(trimmed);
            if (baseDir != null && !candidate.isAbsolute()) {
                candidate = new File(baseDir, trimmed);
            }
            paths.add(candidate.getAbsolutePath());
        }
        return paths;
    }

    private Map<String, String> parseUniqueIdBindings() {
        Map<String, String> bindings = new LinkedHashMap<String, String>();
        if (serviceUniqueIds == null || serviceUniqueIds.isEmpty()) {
            return bindings;
        }
        for (String rawBinding : serviceUniqueIds) {
            if (rawBinding == null || rawBinding.trim().isEmpty()) {
                continue;
            }
            String binding = rawBinding.trim();
            int separatorIndex = binding.indexOf('=');
            if (separatorIndex < 0) {
                if (serviceClasses == null || serviceClasses.size() != 1) {
                    throw new IllegalArgumentException("Bare --service-unique-id requires exactly one --service-class.");
                }
                bindings.put(serviceClasses.get(0), binding);
                continue;
            }
            String serviceClass = binding.substring(0, separatorIndex).trim();
            String uniqueId = binding.substring(separatorIndex + 1).trim();
            if (serviceClass.isEmpty() || uniqueId.isEmpty()) {
                throw new IllegalArgumentException("Invalid --service-unique-id binding: " + rawBinding);
            }
            bindings.put(serviceClass, uniqueId);
        }
        return bindings;
    }
}
