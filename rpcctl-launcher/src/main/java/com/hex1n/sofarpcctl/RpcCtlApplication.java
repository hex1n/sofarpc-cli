package com.hex1n.sofarpcctl;

import picocli.CommandLine;
import picocli.CommandLine.Command;
import picocli.CommandLine.Mixin;
import picocli.CommandLine.Option;
import picocli.CommandLine.ParameterException;

import java.io.File;
import java.io.PrintWriter;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Callable;

public final class RpcCtlApplication {

    static final String VERSION = "0.1.0";

    private RpcCtlApplication() {
    }

    public static void main(String[] args) {
        CommandLine commandLine = new CommandLine(new RootCommand());
        commandLine.addSubcommand("invoke", new InvokeCommand());
        commandLine.addSubcommand("call", new CallCommand());
        commandLine.addSubcommand("list", new ListCommand());
        commandLine.addSubcommand("describe", new DescribeCommand());
        commandLine.addSubcommand("context", buildContextCommand());
        commandLine.addSubcommand("manifest", buildManifestCommand());
        commandLine.setParameterExceptionHandler(new FriendlyParameterExceptionHandler());
        commandLine.setExecutionExceptionHandler((exception, cmd, parseResult) -> {
            if (exception instanceof CliException) {
                CliException cliException = (CliException) exception;
                cmd.getErr().println(cliException.getMessage());
                return cliException.getExitCode();
            }
            cmd.getErr().println("Unexpected error: " + exception.getMessage());
            return ExitCodes.RPC_ERROR;
        });
        int exitCode = commandLine.execute(args);
        System.exit(exitCode);
    }

    private static CommandLine buildContextCommand() {
        CommandLine commandLine = new CommandLine(new ContextRootCommand());
        commandLine.addSubcommand("list", new ContextListCommand());
        commandLine.addSubcommand("show", new ContextShowCommand());
        commandLine.addSubcommand("use", new ContextUseCommand());
        commandLine.addSubcommand("set", new ContextSetCommand());
        commandLine.addSubcommand("delete", new ContextDeleteCommand());
        return commandLine;
    }

    private static CommandLine buildManifestCommand() {
        CommandLine commandLine = new CommandLine(new ManifestRootCommand());
        commandLine.addSubcommand("generate", new ManifestGenerateCommand());
        commandLine.addSubcommand("init", new ManifestGenerateCommand());
        return commandLine;
    }

    @Command(
        name = "rpcctl",
        mixinStandardHelpOptions = true,
        version = VERSION,
        description = "Portable SOFABoot / SOFARPC command line invoker."
    )
    static final class RootCommand implements Runnable {
        @Override
        public void run() {
            System.out.println("Usage: rpcctl <invoke|call|list|describe|context|manifest> [options]");
            System.out.println("Run 'rpcctl --help' or 'rpcctl <subcommand> --help' for details.");
        }
    }

    static final class FriendlyParameterExceptionHandler implements CommandLine.IParameterExceptionHandler {
        @Override
        public int handleParseException(ParameterException exception, String[] args) {
            PrintWriter writer = exception.getCommandLine().getErr();
            writer.println(exception.getMessage());
            writer.println();
            exception.getCommandLine().usage(writer);
            return ExitCodes.PARAMETER_ERROR;
        }
    }

    static final class SharedOptions {
        @Option(names = {"-p", "--profile", "--context"},
            description = "Named user context/profile from ~/.config/sofa-rpcctl/contexts.yaml.")
        private String contextName;

        @Option(names = "--manifest",
            description = "Path to project manifest YAML. Defaults to RPCCTL_MANIFEST or rpcctl-manifest.yaml discovered upward from the current directory.")
        private String manifestPath;

        @Option(names = {"-c", "--config"},
            description = "Path to rpcctl config YAML. Defaults to RPCCTL_CONFIG, ./config/rpcctl.yaml, or ~/.config/sofa-rpcctl/rpcctl.yaml.")
        private String configPath;

        @Option(names = {"-m", "--metadata"},
            description = "Path to metadata YAML. Defaults to metadataPath from config or config/metadata.yaml.")
        private String metadataPath;
    }

    static abstract class BaseCommand implements Callable<Integer> {
        @Mixin
        protected SharedOptions sharedOptions;

        protected ContextCatalog loadContextCatalog(boolean optional) {
            return ConfigLoader.loadContexts(ConfigLoader.resolveDefaultContextsPath(), optional);
        }

        protected ContextCatalog.ResolvedContext resolveActiveContext() {
            ContextCatalog catalog = loadContextCatalog(true);
            return catalog.resolveSelected(sharedOptions.contextName);
        }

        protected LoadedContext loadManifestContext(String manifestPath, ContextCatalog.ResolvedContext resolvedContext) {
            RpcCtlManifest manifest = ConfigLoader.loadManifest(manifestPath);
            RpcCtlConfig config = applyContextDefaults(manifest.toConfig(), resolvedContext.getEntry());
            return new LoadedContext(
                config,
                manifest.toMetadata(),
                null,
                null,
                manifestPath,
                resolvedContext.getName(),
                resolvedContext.getEntry()
            );
        }

        protected LoadedContext loadContext(boolean metadataOptional, ContextCatalog.ResolvedContext resolvedContext) {
            String configPath = sharedOptions.configPath != null
                ? ConfigLoader.resolveOptionalPath(sharedOptions.configPath, PathsHolder.workingDirectorySentinel())
                : ConfigLoader.resolveDefaultConfigPath();
            RpcCtlConfig config = applyContextDefaults(ConfigLoader.loadConfig(configPath), resolvedContext.getEntry());
            String metadataPath = sharedOptions.metadataPath != null
                ? ConfigLoader.resolveOptionalPath(sharedOptions.metadataPath, configPath)
                : ConfigLoader.resolveOptionalPath(config.getMetadataPath(), configPath);
            MetadataCatalog metadata = ConfigLoader.loadMetadata(metadataPath, metadataOptional);
            return new LoadedContext(
                config,
                metadata,
                configPath,
                metadataPath,
                null,
                resolvedContext.getName(),
                resolvedContext.getEntry()
            );
        }

