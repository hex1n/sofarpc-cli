package com.hex1n.sofarpcctl;

import org.junit.After;
import org.junit.Assert;
import org.junit.Test;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

public class VersionDetectorTest {

    private final String originalUserDir = System.getProperty("user.dir");

    @After
    public void restoreUserDir() {
        if (originalUserDir == null) {
            System.clearProperty("user.dir");
        } else {
            System.setProperty("user.dir", originalUserDir);
        }
    }

    @Test
    public void resolveUsesExplicitVersionBeforeWorkspaceDiscovery() {
        VersionDetector detector = new VersionDetector();

        VersionDetector.VersionResolution resolution = detector.resolve("5.4.9", null, null);

        Assert.assertEquals("5.4.9", resolution.getResolvedVersion());
        Assert.assertEquals("cli", resolution.getSource());
        Assert.assertFalse(resolution.isFallbackUsed());
    }

    @Test
    public void resolveDetectsWorkspaceVersionFromPom() throws Exception {
        Path workspace = Files.createTempDirectory("rpcctl-version-detector");
        Path nested = Files.createDirectories(workspace.resolve("service-a/module-b"));
        Files.write(
            workspace.resolve("pom.xml"),
            ("<project>\n"
                + "  <properties>\n"
                + "    <sofa-rpc.version>5.4.7</sofa-rpc.version>\n"
                + "  </properties>\n"
                + "</project>\n").getBytes(StandardCharsets.UTF_8)
        );
        System.setProperty("user.dir", nested.toString());

        VersionDetector.VersionResolution resolution = new VersionDetector().resolve(null, null, null);

        Assert.assertEquals("5.4.7", resolution.getResolvedVersion());
        Assert.assertEquals("workspace-detected", resolution.getSource());
        Assert.assertFalse(resolution.isFallbackUsed());
        Assert.assertFalse(resolution.isDeclaredSupported());
        Assert.assertTrue(resolution.getSupportedVersions().contains("5.4.0"));
    }

    @Test
    public void resolveFallsBackWhenWorkspaceHasNoVersionHints() throws Exception {
        Path workspace = Files.createTempDirectory("rpcctl-version-detector-empty");
        System.setProperty("user.dir", workspace.toString());

        VersionDetector.VersionResolution resolution = new VersionDetector().resolve(null, null, null);

        Assert.assertEquals("5.4.0", resolution.getResolvedVersion());
        Assert.assertEquals("default-fallback", resolution.getSource());
        Assert.assertTrue(resolution.isFallbackUsed());
        Assert.assertTrue(resolution.isDeclaredSupported());
    }
}
