package com.hex1n.sofarpcctl;

import java.io.File;
import java.io.FileInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.Base64;

public final class RuntimeMain {
    private static final String RESULT_MARKER = "RPCCTL_RESULT_JSON:";
    private static final String REQUEST_FILE_FLAG = "--request-file";

    private RuntimeMain() {
    }

    public static void main(String[] args) {
        RuntimeDefaults.prepare();
        try {
            if (args.length < 2 || !"invoke".equals(args[0])) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Runtime usage: java -jar rpcctl-runtime-sofa-<version>.jar invoke <base64-request>\n"
                        + "              java -jar rpcctl-runtime-sofa-<version>.jar invoke --request-file <request-file>"
                );
            }
            String requestArg = resolveRequestArgument(args);
            RuntimeInvocationRequest request = decodeRequest(requestArg);
            RuntimeInvocationResult result = new SofaRpcInvoker().invoke(request);
            String compactResult = ConfigLoader.json().writeValueAsString(result);
            System.out.println(RESULT_MARKER + compactResult);
            System.exit(result.isSuccess() ? ExitCodes.SUCCESS : ExitCodes.RPC_ERROR);
        } catch (CliException exception) {
            System.err.println(exception.getMessage());
            System.exit(exception.getExitCode());
        } catch (Exception exception) {
            System.err.println("Unexpected runtime error: " + exception.getMessage());
            System.exit(ExitCodes.RPC_ERROR);
        }
    }

    private static String resolveRequestArgument(String[] args) {
        if (args.length == 2) {
            return args[1];
        }
        if (args.length == 3 && REQUEST_FILE_FLAG.equals(args[1])) {
            return decodeRequestFromFile(args[2]);
        }
        throw new CliException(
            ExitCodes.PARAMETER_ERROR,
            "Invalid runtime invocation arguments.\n"
                + "Supported forms: invoke <base64-request> or invoke --request-file <request-file>"
        );
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

    private static String decodeRequestFromFile(String requestFilePath) {
        File requestFile = new File(requestFilePath);
        if (!requestFile.isFile()) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Runtime request file is missing: " + requestFilePath
            );
        }
        InputStream inputStream = null;
        try {
            inputStream = new FileInputStream(requestFile);
            byte[] bytes = new byte[(int) requestFile.length()];
            int offset = 0;
            int read;
            while ((read = inputStream.read(bytes, offset, bytes.length - offset)) >= 0) {
                offset += read;
                if (offset == bytes.length) {
                    break;
                }
            }
            if (offset == 0) {
                throw new IOException("Request file is empty.");
            }
            return new String(bytes, 0, offset, StandardCharsets.UTF_8).trim();
        } catch (Exception exception) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Failed to read runtime request file.",
                exception
            );
        } finally {
            if (inputStream != null) {
                try {
                    inputStream.close();
                } catch (Exception ignore) {
                }
            }
        }
    }
}
