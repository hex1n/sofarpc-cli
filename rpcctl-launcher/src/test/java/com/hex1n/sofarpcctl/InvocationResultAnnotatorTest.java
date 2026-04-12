package com.hex1n.sofarpcctl;

import org.junit.Assert;
import org.junit.Test;

import java.util.ArrayList;
import java.util.Arrays;

public class InvocationResultAnnotatorTest {

    @Test
    public void annotatesInvocationMetadataPathAsSchemaPayload() {
        InvocationResultAnnotator annotator = new InvocationResultAnnotator();
        RuntimeInvocationResult result = RuntimeInvocationResult.failure(
            "default",
            "direct",
            "com.example.UserService",
            "user-service",
            "get",
            new ArrayList<String>(),
            false,
            10L,
            "X",
            "failed"
        );

        annotator.annotateInvocationResult(result, "metadata", Arrays.asList("java.lang.Long"));

        Assert.assertEquals("metadata", result.getParamTypeSource());
        Assert.assertEquals("$invoke", result.getInvokeStyle());
        Assert.assertEquals("schema", result.getPayloadMode());
    }

    @Test
    public void annotatesLauncherFailureAsRuntimeSetupError() {
        InvocationResultAnnotator annotator = new InvocationResultAnnotator();
        RuntimeInvocationResult result = RuntimeInvocationResult.failure(
            "default",
            "direct",
            "com.example.UserService",
            "user-service",
            "get",
            Arrays.asList("java.lang.Long"),
            false,
            5L,
            "RPC_ERROR",
            "runtime failed"
        );

        CliException exception = new CliException(ExitCodes.PARAMETER_ERROR, "No SOFARPC runtime found.");

        String errorCode = annotator.classifyLauncherError(exception);
        Assert.assertEquals("RUNTIME_SETUP_ERROR", errorCode);
        annotator.annotateLauncherFailure(result, exception, errorCode);

        Assert.assertEquals("setup", result.getErrorPhase());
        Assert.assertEquals(Boolean.FALSE, result.getRetriable());
        Assert.assertEquals(
            "com.hex1n.sofarpcctl.CliException",
            result.getDiagnostics().get("launcherException")
        );
    }

    @Test
    public void hintsDeserializationPathForRawCollectionTypes() {
        InvocationResultAnnotator annotator = new InvocationResultAnnotator();
        RuntimeInvocationResult result = RuntimeInvocationResult.failure(
            "default",
            "direct",
            "com.example.OverloadedService",
            "overloaded-service",
            "call",
            Arrays.asList("java.util.HashMap"),
            false,
            5L,
            "RPC_METHOD_NOT_FOUND",
            "not found"
        );
        result.setGenericCall(true);

        annotator.applyInvocationFailureHints(result, "cli", "java.util.HashMap", null);

        Assert.assertEquals(
            "Use interface-declared parameter types such as java.util.Map / java.util.List instead of concrete implementations.",
            result.getHint()
        );
    }

    @Test
    public void hintsFallbackVersionWhenRuntimeResolutionFallsBack() {
        InvocationResultAnnotator annotator = new InvocationResultAnnotator();
        RuntimeInvocationResult result = RuntimeInvocationResult.failure(
            "default",
            "direct",
            "com.example.UserService",
            "user-service",
            "get",
            new ArrayList<String>(),
            false,
            5L,
            "RPC_RUNTIME_ERROR",
            "No SOFARPC runtime found."
        );
        VersionDetector.VersionResolution resolution = new VersionDetector.VersionResolution(
            "5.4.0",
            "default-fallback",
            true,
            true,
            Arrays.asList("5.4.0", "5.4.1")
        );

        annotator.annotateVersionResolution(result, resolution);
        CliException exception = new CliException(ExitCodes.RPC_ERROR, "No SOFARPC runtime found.");
        annotator.applyFailureHints(result, exception);

        Assert.assertEquals("true", result.getDiagnostics().get("sofaRpcVersionFallback"));
        Assert.assertTrue(result.getHint().contains("Version detection fell back"));
    }
}
