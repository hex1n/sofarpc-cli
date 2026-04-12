package com.hex1n.sofarpcctl;

import org.junit.Assert;
import org.junit.Test;

import java.io.File;
import java.nio.file.Files;
import java.nio.file.Path;

public class JavaBinaryResolverTest {

    @Test
    public void prefersUnixBinaryOverFallback() throws Exception {
        Path home = Files.createTempDirectory("rpcctl-java-home");
        try {
            Path javaBinary = home.resolve("bin/java");
            Path javaExe = home.resolve("bin/java.exe");
            Files.createDirectories(home.resolve("bin"));
            Files.createFile(javaBinary);
            Files.createFile(javaExe);
            Files.write(javaBinary, "".getBytes("UTF-8"));
            Files.write(javaExe, "".getBytes("UTF-8"));

            String previous = System.getProperty("java.home");
            System.setProperty("java.home", home.toString());
            try {
                Assert.assertEquals(
                    javaBinary.toFile().getAbsolutePath(),
                    new JavaBinaryResolver().resolve().getAbsolutePath()
                );
            } finally {
                if (previous != null) {
                    System.setProperty("java.home", previous);
                } else {
                    System.clearProperty("java.home");
                }
            }
        } finally {
            deleteRecursively(home);
        }
    }

    @Test
    public void fallsBackToWindowsBinaryWhenUnixBinaryIsMissing() throws Exception {
        Path home = Files.createTempDirectory("rpcctl-java-home-win");
        try {
            Path windowsBinary = home.resolve("bin/java.exe");
            Files.createDirectories(home.resolve("bin"));
            Files.createFile(windowsBinary);
            Files.write(windowsBinary, "".getBytes("UTF-8"));

            String previous = System.getProperty("java.home");
            System.setProperty("java.home", home.toString());
            try {
                Assert.assertEquals(
                    windowsBinary.toFile().getAbsolutePath(),
                    new JavaBinaryResolver().resolve().getAbsolutePath()
                );
            } finally {
                if (previous != null) {
                    System.setProperty("java.home", previous);
                } else {
                    System.clearProperty("java.home");
                }
            }
        } finally {
            deleteRecursively(home);
        }
    }

    private void deleteRecursively(Path root) throws Exception {
        if (Files.notExists(root)) {
            return;
        }
        Files.walk(root)
            .sorted((a, b) -> b.compareTo(a))
            .forEach(path -> {
                try {
                    Files.deleteIfExists(path);
                } catch (Exception ignored) {
                }
            });
    }
}