        protected void validateSourceOptions() {
            boolean hasManifest = sharedOptions.manifestPath != null && !sharedOptions.manifestPath.trim().isEmpty();
            boolean hasConfig = sharedOptions.configPath != null && !sharedOptions.configPath.trim().isEmpty();
            boolean hasMetadata = sharedOptions.metadataPath != null && !sharedOptions.metadataPath.trim().isEmpty();
            if (hasManifest && (hasConfig || hasMetadata)) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Use --manifest by itself, or use --config/--metadata. Do not mix them."
                );
            }
        }

        protected LoadedContext loadContextForMetadataCommands() {
            validateSourceOptions();
            ContextCatalog.ResolvedContext resolvedContext = resolveActiveContext();
            if (sharedOptions.manifestPath != null && !sharedOptions.manifestPath.trim().isEmpty()) {
                String manifestPath = ConfigLoader.resolveOptionalPath(
                    sharedOptions.manifestPath,
                    PathsHolder.workingDirectorySentinel()
                );
                return loadManifestContext(manifestPath, resolvedContext);
            }

            if (sharedOptions.configPath != null) {
                return loadContext(false, resolvedContext);
            }

            if (sharedOptions.metadataPath != null) {
                String metadataPath = ConfigLoader.resolveOptionalPath(
                    sharedOptions.metadataPath,
                    PathsHolder.workingDirectorySentinel()
                );
                MetadataCatalog metadata = ConfigLoader.loadMetadata(metadataPath, false);
                return new LoadedContext(
                    applyContextDefaults(new RpcCtlConfig(), resolvedContext.getEntry()),
                    metadata,
                    null,
                    metadataPath,
                    null,
                    resolvedContext.getName(),
                    resolvedContext.getEntry()
                );
            }

            if (resolvedContext.getEntry() != null
                && resolvedContext.getEntry().getManifestPath() != null
                && !resolvedContext.getEntry().getManifestPath().trim().isEmpty()) {
                String manifestPath = ConfigLoader.resolveOptionalPath(
                    resolvedContext.getEntry().getManifestPath(),
                    PathsHolder.workingDirectorySentinel()
                );
                return loadManifestContext(manifestPath, resolvedContext);
            }

            String discoveredManifestPath = ConfigLoader.resolveDefaultManifestPath();
            if (discoveredManifestPath != null && new File(discoveredManifestPath).isFile()) {
                return loadManifestContext(discoveredManifestPath, resolvedContext);
            }

            String defaultConfigPath = ConfigLoader.resolveDefaultConfigPath();
            if (defaultConfigPath != null && new File(defaultConfigPath).isFile()) {
                return loadContext(false, resolvedContext);
            }

            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "No metadata catalog found. Pass --metadata, add rpcctl-manifest.yaml in the project or ~/.config/sofa-rpcctl/, or create ~/.config/sofa-rpcctl/rpcctl.yaml."
            );
        }

        protected void printJson(Object payload) {
            System.out.println(ConfigLoader.toPrettyJson(payload));
        }

        protected RpcCtlConfig applyContextDefaults(
            RpcCtlConfig config,
            ContextCatalog.ContextEntry contextEntry
        ) {
            RpcCtlConfig merged = new RpcCtlConfig();
            merged.setMetadataPath(config.getMetadataPath());
            merged.setDefaultEnv(firstNonBlank(
                contextEntry == null ? null : contextEntry.getEnv(),
                config.getDefaultEnv()
            ));
            merged.setProtocol(firstNonBlank(
                contextEntry == null ? null : contextEntry.getProtocol(),
                config.getProtocol(),
                "bolt"
            ));
            merged.setSerialization(firstNonBlank(
                contextEntry == null ? null : contextEntry.getSerialization(),
                config.getSerialization(),
                "hessian2"
            ));
            merged.setTimeoutMs(contextEntry != null && contextEntry.getTimeoutMs() != null
                ? contextEntry.getTimeoutMs()
                : config.getTimeoutMs());
            merged.setSofaRpcVersion(firstNonBlank(
                contextEntry == null ? null : contextEntry.getSofaRpcVersion(),
                config.getSofaRpcVersion()
            ));
            merged.setEnvs(config.getEnvs());
            return merged;
        }

        protected String firstNonBlank(String... values) {
            if (values == null) {
                return null;
            }
            for (String value : values) {
                if (value != null && !value.trim().isEmpty()) {
                    return value.trim();
                }
            }
            return null;
        }
    }

    @Command(name = "invoke", mixinStandardHelpOptions = true, description = "Invoke one SOFARPC method.")
    static class InvokeCommand extends BaseCommand {
        protected final VersionDetector versionDetector = new VersionDetector();
        protected final ProcessRuntimeInvoker runtimeInvoker = new ProcessRuntimeInvoker();

        @Option(names = "--env", description = "Environment name from config. Optional when target flags are passed inline.")
        protected String environmentName;

        @Option(names = "--direct-url", description = "Direct SOFARPC target, for example bolt://127.0.0.1:12200")
        protected String directUrl;

        @Option(names = "--registry", description = "Registry address, for example 127.0.0.1:2181 or zookeeper://127.0.0.1:2181")
        protected String registryAddress;

        @Option(names = "--registry-protocol", description = "Registry protocol when --registry has no URI scheme, for example zookeeper or sofa")
        protected String registryProtocol;

        @Option(names = "--protocol", description = "SOFARPC protocol. Defaults to manifest/config value, then bolt.")
        protected String protocol;

        @Option(names = "--serialization", description = "Serialization name. Defaults to manifest/config value, then hessian2.")
        protected String serialization;

        @Option(names = "--timeout-ms", description = "Timeout in milliseconds. Defaults to manifest/config value, then 3000.")
        protected Integer timeoutMs;

        @Option(names = "--sofa-rpc-version", description = "Explicit SOFARPC client version. When omitted, rpcctl will infer it from the current project or selected environment.")
        protected String sofaRpcVersion;

        @Option(names = "--service", required = true, description = "Fully qualified service interface name.")
        protected String serviceName;

        @Option(names = "--method", required = true, description = "Method name to invoke.")
        protected String methodName;

        @Option(names = "--types",
            description = "Comma-separated parameter types. Omit this when metadata already provides them.")
        protected String rawTypes;

        @Option(names = "--args", defaultValue = "[]", description = "JSON array of method arguments.")
        protected String argsJson;

        @Option(names = "--unique-id", description = "SOFARPC uniqueId override.")
        protected String uniqueId;

        @Option(names = "--confirm", description = "Required for metadata methods marked as write or dangerous.")
        protected boolean confirm;

        @Override
        public Integer call() {
            LoadedContext loadedContext = loadContextForInvoke();
            RpcCtlConfig.EnvironmentConfig environmentConfig = resolveEnvironmentConfig(loadedContext);
            MetadataCatalog.ServiceMetadata serviceMetadata = loadedContext.getMetadata().getService(serviceName);
            MetadataCatalog.MethodMetadata methodMetadata = serviceMetadata == null ? null : serviceMetadata.getMethod(methodName);

            List<String> paramTypes = resolveParamTypes(methodMetadata);
            String resolvedUniqueId = resolveUniqueId(environmentConfig, serviceMetadata);
            enforceRiskConfirmation(methodMetadata);

            String resolvedVersion = versionDetector.resolve(sofaRpcVersion, loadedContext.getConfig(), environmentConfig);
            RuntimeInvocationRequest request = new RuntimeInvocationRequest();
            request.setEnvironmentName(resolveEffectiveEnvironmentName(loadedContext));
            request.setEnvironmentConfig(environmentConfig);
            request.setServiceName(serviceName);
            request.setUniqueId(resolvedUniqueId);
            request.setMethodName(methodName);
            request.setParamTypes(paramTypes);
            request.setArgsJson(argsJson);

            RuntimeInvocationResult result;
            long startTime = System.currentTimeMillis();
            try {
                result = runtimeInvoker.invoke(
                    resolvedVersion,
                    request,
                    resolveRuntimeAccessOptions(loadedContext)
                );
            } catch (CliException exception) {
                result = RuntimeInvocationResult.failure(
                    request.getEnvironmentName(),
                    environmentConfig.getMode(),
                    serviceName,
                    resolvedUniqueId,
                    methodName,
                    paramTypes,
                    false,
                    System.currentTimeMillis() - startTime,
                    classifyLauncherError(exception),
                    exception.getMessage()
                );
                applyFailureHints(result, exception);
                printJson(result);
                return exception.getExitCode();
            }
            printJson(result);
            return result.isSuccess() ? ExitCodes.SUCCESS : ExitCodes.RPC_ERROR;
        }

        private LoadedContext loadContextForInvoke() {
            validateSourceOptions();
            ContextCatalog.ResolvedContext resolvedContext = resolveActiveContext();
            if (sharedOptions.manifestPath != null && !sharedOptions.manifestPath.trim().isEmpty()) {
                String manifestPath = ConfigLoader.resolveOptionalPath(
                    sharedOptions.manifestPath,
                    PathsHolder.workingDirectorySentinel()
                );
                return loadManifestContext(manifestPath, resolvedContext);
            }

            if (sharedOptions.configPath != null) {
                return loadContext(true, resolvedContext);
            }

            if (sharedOptions.metadataPath != null) {
                String metadataPath = ConfigLoader.resolveOptionalPath(
                    sharedOptions.metadataPath,
                    PathsHolder.workingDirectorySentinel()
                );
                MetadataCatalog metadata = ConfigLoader.loadMetadata(metadataPath, true);
                return new LoadedContext(
                    applyContextDefaults(new RpcCtlConfig(), resolvedContext.getEntry()),
                    metadata,
                    null,
                    metadataPath,
                    null,
                    resolvedContext.getName(),
                    resolvedContext.getEntry()
                );
            }

            if (resolvedContext.getEntry() != null
                && resolvedContext.getEntry().getManifestPath() != null
                && !resolvedContext.getEntry().getManifestPath().trim().isEmpty()) {
                String manifestPath = ConfigLoader.resolveOptionalPath(
                    resolvedContext.getEntry().getManifestPath(),
                    PathsHolder.workingDirectorySentinel()
                );
                return loadManifestContext(manifestPath, resolvedContext);
            }

            String discoveredManifestPath = ConfigLoader.resolveDefaultManifestPath();
            if (discoveredManifestPath != null && new File(discoveredManifestPath).isFile()) {
                return loadManifestContext(discoveredManifestPath, resolvedContext);
            }

            String defaultConfigPath = ConfigLoader.resolveDefaultConfigPath();
            if (defaultConfigPath != null && new File(defaultConfigPath).isFile()) {
                return loadContext(true, resolvedContext);
            }

            return new LoadedContext(
                applyContextDefaults(new RpcCtlConfig(), resolvedContext.getEntry()),
                new MetadataCatalog(),
                null,
                null,
                null,
                resolvedContext.getName(),
                resolvedContext.getEntry()
            );
        }

        private RpcCtlConfig.EnvironmentConfig resolveEnvironmentConfig(LoadedContext loadedContext) {
            boolean hasInlineDirect = directUrl != null && !directUrl.trim().isEmpty();
            boolean hasInlineRegistry = registryAddress != null && !registryAddress.trim().isEmpty();
            boolean hasInlineTarget = hasInlineDirect || hasInlineRegistry;

            if (environmentName != null && !environmentName.trim().isEmpty()) {
                if (hasInlineTarget) {
                    throw new CliException(
                        ExitCodes.PARAMETER_ERROR,
                        "Use either --env or inline target flags, not both."
                    );
                }
                return loadedContext.getConfig().requireEnv(environmentName.trim());
            }

            if (!hasInlineTarget) {
                String contextEnv = loadedContext.getContextEntry() == null ? null : loadedContext.getContextEntry().getEnv();
                if (contextEnv != null && !contextEnv.trim().isEmpty()) {
                    return loadedContext.getConfig().requireEnv(contextEnv.trim());
                }
                String defaultEnv = loadedContext.getConfig().getDefaultEnv();
                if (defaultEnv != null && !defaultEnv.trim().isEmpty()) {
                    return loadedContext.getConfig().requireEnv(defaultEnv.trim());
                }
                RpcCtlConfig.EnvironmentConfig contextTarget = resolveContextTarget(loadedContext);
                if (contextTarget != null) {
                    return contextTarget;
                }
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Missing target. Use --env, rely on defaultEnv from manifest/config/context, or pass --direct-url / --registry inline."
                );
            }
            if (hasInlineDirect && hasInlineRegistry) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Use either --direct-url or --registry, not both."
                );
            }

            RpcCtlConfig.EnvironmentConfig environmentConfig = new RpcCtlConfig.EnvironmentConfig();
            environmentConfig.setProtocol(protocol);
            environmentConfig.setSerialization(serialization);
            environmentConfig.setTimeoutMs(timeoutMs);
            if (hasInlineDirect) {
                environmentConfig.setMode("direct");
                environmentConfig.setDirectUrl(directUrl.trim());
            } else {
                environmentConfig.setMode("registry");
                environmentConfig.setRegistryAddress(registryAddress.trim());
                if (registryProtocol != null && !registryProtocol.trim().isEmpty()) {
                    environmentConfig.setRegistryProtocol(registryProtocol.trim());
                }
            }
            return loadedContext.getConfig().applyDefaults(environmentConfig);
        }

        private RpcCtlConfig.EnvironmentConfig resolveContextTarget(LoadedContext loadedContext) {
            ContextCatalog.ContextEntry contextEntry = loadedContext.getContextEntry();
            if (contextEntry == null || contextEntry.isEmpty()) {
                return null;
            }
            if (contextEntry.getDirectUrl() != null && !contextEntry.getDirectUrl().trim().isEmpty()) {
                RpcCtlConfig.EnvironmentConfig environmentConfig = new RpcCtlConfig.EnvironmentConfig();
                environmentConfig.setMode("direct");
                environmentConfig.setDirectUrl(contextEntry.getDirectUrl());
                environmentConfig.setProtocol(contextEntry.getProtocol());
                environmentConfig.setSerialization(contextEntry.getSerialization());
                environmentConfig.setTimeoutMs(contextEntry.getTimeoutMs());
                environmentConfig.setSofaRpcVersion(contextEntry.getSofaRpcVersion());
                return loadedContext.getConfig().applyDefaults(environmentConfig);
            }
            if (contextEntry.getRegistryAddress() != null && !contextEntry.getRegistryAddress().trim().isEmpty()) {
                RpcCtlConfig.EnvironmentConfig environmentConfig = new RpcCtlConfig.EnvironmentConfig();
                environmentConfig.setMode("registry");
                environmentConfig.setRegistryAddress(contextEntry.getRegistryAddress());
                environmentConfig.setRegistryProtocol(contextEntry.getRegistryProtocol());
                environmentConfig.setProtocol(contextEntry.getProtocol());
                environmentConfig.setSerialization(contextEntry.getSerialization());
                environmentConfig.setTimeoutMs(contextEntry.getTimeoutMs());
                environmentConfig.setSofaRpcVersion(contextEntry.getSofaRpcVersion());
                return loadedContext.getConfig().applyDefaults(environmentConfig);
            }
            return null;
        }

        private List<String> resolveParamTypes(MetadataCatalog.MethodMetadata methodMetadata) {
            List<String> explicitTypes = TypeNameUtils.parseTypes(rawTypes);
            if (!explicitTypes.isEmpty()) {
                return explicitTypes;
            }
            if (methodMetadata != null && methodMetadata.getParamTypes() != null) {
                return new ArrayList<String>(methodMetadata.getParamTypes());
            }
            return new ArrayList<String>();
        }

        private String resolveUniqueId(
            RpcCtlConfig.EnvironmentConfig environmentConfig,
            MetadataCatalog.ServiceMetadata serviceMetadata
        ) {
            if (uniqueId != null && !uniqueId.trim().isEmpty()) {
                return uniqueId;
            }
            if (serviceMetadata != null && serviceMetadata.getUniqueId() != null) {
                return serviceMetadata.getUniqueId();
            }
            return environmentConfig.getUniqueId();
        }

        private void enforceRiskConfirmation(MetadataCatalog.MethodMetadata methodMetadata) {
            if (methodMetadata == null || methodMetadata.getRisk() == null) {
                return;
            }
            String normalizedRisk = methodMetadata.getRisk().trim().toLowerCase();
            if (("write".equals(normalizedRisk) || "dangerous".equals(normalizedRisk)) && !confirm) {
                throw new CliException(
                    ExitCodes.POLICY_DENIED,
                    "Method is marked as " + normalizedRisk + ". Re-run with --confirm."
                );
            }
        }

        private String classifyLauncherError(CliException exception) {
            if (exception.getExitCode() == ExitCodes.PARAMETER_ERROR) {
                return "RUNTIME_SETUP_ERROR";
            }
            if (exception.getExitCode() == ExitCodes.POLICY_DENIED) {
                return "POLICY_DENIED";
            }
            return "RUNTIME_ERROR";
        }

        private String resolveEffectiveEnvironmentName(LoadedContext loadedContext) {
            if ((directUrl != null && !directUrl.trim().isEmpty())
                || (registryAddress != null && !registryAddress.trim().isEmpty())) {
                return null;
            }
            if (environmentName != null && !environmentName.trim().isEmpty()) {
                return environmentName.trim();
            }
            if (loadedContext.getContextEntry() != null
                && loadedContext.getContextEntry().getEnv() != null
                && !loadedContext.getContextEntry().getEnv().trim().isEmpty()) {
                return loadedContext.getContextEntry().getEnv().trim();
            }
            if (loadedContext.getConfig().getDefaultEnv() != null && !loadedContext.getConfig().getDefaultEnv().trim().isEmpty()) {
                return loadedContext.getConfig().getDefaultEnv().trim();
            }
            return null;
        }

        private RuntimeAccessOptions resolveRuntimeAccessOptions(LoadedContext loadedContext) {
            String runtimeHome = firstNonBlank(
                System.getenv("RPCCTL_RUNTIME_HOME"),
                loadedContext.getContextEntry() == null ? null : loadedContext.getContextEntry().getRuntimeHome()
            );
            String runtimeBaseUrl = firstNonBlank(
                System.getenv("RPCCTL_RUNTIME_BASE_URL"),
                loadedContext.getContextEntry() == null ? null : loadedContext.getContextEntry().getRuntimeBaseUrl(),
                defaultRuntimeBaseUrl()
            );
            String runtimeCacheDir = firstNonBlank(
                System.getenv("RPCCTL_RUNTIME_CACHE_DIR"),
                loadedContext.getContextEntry() == null ? null : loadedContext.getContextEntry().getRuntimeCacheDir(),
                ConfigLoader.resolveXdgCacheRoot().resolve("sofa-rpcctl").resolve("runtimes").toString()
            );
            boolean autoDownloadEnabled = loadedContext.getContextEntry() == null
                || loadedContext.getContextEntry().getAutoDownloadRuntimes() == null
                || loadedContext.getContextEntry().getAutoDownloadRuntimes().booleanValue();
            String explicitAutoDownload = System.getenv("RPCCTL_RUNTIME_AUTO_DOWNLOAD");
            if (explicitAutoDownload != null && !explicitAutoDownload.trim().isEmpty()) {
                autoDownloadEnabled = Boolean.parseBoolean(explicitAutoDownload.trim());
            }
            return new RuntimeAccessOptions(runtimeHome, runtimeBaseUrl, runtimeCacheDir, autoDownloadEnabled);
        }

        private void applyFailureHints(RuntimeInvocationResult result, CliException exception) {
            String message = exception.getMessage() == null ? "" : exception.getMessage();
            if (message.contains("No SOFARPC runtime found")) {
                result.setHint("Set --sofa-rpc-version, configure runtimeBaseUrl via context, or build/install the matching runtime.");
            } else if (message.toLowerCase().contains("timed out")) {
                result.setHint("Check registry/provider reachability, uniqueId, and timeout settings.");
            } else if (message.contains("Missing target")) {
                result.setHint("Use a context/defaultEnv, or pass --direct-url / --registry inline.");
            }
        }

        private String defaultRuntimeBaseUrl() {
            return "https://github.com/hex1n/sofa-rpcctl/releases/download/v" + VERSION;
        }
    }

    @Command(name = "call", mixinStandardHelpOptions = true, description = "Short syntax for invoke. Example: rpcctl call test-zk::com.foo.UserService/getUser '[123]'")
    static final class CallCommand extends BaseCommand {
        @CommandLine.Parameters(index = "0", description = "Endpoint spec: <service>/<method> or <env>::<service>/<method>.")
        private String endpoint;

        @CommandLine.Parameters(index = "1", arity = "0..1", description = "JSON array of arguments. Defaults to []")
        private String positionalArgsJson = "[]";

        @Option(names = "--env", description = "Environment name override.")
        private String environmentName;

        @Option(names = "--direct-url", description = "Direct SOFARPC target, for example bolt://127.0.0.1:12200")
        private String directUrl;

        @Option(names = "--registry", description = "Registry address, for example zookeeper://127.0.0.1:2181")
        private String registryAddress;

        @Option(names = "--registry-protocol", description = "Registry protocol when --registry has no URI scheme.")
        private String registryProtocol;

        @Option(names = "--protocol", description = "SOFARPC protocol override.")
        private String protocol;

        @Option(names = "--serialization", description = "Serialization override.")
        private String serialization;

        @Option(names = "--timeout-ms", description = "Timeout override.")
        private Integer timeoutMs;

        @Option(names = "--sofa-rpc-version", description = "SOFARPC client version override.")
        private String sofaRpcVersion;

        @Option(names = "--types", description = "Comma-separated parameter types override.")
        private String rawTypes;

        @Option(names = "--unique-id", description = "SOFARPC uniqueId override.")
        private String uniqueId;

        @Option(names = "--confirm", description = "Required for write or dangerous methods.")
        private boolean confirm;

        @Override
        public Integer call() {
            ParsedEndpoint parsedEndpoint = parseEndpoint(endpoint);
            if (environmentName != null && parsedEndpoint.environmentName != null) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Use either --env or env::service/method in call syntax, not both."
                );
            }

            InvokeCommand invokeCommand = new InvokeCommand();
            invokeCommand.sharedOptions = sharedOptions;
            invokeCommand.environmentName = environmentName != null ? environmentName : parsedEndpoint.environmentName;
            invokeCommand.directUrl = directUrl;
            invokeCommand.registryAddress = registryAddress;
            invokeCommand.registryProtocol = registryProtocol;
            invokeCommand.protocol = protocol;
            invokeCommand.serialization = serialization;
            invokeCommand.timeoutMs = timeoutMs;
            invokeCommand.sofaRpcVersion = sofaRpcVersion;
            invokeCommand.serviceName = parsedEndpoint.serviceName;
            invokeCommand.methodName = parsedEndpoint.methodName;
            invokeCommand.rawTypes = rawTypes;
            invokeCommand.argsJson = positionalArgsJson;
            invokeCommand.uniqueId = uniqueId;
            invokeCommand.confirm = confirm;
            return invokeCommand.call();
        }

        private ParsedEndpoint parseEndpoint(String rawEndpoint) {
            String candidate = rawEndpoint == null ? "" : rawEndpoint.trim();
            if (candidate.isEmpty()) {
                throw new CliException(ExitCodes.PARAMETER_ERROR, "Missing call endpoint.");
            }

            String envName = null;
            int envSeparator = candidate.indexOf("::");
            if (envSeparator >= 0) {
                envName = candidate.substring(0, envSeparator).trim();
                candidate = candidate.substring(envSeparator + 2).trim();
            }

            int methodSeparator = candidate.lastIndexOf('/');
            if (methodSeparator <= 0 || methodSeparator == candidate.length() - 1) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Call endpoint must look like <service>/<method> or <env>::<service>/<method>."
                );
            }
            String serviceName = candidate.substring(0, methodSeparator).trim();
            String methodName = candidate.substring(methodSeparator + 1).trim();
            if (serviceName.isEmpty() || methodName.isEmpty()) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Call endpoint must include both service and method."
                );
            }
            return new ParsedEndpoint(envName, serviceName, methodName);
        }

        static final class ParsedEndpoint {
            private final String environmentName;
            private final String serviceName;
            private final String methodName;

            ParsedEndpoint(String environmentName, String serviceName, String methodName) {
                this.environmentName = environmentName;
                this.serviceName = serviceName;
                this.methodName = methodName;
            }
        }
    }

    @Command(name = "list", mixinStandardHelpOptions = true, description = "List services defined in metadata.")
    static final class ListCommand extends BaseCommand {
        @Override
        public Integer call() {
            LoadedContext loadedContext = loadContextForMetadataCommands();
            if (loadedContext.getMetadata().isEmpty()) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Metadata is empty. list requires a metadata catalog."
                );
            }

            List<Map<String, Object>> services = new ArrayList<Map<String, Object>>();
            for (Map.Entry<String, MetadataCatalog.ServiceMetadata> entry : loadedContext.getMetadata().getServices().entrySet()) {
                Map<String, Object> row = new LinkedHashMap<String, Object>();
                row.put("service", entry.getKey());
                row.put("description", entry.getValue().getDescription());
                row.put("uniqueId", entry.getValue().getUniqueId());
                row.put("methodCount", entry.getValue().getMethods() == null ? 0 : entry.getValue().getMethods().size());
                services.add(row);
            }

            Map<String, Object> payload = new LinkedHashMap<String, Object>();
            payload.put("count", services.size());
            payload.put("services", services);
            printJson(payload);
            return ExitCodes.SUCCESS;
        }
    }

    @Command(name = "describe", mixinStandardHelpOptions = true, description = "Describe one service from metadata.")
    static final class DescribeCommand extends BaseCommand {
        @Option(names = "--service", required = true, description = "Fully qualified service interface name.")
        private String serviceName;

        @Override
        public Integer call() {
            LoadedContext loadedContext = loadContextForMetadataCommands();
            MetadataCatalog.ServiceMetadata serviceMetadata = loadedContext.getMetadata().getService(serviceName);
            if (serviceMetadata == null) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Service not found in metadata: " + serviceName
                );
            }

            Map<String, Object> payload = new LinkedHashMap<String, Object>();
            payload.put("service", serviceName);
            payload.put("description", serviceMetadata.getDescription());
            payload.put("uniqueId", serviceMetadata.getUniqueId());
            payload.put("methods", serviceMetadata.getMethods());
            printJson(payload);
            return ExitCodes.SUCCESS;
        }
    }

    @Command(name = "context", mixinStandardHelpOptions = true, description = "Manage user contexts/profiles.")
    static final class ContextRootCommand implements Runnable {
        @Override
        public void run() {
            System.out.println("Usage: rpcctl context <list|show|use|set|delete> [options]");
        }
    }

    static abstract class ContextBaseCommand implements Callable<Integer> {
        protected ContextCatalog loadContexts(boolean optional) {
            return ConfigLoader.loadContexts(ConfigLoader.resolveDefaultContextsPath(), optional);
        }

        protected void saveContexts(ContextCatalog catalog) {
            ConfigLoader.writeYaml(
                new File(ConfigLoader.resolveDefaultContextsPath()),
                catalog,
                "contexts"
            );
        }

        protected void printJson(Object payload) {
            System.out.println(ConfigLoader.toPrettyJson(payload));
        }
    }

    @Command(name = "list", mixinStandardHelpOptions = true, description = "List configured contexts.")
    static final class ContextListCommand extends ContextBaseCommand {
        @Override
        public Integer call() {
            ContextCatalog catalog = loadContexts(true);
            List<Map<String, Object>> contexts = new ArrayList<Map<String, Object>>();
            for (Map.Entry<String, ContextCatalog.ContextEntry> entry : catalog.getContexts().entrySet()) {
                Map<String, Object> row = new LinkedHashMap<String, Object>();
                row.put("name", entry.getKey());
                row.put("current", entry.getKey().equals(catalog.getCurrentContext()));
                row.put("description", entry.getValue().getDescription());
                row.put("manifestPath", entry.getValue().getManifestPath());
                row.put("env", entry.getValue().getEnv());
                row.put("directUrl", entry.getValue().getDirectUrl());
                row.put("registryAddress", entry.getValue().getRegistryAddress());
                row.put("runtimeBaseUrl", entry.getValue().getRuntimeBaseUrl());
                contexts.add(row);
            }

            Map<String, Object> payload = new LinkedHashMap<String, Object>();
            payload.put("currentContext", catalog.getCurrentContext());
            payload.put("count", contexts.size());
            payload.put("contexts", contexts);
            printJson(payload);
            return ExitCodes.SUCCESS;
        }
    }

    @Command(name = "show", mixinStandardHelpOptions = true, description = "Show one context or the current context.")
    static final class ContextShowCommand extends ContextBaseCommand {
        @Option(names = "--name", description = "Context name. Defaults to the current context.")
        private String name;

        @Override
        public Integer call() {
            ContextCatalog catalog = loadContexts(false);
            ContextCatalog.ResolvedContext resolvedContext = catalog.resolveSelected(name);
            if (resolvedContext.getName() == null) {
                throw new CliException(ExitCodes.PARAMETER_ERROR, "No current context is set.");
            }
            Map<String, Object> payload = new LinkedHashMap<String, Object>();
            payload.put("name", resolvedContext.getName());
            payload.put("current", resolvedContext.getName().equals(catalog.getCurrentContext()));
            payload.put("context", resolvedContext.getEntry());
            printJson(payload);
            return ExitCodes.SUCCESS;
        }
    }

    @Command(name = "use", mixinStandardHelpOptions = true, description = "Set the current context.")
    static final class ContextUseCommand extends ContextBaseCommand {
        @CommandLine.Parameters(index = "0", description = "Context name to activate.")
        private String name;

        @Override
        public Integer call() {
            ContextCatalog catalog = loadContexts(false);
            if (catalog.getContexts() == null || !catalog.getContexts().containsKey(name)) {
                throw new CliException(ExitCodes.PARAMETER_ERROR, "Unknown context: " + name);
            }
            catalog.setCurrentContext(name);
            saveContexts(catalog);
            Map<String, Object> payload = new LinkedHashMap<String, Object>();
            payload.put("currentContext", name);
            payload.put("contextsPath", ConfigLoader.resolveDefaultContextsPath());
            printJson(payload);
            return ExitCodes.SUCCESS;
        }
    }

    @Command(name = "set", mixinStandardHelpOptions = true, description = "Create or update one context.")
    static final class ContextSetCommand extends ContextBaseCommand {
        @CommandLine.Parameters(index = "0", description = "Context name.")
        private String name;

        @Option(names = "--description", description = "Context description.")
        private String description;

        @Option(names = "--manifest", description = "Default manifest path for this context.")
        private String manifestPath;

        @Option(names = "--env", description = "Default env name for this context.")
        private String env;

        @Option(names = "--direct-url", description = "Default direct target.")
        private String directUrl;

        @Option(names = "--registry", description = "Default registry address.")
        private String registryAddress;

        @Option(names = "--registry-protocol", description = "Default registry protocol.")
        private String registryProtocol;

        @Option(names = "--protocol", description = "Default SOFARPC protocol.")
        private String protocol;

        @Option(names = "--serialization", description = "Default serialization.")
        private String serialization;

        @Option(names = "--timeout-ms", description = "Default timeout.")
        private Integer timeoutMs;

        @Option(names = "--sofa-rpc-version", description = "Default SOFARPC version.")
        private String sofaRpcVersion;

        @Option(names = "--runtime-base-url", description = "Runtime download base URL.")
        private String runtimeBaseUrl;

        @Option(names = "--runtime-home", description = "Runtime home override.")
        private String runtimeHome;

        @Option(names = "--runtime-cache-dir", description = "Runtime cache directory override.")
        private String runtimeCacheDir;

        @Option(names = "--auto-download-runtimes", description = "Enable runtime auto-download for this context.")
        private Boolean autoDownloadRuntimes;

        @Option(names = "--current", description = "Set this context as the current context.")
        private boolean current;

        @Override
        public Integer call() {
            ContextCatalog catalog = loadContexts(true);
            if (catalog.getContexts() == null) {
                catalog.setContexts(new LinkedHashMap<String, ContextCatalog.ContextEntry>());
            }
            ContextCatalog.ContextEntry entry = catalog.getContexts().get(name);
            if (entry == null) {
                entry = new ContextCatalog.ContextEntry();
                catalog.getContexts().put(name, entry);
            }

            if (description != null) {
                entry.setDescription(description);
            }
            if (manifestPath != null) {
                entry.setManifestPath(manifestPath);
            }
            if (env != null) {
                entry.setEnv(env);
            }
            if (directUrl != null) {
                entry.setDirectUrl(directUrl);
            }
            if (registryAddress != null) {
                entry.setRegistryAddress(registryAddress);
            }
            if (registryProtocol != null) {
                entry.setRegistryProtocol(registryProtocol);
            }
            if (protocol != null) {
                entry.setProtocol(protocol);
            }
            if (serialization != null) {
                entry.setSerialization(serialization);
            }
            if (timeoutMs != null) {
                entry.setTimeoutMs(timeoutMs);
            }
            if (sofaRpcVersion != null) {
                entry.setSofaRpcVersion(sofaRpcVersion);
            }
            if (runtimeBaseUrl != null) {
                entry.setRuntimeBaseUrl(runtimeBaseUrl);
            }
            if (runtimeHome != null) {
                entry.setRuntimeHome(runtimeHome);
            }
            if (runtimeCacheDir != null) {
                entry.setRuntimeCacheDir(runtimeCacheDir);
            }
            if (autoDownloadRuntimes != null) {
                entry.setAutoDownloadRuntimes(autoDownloadRuntimes);
            }

            if (current || catalog.getCurrentContext() == null || catalog.getCurrentContext().trim().isEmpty()) {
                catalog.setCurrentContext(name);
            }

            saveContexts(catalog);
            Map<String, Object> payload = new LinkedHashMap<String, Object>();
            payload.put("currentContext", catalog.getCurrentContext());
            payload.put("name", name);
            payload.put("context", entry);
            payload.put("contextsPath", ConfigLoader.resolveDefaultContextsPath());
            printJson(payload);
            return ExitCodes.SUCCESS;
        }
    }

    @Command(name = "delete", mixinStandardHelpOptions = true, description = "Delete one context.")
    static final class ContextDeleteCommand extends ContextBaseCommand {
        @CommandLine.Parameters(index = "0", description = "Context name.")
        private String name;

        @Override
        public Integer call() {
            ContextCatalog catalog = loadContexts(false);
            if (catalog.getContexts() == null || catalog.getContexts().remove(name) == null) {
                throw new CliException(ExitCodes.PARAMETER_ERROR, "Unknown context: " + name);
            }
            if (name.equals(catalog.getCurrentContext())) {
                catalog.setCurrentContext(null);
            }
            saveContexts(catalog);
            Map<String, Object> payload = new LinkedHashMap<String, Object>();
            payload.put("deleted", name);
            payload.put("currentContext", catalog.getCurrentContext());
            printJson(payload);
            return ExitCodes.SUCCESS;
        }
    }

    @Command(name = "manifest", mixinStandardHelpOptions = true, description = "Generate or manage rpcctl manifests.")
    static final class ManifestRootCommand implements Runnable {
        @Override
        public void run() {
            System.out.println("Usage: rpcctl manifest <generate|init> [options]");
        }
    }

    @Command(name = "generate", mixinStandardHelpOptions = true, description = "Generate rpcctl-manifest.yaml from config + metadata, or create a skeleton.")
    static final class ManifestGenerateCommand implements Callable<Integer> {
        @Option(names = "--config", description = "Source rpcctl config YAML. Defaults to ./config/rpcctl.yaml when present.")
        private String configPath;

        @Option(names = "--metadata", description = "Source metadata YAML. Defaults to ./config/metadata.yaml when present.")
        private String metadataPath;

        @Option(names = "--output", description = "Output manifest path.", defaultValue = "rpcctl-manifest.yaml")
        private String outputPath;

        @Option(names = "--default-env", description = "Override defaultEnv in the generated manifest.")
        private String defaultEnv;

        @Option(names = "--sofa-rpc-version", description = "Override root sofaRpcVersion in the generated manifest.")
        private String sofaRpcVersion;

        @Option(names = "--protocol", description = "Override root protocol in the generated manifest.")
        private String protocol;

        @Option(names = "--serialization", description = "Override root serialization in the generated manifest.")
        private String serialization;

        @Option(names = "--timeout-ms", description = "Override root timeout in the generated manifest.")
        private Integer timeoutMs;

        @Option(names = "--force", description = "Overwrite the output file if it already exists.")
        private boolean force;

        @Override
        public Integer call() {
            File outputFile = new File(outputPath);
            if (outputFile.exists() && !force) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Output file already exists. Re-run with --force: " + outputFile.getPath()
                );
            }

            RpcCtlManifest manifest = new RpcCtlManifest();
            String resolvedConfigPath = resolveSourcePath(configPath, "config/rpcctl.yaml");
            if (resolvedConfigPath != null && new File(resolvedConfigPath).isFile()) {
                RpcCtlConfig config = ConfigLoader.loadConfig(resolvedConfigPath);
                manifest.setDefaultEnv(config.getDefaultEnv());
                manifest.setProtocol(config.getProtocol());
                manifest.setSerialization(config.getSerialization());
                manifest.setTimeoutMs(config.getTimeoutMs());
                manifest.setSofaRpcVersion(config.getSofaRpcVersion());
                Map<String, RpcCtlManifest.EnvironmentBinding> envs =
                    new LinkedHashMap<String, RpcCtlManifest.EnvironmentBinding>();
                for (Map.Entry<String, RpcCtlConfig.EnvironmentConfig> entry : config.getEnvs().entrySet()) {
                    RpcCtlManifest.EnvironmentBinding binding = new RpcCtlManifest.EnvironmentBinding();
                    binding.setMode(entry.getValue().getMode());
                    binding.setProtocol(entry.getValue().getProtocol());
                    binding.setSerialization(entry.getValue().getSerialization());
                    binding.setRegistryProtocol(entry.getValue().getRegistryProtocol());
                    binding.setRegistryAddress(entry.getValue().getRegistryAddress());
                    binding.setDirectUrl(entry.getValue().getDirectUrl());
                    binding.setTimeoutMs(entry.getValue().getTimeoutMs());
                    binding.setUniqueId(entry.getValue().getUniqueId());
                    binding.setSofaRpcVersion(entry.getValue().getSofaRpcVersion());
                    envs.put(entry.getKey(), binding);
                }
                manifest.setEnvs(envs);
            }

            String resolvedMetadataPath = resolveSourcePath(metadataPath, "config/metadata.yaml");
            if (resolvedMetadataPath != null && new File(resolvedMetadataPath).isFile()) {
                manifest.setServices(ConfigLoader.loadMetadata(resolvedMetadataPath, true).getServices());
            }

            if (defaultEnv != null) {
                manifest.setDefaultEnv(defaultEnv);
            }
            if (sofaRpcVersion != null) {
                manifest.setSofaRpcVersion(sofaRpcVersion);
            }
            if (protocol != null) {
                manifest.setProtocol(protocol);
            }
            if (serialization != null) {
                manifest.setSerialization(serialization);
            }
            if (timeoutMs != null) {
                manifest.setTimeoutMs(timeoutMs);
            }

            ConfigLoader.writeYaml(outputFile, manifest, "manifest");

            Map<String, Object> payload = new LinkedHashMap<String, Object>();
            payload.put("output", outputFile.getAbsolutePath());
            payload.put("defaultEnv", manifest.getDefaultEnv());
            payload.put("serviceCount", manifest.getServices() == null ? 0 : manifest.getServices().size());
            payload.put("envCount", manifest.getEnvs() == null ? 0 : manifest.getEnvs().size());
            System.out.println(ConfigLoader.toPrettyJson(payload));
            return ExitCodes.SUCCESS;
        }

        private String resolveSourcePath(String explicitPath, String defaultRelativePath) {
            if (explicitPath != null && !explicitPath.trim().isEmpty()) {
                return ConfigLoader.resolveOptionalPath(explicitPath, PathsHolder.workingDirectorySentinel());
            }
            File defaultFile = new File(defaultRelativePath);
            return defaultFile.isFile() ? defaultFile.getAbsolutePath() : null;
        }
    }
}
