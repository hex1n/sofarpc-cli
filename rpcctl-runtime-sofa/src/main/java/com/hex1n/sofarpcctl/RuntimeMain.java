package com.hex1n.sofarpcctl;

import java.nio.charset.StandardCharsets;
import java.util.Base64;

public final class RuntimeMain {

    private RuntimeMain() {
    }

    public static void main(String[] args) {
        RuntimeDefaults.prepare();
        try {
            if (args.length != 2 || !"invoke".equals(args[0])) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Runtime usage: java -jar rpcctl-runtime-sofa-<version>.jar invoke <base64-request>"
                );
            }
            RuntimeInvocationRequest request = decodeRequest(args[1]);
            RuntimeInvocationResult result = new SofaRpcInvoker().invoke(request);
            System.out.println(ConfigLoader.toPrettyJson(result));
            System.exit(result.isSuccess() ? ExitCodes.SUCCESS : ExitCodes.RPC_ERROR);
        } catch (CliException exception) {
            System.err.println(exception.getMessage());
            System.exit(exception.getExitCode());
        } catch (Exception exception) {
            System.err.println("Unexpected runtime error: " + exception.getMessage());
            System.exit(ExitCodes.RPC_ERROR);
        }
    }

    private static RuntimeInvocationRequest decodeRequest(String encodedRequest) {
        try {
            byte[] bytes = Base64.getUrlDecoder().decode(encodedRequest);
            return ConfigLoader.json().readValue(new String(bytes, StandardCharsets.UTF_8), RuntimeInvocationRequest.class);
        } catch (Exception exception) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Failed to decode runtime invocation request.",
                exception
            );
        }
    }
}
