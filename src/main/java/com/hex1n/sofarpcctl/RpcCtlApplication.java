package com.hex1n.sofarpcctl;

import picocli.CommandLine;
import picocli.CommandLine.Command;
import picocli.CommandLine.Mixin;
import picocli.CommandLine.Option;
import picocli.CommandLine.ParameterException;

import java.io.PrintWriter;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Callable;

public final class RpcCtlApplication {

    private RpcCtlApplication() {
    }

    public static void main(String[] args) {
        CommandLine commandLine = new CommandLine(new RootCommand());
        commandLine.addSubcommand("invoke", new InvokeCommand());
        commandLine.addSubcommand("list", new ListCommand());
        commandLine.addSubcommand("describe", new DescribeCommand());
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

    @Command(
        name = "rpcctl",
        mixinStandardHelpOptions = true,
        version = "0.1.0",
        description = "Portable SOFABoot / SOFARPC command line invoker."
    )
    static final class RootCommand implements Runnable {
        @Override
        public void run() {
            System.out.println("Usage: rpcctl <invoke|list|describe> [options]");
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

        protected LoadedContext loadContext(boolean metadataOptional) {
            String configPath = sharedOptions.configPath != null
                ? ConfigLoader.resolveOptionalPath(sharedOptions.configPath, PathsHolder.workingDirectorySentinel())
                : ConfigLoader.resolveDefaultConfigPath();
            RpcCtlConfig config = ConfigLoader.loadConfig(configPath);
            String metadataPath = sharedOptions.metadataPath != null
                ? ConfigLoader.resolveOptionalPath(sharedOptions.metadataPath, configPath)
                : ConfigLoader.resolveOptionalPath(config.getMetadataPath(), configPath);
            MetadataCatalog metadata = ConfigLoader.loadMetadata(metadataPath, metadataOptional);
            return new LoadedContext(config, metadata, configPath, metadataPath);
        }

        protected void printJson(Object payload) {
            System.out.println(ConfigLoader.toPrettyJson(payload));
        }
    }

    @Command(name = "invoke", mixinStandardHelpOptions = true, description = "Invoke one SOFARPC method.")
    static final class InvokeCommand extends BaseCommand {
        @Option(names = "--env", required = true, description = "Environment name from config.")
        private String environmentName;

        @Option(names = "--service", required = true, description = "Fully qualified service interface name.")
        private String serviceName;

        @Option(names = "--method", required = true, description = "Method name to invoke.")
        private String methodName;

        @Option(names = "--types",
            description = "Comma-separated parameter types. Omit this when metadata already provides them.")
        private String rawTypes;

        @Option(names = "--args", defaultValue = "[]", description = "JSON array of method arguments.")
        private String argsJson;

        @Option(names = "--unique-id", description = "SOFARPC uniqueId override.")
        private String uniqueId;

        @Option(names = "--confirm", description = "Required for metadata methods marked as write or dangerous.")
        private boolean confirm;

        @Override
        public Integer call() {
            LoadedContext loadedContext = loadContext(true);
            RpcCtlConfig.EnvironmentConfig environmentConfig = loadedContext.config.requireEnv(environmentName);
            MetadataCatalog.ServiceMetadata serviceMetadata = loadedContext.metadata.getService(serviceName);
            MetadataCatalog.MethodMetadata methodMetadata = serviceMetadata == null ? null : serviceMetadata.getMethod(methodName);

            List<String> paramTypes = resolveParamTypes(methodMetadata);
            String resolvedUniqueId = resolveUniqueId(environmentConfig, serviceMetadata);
            enforceRiskConfirmation(methodMetadata);

            InvocationPayloads.ResolvedPayloads payloads = InvocationPayloads.resolve(paramTypes, argsJson);
            SofaRpcInvoker.InvocationResult result = new SofaRpcInvoker().invoke(
                environmentConfig,
                environmentName,
                serviceName,
                resolvedUniqueId,
                methodName,
                payloads
            );
            printJson(result.asMap());
            return result.isSuccess() ? ExitCodes.SUCCESS : ExitCodes.RPC_ERROR;
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
    }

    @Command(name = "list", mixinStandardHelpOptions = true, description = "List services defined in metadata.")
    static final class ListCommand extends BaseCommand {
        @Override
        public Integer call() {
            LoadedContext loadedContext = loadContext(false);
            if (loadedContext.metadata.isEmpty()) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Metadata is empty. list requires a metadata catalog."
                );
            }

            List<Map<String, Object>> services = new ArrayList<Map<String, Object>>();
            for (Map.Entry<String, MetadataCatalog.ServiceMetadata> entry : loadedContext.metadata.getServices().entrySet()) {
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
            LoadedContext loadedContext = loadContext(false);
            MetadataCatalog.ServiceMetadata serviceMetadata = loadedContext.metadata.getService(serviceName);
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

    static final class LoadedContext {
        private final RpcCtlConfig config;
        private final MetadataCatalog metadata;
        private final String configPath;
        private final String metadataPath;

        private LoadedContext(RpcCtlConfig config, MetadataCatalog metadata, String configPath, String metadataPath) {
            this.config = config;
            this.metadata = metadata;
            this.configPath = configPath;
            this.metadataPath = metadataPath;
        }
    }

    static final class PathsHolder {
        private PathsHolder() {
        }

        static String workingDirectorySentinel() {
            return new java.io.File(".").getAbsolutePath();
        }
    }

    static final class ExitCodes {
        static final int SUCCESS = 0;
        static final int PARAMETER_ERROR = 2;
        static final int RPC_ERROR = 5;
        static final int POLICY_DENIED = 8;

        private ExitCodes() {
        }
    }

    static final class CliException extends RuntimeException {
        private final int exitCode;

        CliException(int exitCode, String message) {
            super(message);
            this.exitCode = exitCode;
        }

        CliException(int exitCode, String message, Throwable cause) {
            super(message, cause);
            this.exitCode = exitCode;
        }

        int getExitCode() {
            return exitCode;
        }
    }
}
