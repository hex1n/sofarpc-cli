package com.hex1n.sofarpcctl;

import picocli.CommandLine;
import picocli.CommandLine.Command;
import picocli.CommandLine.Mixin;
import picocli.CommandLine.Option;
import picocli.CommandLine.ParameterException;

import java.io.File;
import java.io.PrintWriter;
import java.util.ArrayList;
import java.util.Collections;
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
        commandLine.addSubcommand("doctor", new DoctorCommand());
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
            System.out.println("Usage: rpcctl <invoke|call|doctor|list|describe|context|manifest> [options]");
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

        String getContextName() {
            return contextName;
        }

        String getManifestPath() {
            return manifestPath;
        }

        String getConfigPath() {
            return configPath;
        }

        String getMetadataPath() {
            return metadataPath;
        }
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
            merged.setDefaultEnv(StringValueResolver.firstNonBlank(
                contextEntry == null ? null : contextEntry.getEnv(),
                config.getDefaultEnv()
            ));
            merged.setProtocol(StringValueResolver.firstNonBlank(
                contextEntry == null ? null : contextEntry.getProtocol(),
                config.getProtocol(),
                "bolt"
            ));
            merged.setSerialization(StringValueResolver.firstNonBlank(
                contextEntry == null ? null : contextEntry.getSerialization(),
                config.getSerialization(),
                "hessian2"
            ));
            merged.setTimeoutMs(
                contextEntry != null && contextEntry.getTimeoutMs() != null
                    ? contextEntry.getTimeoutMs()
                    : config.getTimeoutMs()
            );
            merged.setSofaRpcVersion(StringValueResolver.firstNonBlank(
                contextEntry == null ? null : contextEntry.getSofaRpcVersion(),
                config.getSofaRpcVersion()
            ));
            merged.setStubPaths(copyList(
                contextEntry != null && contextEntry.getStubPaths() != null && !contextEntry.getStubPaths().isEmpty()
                    ? contextEntry.getStubPaths()
                    : config.getStubPaths()
            ));
            merged.setEnvs(config.getEnvs());
            return merged;
        }

        protected List<String> copyList(List<String> values) {
            return values == null ? new ArrayList<String>() : new ArrayList<String>(values);
        }

        protected List<String> resolvePathList(List<String> configuredPaths, String baseFilePath) {
            if (configuredPaths == null || configuredPaths.isEmpty()) {
                return Collections.emptyList();
            }
            List<String> resolved = new ArrayList<String>(configuredPaths.size());
            for (String configuredPath : configuredPaths) {
                if (configuredPath == null || configuredPath.trim().isEmpty()) {
                    continue;
                }
                resolved.add(ConfigLoader.resolveOptionalPath(configuredPath, baseFilePath));
            }
            return resolved;
        }

        
    }

    @Command(name = "invoke", mixinStandardHelpOptions = true, description = "Invoke one SOFARPC method.")
    static class InvokeCommand extends BaseCommand {
        protected final VersionDetector versionDetector = new VersionDetector();
        protected final ProcessRuntimeInvoker runtimeInvoker = new ProcessRuntimeInvoker();
        protected final ContextLoadResolver contextLoadResolver = new ContextLoadResolver();
        protected final InvocationResultAnnotator invocationResultAnnotator = new InvocationResultAnnotator();
        protected final EnvironmentTargetResolver environmentTargetResolver = new EnvironmentTargetResolver();
        protected final RuntimeAccessResolver runtimeAccessResolver = new RuntimeAccessResolver();
        protected final StubPathResolver stubPathResolver = new StubPathResolver();

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

        @Option(names = "--stub-path",
            description = "Business interface jar or classes directory. Repeat to add multiple entries.")
        protected List<String> stubPaths = new ArrayList<String>();

        @Override
        public Integer call() {
            LoadedContext loadedContext = contextLoadResolver.resolveForInvoke(this).getLoadedContext();
            ResolvedEnvironment resolvedEnvironment = environmentTargetResolver.resolve(
                loadedContext,
                environmentName,
                directUrl,
                registryAddress,
                registryProtocol,
                protocol,
                serialization,
                timeoutMs,
                false
            );
            RpcCtlConfig.EnvironmentConfig environmentConfig = resolvedEnvironment.getEnvironmentConfig();
            MetadataCatalog.ServiceMetadata serviceMetadata = loadedContext.getMetadata().getService(serviceName);
            MetadataCatalog.MethodMetadata methodMetadata = serviceMetadata == null ? null : serviceMetadata.getMethod(methodName);

            ResolvedParamTypes resolvedParamTypes = resolveParamTypes(methodMetadata);
            List<String> paramTypes = resolvedParamTypes.getTypes();
            String resolvedUniqueId = resolveUniqueId(environmentConfig, serviceMetadata);
            enforceRiskConfirmation(resolvedParamTypes.getRisk());

            VersionDetector.VersionResolution versionResolution = versionDetector.resolve(
                sofaRpcVersion,
                loadedContext.getConfig(),
                environmentConfig
            );
            String resolvedVersion = versionResolution.getResolvedVersion();
            RuntimeInvocationRequest request = new RuntimeInvocationRequest();
            request.setEnvironmentName(resolveEffectiveEnvironmentName(loadedContext));
            request.setEnvironmentConfig(environmentConfig);
            request.setServiceName(serviceName);
            request.setUniqueId(resolvedUniqueId);
            request.setMethodName(methodName);
            request.setParamTypes(paramTypes);
            request.setStubPaths(stubPathResolver.resolve(stubPaths, loadedContext).getPaths());
            request.setArgsJson(argsJson);

            RuntimeInvocationResult result;
            long startTime = System.currentTimeMillis();
            try {
                result = runtimeInvoker.invoke(
                    resolvedVersion,
                    request,
                    runtimeAccessResolver.resolve(loadedContext)
                );
            } catch (CliException exception) {
                String launcherErrorCode = invocationResultAnnotator.classifyLauncherError(exception);
                result = RuntimeInvocationResult.failure(
                    request.getEnvironmentName(),
                    environmentConfig.getMode(),
                    serviceName,
                    resolvedUniqueId,
                    methodName,
                    paramTypes,
                    false,
                    System.currentTimeMillis() - startTime,
                    launcherErrorCode,
                    exception.getMessage()
                );
                invocationResultAnnotator.annotateInvocationResult(result, resolvedParamTypes.getSource(), paramTypes);
                invocationResultAnnotator.annotateLauncherFailure(result, exception, launcherErrorCode);
                invocationResultAnnotator.annotateVersionResolution(result, versionResolution);
                invocationResultAnnotator.applyFailureHints(result, exception);
                invocationResultAnnotator.applyInvocationFailureHints(
                    result,
                    resolvedParamTypes.getSource(),
                    rawTypes,
                    request.getStubPaths()
                );
                printJson(result);
                return exception.getExitCode();
            }
            invocationResultAnnotator.annotateInvocationResult(result, resolvedParamTypes.getSource(), paramTypes);
            invocationResultAnnotator.annotateVersionResolution(result, versionResolution);
            invocationResultAnnotator.applyInvocationFailureHints(
                result,
                resolvedParamTypes.getSource(),
                rawTypes,
                request.getStubPaths()
            );
            printJson(result);
            return result.isSuccess() ? ExitCodes.SUCCESS : ExitCodes.RPC_ERROR;
        }

        private ResolvedParamTypes resolveParamTypes(MetadataCatalog.MethodMetadata methodMetadata) {
            List<String> explicitTypes = TypeNameUtils.parseTypes(rawTypes);
            List<String> normalizedExplicitTypes = TypeNameUtils.normalizeParamTypes(explicitTypes);
            List<MetadataCatalog.MethodOverload> overloads = methodMetadata == null
                ? Collections.<MetadataCatalog.MethodOverload>emptyList()
                : methodMetadata.getResolvedOverloads();

            if (!normalizedExplicitTypes.isEmpty()) {
                MetadataCatalog.MethodOverload matchingOverload = findMatchingOverload(overloads, normalizedExplicitTypes);
                if (matchingOverload != null) {
                    return new ResolvedParamTypes(
                        TypeNameUtils.normalizeParamTypes(matchingOverload.getParamTypes()),
                        "metadata",
                        matchingOverload.getRisk()
                    );
                }
                return new ResolvedParamTypes(normalizedExplicitTypes, "cli", deriveRisk(methodMetadata));
            }

            if (overloads.isEmpty()) {
                return new ResolvedParamTypes(new ArrayList<String>(), "none", null);
            }

            int argCount = resolveArgsCount();
            List<MetadataCatalog.MethodOverload> matchingArity = new ArrayList<MetadataCatalog.MethodOverload>();
            for (MetadataCatalog.MethodOverload overload : overloads) {
                List<String> normalizedMetadataTypes = TypeNameUtils.normalizeParamTypes(overload.getParamTypes());
                if (normalizedMetadataTypes.size() == argCount) {
                    matchingArity.add(overload);
                }
            }

            if (matchingArity.size() == 1) {
                MetadataCatalog.MethodOverload matched = matchingArity.get(0);
                return new ResolvedParamTypes(
                    TypeNameUtils.normalizeParamTypes(matched.getParamTypes()),
                    "metadata",
                    matched.getRisk()
                );
            }
            if (matchingArity.size() > 1) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Method " + serviceName + "/" + methodName + " has multiple overloads with "
                        + argCount + " arguments. Pass --types to disambiguate."
                );
            }
            if (methodMetadata != null) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "No overload of " + serviceName + "/" + methodName + " accepts "
                        + argCount + " arguments. Pass --types if metadata is stale."
                );
            }
            return new ResolvedParamTypes(new ArrayList<String>(), "none", null);
        }

        private MetadataCatalog.MethodOverload findMatchingOverload(
            List<MetadataCatalog.MethodOverload> overloads,
            List<String> normalizedExplicitTypes
        ) {
            if (overloads == null || overloads.isEmpty()) {
                return null;
            }
            for (MetadataCatalog.MethodOverload overload : overloads) {
                List<String> normalizedMetadataTypes = TypeNameUtils.normalizeParamTypes(overload.getParamTypes());
                if (normalizedExplicitTypes.equals(normalizedMetadataTypes)) {
                    return overload;
                }
            }
            return null;
        }

        private int resolveArgsCount() {
            String candidate = argsJson == null || argsJson.trim().isEmpty() ? "[]" : argsJson.trim();
            try {
                com.fasterxml.jackson.databind.JsonNode jsonNode = ConfigLoader.json().readTree(candidate);
                if (jsonNode == null || jsonNode.isNull()) {
                    return 0;
                }
                if (!jsonNode.isArray()) {
                    throw new CliException(ExitCodes.PARAMETER_ERROR, "--args must be a JSON array.");
                }
                return jsonNode.size();
            } catch (CliException exception) {
                throw exception;
            } catch (Exception exception) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Failed to parse --args JSON array.",
                    exception
                );
            }
        }

        private String deriveRisk(MetadataCatalog.MethodMetadata methodMetadata) {
            if (methodMetadata == null || methodMetadata.getRisk() == null || methodMetadata.getRisk().trim().isEmpty()) {
                return null;
            }
            return methodMetadata.getRisk();
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

        private void enforceRiskConfirmation(String risk) {
            if (risk == null) {
                return;
            }
            String normalizedRisk = risk.trim().toLowerCase();
            if (("write".equals(normalizedRisk) || "dangerous".equals(normalizedRisk)) && !confirm) {
                throw new CliException(
                    ExitCodes.POLICY_DENIED,
                    "Method is marked as " + normalizedRisk + ". Re-run with --confirm."
                );
            }
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

        static final class ResolvedParamTypes {
            private final List<String> types;
            private final String source;
            private final String risk;

            ResolvedParamTypes(List<String> types, String source, String risk) {
                this.types = types;
                this.source = source;
                this.risk = risk;
            }

            List<String> getTypes() {
                return types;
            }

            String getSource() {
                return source;
            }

            String getRisk() {
                return risk;
            }
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

        @Option(names = "--stub-path",
            description = "Business interface jar or classes directory. Repeat to add multiple entries.")
        private List<String> stubPaths = new ArrayList<String>();

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
            invokeCommand.stubPaths = new ArrayList<String>(stubPaths);
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
                row.put("overloadCount", entry.getValue().getOverloadCount());
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

    @Command(name = "doctor", mixinStandardHelpOptions = true, description = "Diagnose config discovery, runtime resolution, and target reachability.")
    static final class DoctorCommand extends BaseCommand {
        private final VersionDetector versionDetector = new VersionDetector();
        private final RuntimeLocator runtimeLocator = new RuntimeLocator();
        private final NetworkProbe networkProbe = new NetworkProbe();
        private final ContextLoadResolver contextLoadResolver = new ContextLoadResolver();
        private final EnvironmentTargetResolver environmentTargetResolver = new EnvironmentTargetResolver();
        private final RuntimeAccessResolver runtimeAccessResolver = new RuntimeAccessResolver();
        private final StubPathResolver stubPathResolver = new StubPathResolver();
        private final DoctorReportAssembler doctorReportAssembler = new DoctorReportAssembler();

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

        @Option(names = "--timeout-ms", description = "SOFARPC timeout override.")
        private Integer timeoutMs;

        @Option(names = "--sofa-rpc-version", description = "SOFARPC version override.")
        private String sofaRpcVersion;

        @Option(names = "--stub-path",
            description = "Business interface jar or classes directory. Repeat to add multiple entries.")
        private List<String> stubPaths = new ArrayList<String>();

        @Option(names = "--probe-timeout-ms", description = "TCP timeout for reachability probes.", defaultValue = "1000")
        private int probeTimeoutMs;

        @Override
        public Integer call() {
            DoctorReport report = new DoctorReport();

            ContextCatalog catalog = loadContextCatalog(true);
            ContextCatalog.ResolvedContext resolvedContext = doctorReportAssembler.collectContextSection(
                report,
                catalog,
                sharedOptions.contextName
            );
            if (resolvedContext == null) {
                return finishDoctor(report);
            }

            ContextLoadResolution contextLoad;
            try {
                contextLoad = contextLoadResolver.resolveForDoctor(this, resolvedContext);
            } catch (CliException exception) {
                report.error("discovery", exception.getMessage(), null);
                return finishDoctor(report);
            }
            doctorReportAssembler.collectDiscoverySection(report, contextLoad);

            StubPathResolution resolvedStubPaths = stubPathResolver.resolve(stubPaths, contextLoad.getLoadedContext());
            doctorReportAssembler.collectStubPathSection(report, resolvedStubPaths);
            doctorReportAssembler.collectJavaSection(report);

            ResolvedEnvironment resolvedEnvironment;
            try {
                resolvedEnvironment = environmentTargetResolver.resolve(
                    contextLoad.getLoadedContext(),
                    environmentName,
                    directUrl,
                    registryAddress,
                    registryProtocol,
                    protocol,
                    serialization,
                    timeoutMs,
                    true
                );
            } catch (CliException exception) {
                report.error("environment", exception.getMessage(), null);
                return finishDoctor(report);
            }

            doctorReportAssembler.collectEnvironmentSection(report, resolvedEnvironment);

            VersionDetector.VersionResolution versionResolution = versionDetector.resolve(
                sofaRpcVersion,
                contextLoad.getLoadedContext().getConfig(),
                resolvedEnvironment.getEnvironmentConfig()
            );
            doctorReportAssembler.collectVersionSection(report, versionResolution);

            RuntimeAccessOptions runtimeAccessOptions = runtimeAccessResolver.resolve(contextLoad.getLoadedContext());
            RuntimeLocator.RuntimeResolutionProbe runtimeProbe = runtimeLocator.probeRuntimeJar(
                versionResolution.getResolvedVersion(),
                runtimeAccessOptions
            );
            doctorReportAssembler.collectRuntimeSection(report, versionResolution, runtimeProbe, runtimeAccessOptions);
            doctorReportAssembler.collectNetworkSection(report, networkProbe, resolvedEnvironment, probeTimeoutMs);

            return finishDoctor(report);
        }

        private Integer finishDoctor(DoctorReport report) {
            printJson(report.toPayload());
            return report.exitCode();
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
                row.put("stubPaths", entry.getValue().getStubPaths());
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

        @Option(names = "--stub-path", description = "Default stub jar/classes path. Repeat to add multiple entries.")
        private List<String> stubPaths = new ArrayList<String>();

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
            if (stubPaths != null && !stubPaths.isEmpty()) {
                entry.setStubPaths(new ArrayList<String>(stubPaths));
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

        @Option(names = "--stub-path",
            description = "Business interface jar or classes directory used for schema import. Repeat to add multiple entries.")
        private List<String> stubPaths = new ArrayList<String>();

        @Option(names = "--service-class",
            description = "Fully qualified service interface or stub class to import into the manifest. Repeat to add multiple services.")
        private List<String> serviceClasses = new ArrayList<String>();

        @Option(names = "--service-unique-id",
            description = "Optional uniqueId binding in the form <service>=<uniqueId>. When only one --service-class is present, a bare uniqueId is also accepted.")
        private List<String> serviceUniqueIds = new ArrayList<String>();

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
                manifest.setStubPaths(config.getStubPaths());
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

            StubMetadataImporter.ImportResult importResult = new StubMetadataImporter.ImportResult();
            if (!serviceClasses.isEmpty()) {
                importResult = new StubMetadataImporter().importServices(
                    resolvePathList(stubPaths, PathsHolder.workingDirectorySentinel()),
                    serviceClasses,
                    parseUniqueIdBindings()
                );
                if (manifest.getServices() == null) {
                    manifest.setServices(new LinkedHashMap<String, MetadataCatalog.ServiceMetadata>());
                }
                manifest.getServices().putAll(importResult.getServices());
            }
            if (!stubPaths.isEmpty()) {
                manifest.setStubPaths(new ArrayList<String>(stubPaths));
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
            payload.put("stubPathCount", manifest.getStubPaths() == null ? 0 : manifest.getStubPaths().size());
            payload.put("importedServiceCount", importResult.getServices().size());
            payload.put("importedOverloadCount", importResult.getImportedOverloadCount());
            payload.put("skippedOverloads", importResult.getSkippedOverloads());
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

        private List<String> resolvePathList(List<String> configuredPaths, String baseFilePath) {
            if (configuredPaths == null || configuredPaths.isEmpty()) {
                return Collections.emptyList();
            }
            List<String> resolved = new ArrayList<String>(configuredPaths.size());
            for (String configuredPath : configuredPaths) {
                if (configuredPath == null || configuredPath.trim().isEmpty()) {
                    continue;
                }
                resolved.add(ConfigLoader.resolveOptionalPath(configuredPath, baseFilePath));
            }
            return resolved;
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
                    if (serviceClasses.size() != 1) {
                        throw new CliException(
                            ExitCodes.PARAMETER_ERROR,
                            "Bare --service-unique-id requires exactly one --service-class."
                        );
                    }
                    bindings.put(serviceClasses.get(0), binding);
                    continue;
                }
                String serviceClass = binding.substring(0, separatorIndex).trim();
                String uniqueId = binding.substring(separatorIndex + 1).trim();
                if (serviceClass.isEmpty() || uniqueId.isEmpty()) {
                    throw new CliException(
                        ExitCodes.PARAMETER_ERROR,
                        "Invalid --service-unique-id binding: " + rawBinding
                    );
                }
                bindings.put(serviceClass, uniqueId);
            }
            return bindings;
        }
    }
}
