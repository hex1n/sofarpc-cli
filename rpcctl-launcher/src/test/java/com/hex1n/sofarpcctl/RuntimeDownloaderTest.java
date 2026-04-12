package com.hex1n.sofarpcctl;

import org.junit.Assert;
import org.junit.Test;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;

public class RuntimeDownloaderTest {

    @Test
    public void downloadCachesRuntimeWhenChecksumMatches() throws Exception {
        Path baseDir = Files.createTempDirectory("rpcctl-runtime-base");
        Path cacheDir = Files.createTempDirectory("rpcctl-runtime-cache");
        String version = "5.4.0";
        String fileName = "rpcctl-runtime-sofa-" + version + ".jar";
        Path runtimeFile = baseDir.resolve(fileName);
        Files.write(runtimeFile, "hello runtime".getBytes(StandardCharsets.UTF_8));
        Files.write(
            baseDir.resolve("checksums.txt"),
            (sha256(runtimeFile) + "  " + fileName + "\n").getBytes(StandardCharsets.UTF_8)
        );

        RuntimeDownloader.DownloadResult result = new RuntimeDownloader().download(
            version,
            fileName,
            new RuntimeAccessOptions(null, baseDir.toString(), cacheDir.toString(), true)
        );

        Assert.assertTrue(result.isAttempted());
        Assert.assertTrue(result.isSuccess());
        Assert.assertTrue(result.getFile().isFile());
        Assert.assertEquals("hello runtime", new String(Files.readAllBytes(result.getFile().toPath()), StandardCharsets.UTF_8));
        Assert.assertEquals(1, result.getAttempts().size());
        Assert.assertTrue(result.getAttempts().get(0).isSuccess());
    }

    @Test
    public void downloadReportsCandidateFailuresWhenChecksumDoesNotMatch() throws Exception {
        Path baseDir = Files.createTempDirectory("rpcctl-runtime-base-bad");
        Path cacheDir = Files.createTempDirectory("rpcctl-runtime-cache-bad");
        String version = "5.4.0";
        String fileName = "rpcctl-runtime-sofa-" + version + ".jar";
        Files.write(baseDir.resolve(fileName), "bad runtime".getBytes(StandardCharsets.UTF_8));
        Files.write(
            baseDir.resolve("checksums.txt"),
            ("0000000000000000000000000000000000000000000000000000000000000000  " + fileName + "\n")
                .getBytes(StandardCharsets.UTF_8)
        );

        RuntimeDownloader.DownloadResult result = new RuntimeDownloader().download(
            version,
            fileName,
            new RuntimeAccessOptions(null, baseDir.toString(), cacheDir.toString(), true)
        );

        Assert.assertTrue(result.isAttempted());
        Assert.assertFalse(result.isSuccess());
        Assert.assertEquals(3, result.getAttempts().size());
        Assert.assertTrue(result.summarizeFailures().contains("Checksum mismatch after download."));
    }

    private String sha256(Path file) throws Exception {
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        byte[] bytes = digest.digest(Files.readAllBytes(file));
        StringBuilder builder = new StringBuilder(bytes.length * 2);
        for (byte value : bytes) {
            int unsigned = value & 0xFF;
            if (unsigned < 0x10) {
                builder.append('0');
            }
            builder.append(Integer.toHexString(unsigned));
        }
        return builder.toString();
    }
}
