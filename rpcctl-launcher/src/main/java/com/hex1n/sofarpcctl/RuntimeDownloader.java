package com.hex1n.sofarpcctl;

import java.io.BufferedInputStream;
import java.io.BufferedReader;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.RandomAccessFile;
import java.net.URL;
import java.net.URLConnection;
import java.nio.channels.FileChannel;
import java.nio.channels.FileLock;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.StandardCopyOption;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Locale;

public final class RuntimeDownloader {
    private static final int STREAM_BUFFER_SIZE = 8192;
    private static final String VERBOSE_ENV = "RPCCTL_RUNTIME_VERBOSE";

    public DownloadResult download(String sofaRpcVersion, String fileName, RuntimeAccessOptions accessOptions) {
        String baseUrl = normalizeBaseUrl(accessOptions.getRuntimeBaseUrl());
        if (baseUrl == null) {
            return DownloadResult.notAttempted();
        }

        File targetFile = resolveCacheFile(sofaRpcVersion, fileName, accessOptions);
        if (targetFile.isFile()) {
            return DownloadResult.success(targetFile, Collections.<DownloadAttempt>emptyList());
        }

        String[] candidates = new String[] {
            baseUrl + "/" + fileName,
            baseUrl + "/sofa-rpc/" + sofaRpcVersion + "/" + fileName,
            baseUrl + "/runtimes/sofa-rpc/" + sofaRpcVersion + "/" + fileName
        };
        return downloadUnderLock(sofaRpcVersion, fileName, targetFile, candidates);
    }

    private DownloadResult downloadUnderLock(
        String sofaRpcVersion,
        String fileName,
        File targetFile,
        String[] candidates
    ) {
        File parent = targetFile.getParentFile();
        if (parent != null && !parent.isDirectory() && !parent.mkdirs()) {
            throw new CliException(
                ExitCodes.RPC_ERROR,
                "Failed to create runtime cache directory: " + parent.getPath()
            );
        }

        File lockFile = new File(targetFile.getPath() + ".lock");
        RandomAccessFile lockStream = null;
        FileChannel channel = null;
        FileLock lock = null;
        try {
            lockStream = new RandomAccessFile(lockFile, "rw");
            channel = lockStream.getChannel();
            log("Waiting for runtime cache lock: " + lockFile.getAbsolutePath());
            lock = channel.lock();
            log("Acquired runtime cache lock: " + lockFile.getAbsolutePath());
            if (targetFile.isFile()) {
                return DownloadResult.success(targetFile, Collections.<DownloadAttempt>emptyList());
            }

            List<DownloadAttempt> attempts = new ArrayList<DownloadAttempt>();
            for (String candidate : candidates) {
                ChecksumExpectation checksumExpectation = resolveExpectedChecksum(candidate, fileName);
                DownloadAttempt attempt = tryDownload(candidate, targetFile, checksumExpectation);
                attempts.add(attempt);
                if (attempt.isSuccess()) {
                    return DownloadResult.success(targetFile, attempts);
                }
            }
            return DownloadResult.failed(attempts);
        } catch (IOException exception) {
            throw new CliException(
                ExitCodes.RPC_ERROR,
                "Failed to coordinate runtime download for version " + sofaRpcVersion + ".",
                exception
            );
        } finally {
            if (lock != null) {
                try {
                    lock.release();
                } catch (IOException ignore) {
                }
            }
            if (channel != null) {
                try {
                    channel.close();
                } catch (IOException ignore) {
                }
            }
            if (lockStream != null) {
                try {
                    lockStream.close();
                } catch (IOException ignore) {
                }
            }
        }
    }

    private File resolveCacheFile(String sofaRpcVersion, String fileName, RuntimeAccessOptions accessOptions) {
        String cacheDir = accessOptions.getRuntimeCacheDir();
        Path root = cacheDir == null || cacheDir.trim().isEmpty()
            ? ConfigLoader.resolveXdgCacheRoot().resolve("sofa-rpcctl").resolve("runtimes")
            : Paths.get(cacheDir.trim()).toAbsolutePath().normalize();
        return root.resolve("sofa-rpc").resolve(sofaRpcVersion).resolve(fileName).toFile();
    }

    private String normalizeBaseUrl(String rawBaseUrl) {
        if (rawBaseUrl == null || rawBaseUrl.trim().isEmpty()) {
            return null;
        }
        String trimmed = rawBaseUrl.trim();
        if (trimmed.contains("://")) {
            return trimmed.endsWith("/") ? trimmed.substring(0, trimmed.length() - 1) : trimmed;
        }
        File file = new File(trimmed);
        if (file.isDirectory()) {
            return file.toURI().toString().replaceAll("/$", "");
        }
        return trimmed.endsWith("/") ? trimmed.substring(0, trimmed.length() - 1) : trimmed;
    }

    private DownloadAttempt tryDownload(String sourceUrl, File targetFile, ChecksumExpectation checksumExpectation) {
        File partialFile = new File(targetFile.getPath() + ".part");
        log("Trying runtime download candidate: " + sourceUrl);
        if (checksumExpectation.getSha256() != null) {
            log("Resolved runtime checksum from " + checksumExpectation.getSource() + ": " + checksumExpectation.getSha256());
        } else {
            log("No checksum metadata found for " + sourceUrl);
        }

        try {
            URLConnection connection = new URL(sourceUrl).openConnection();
            connection.setConnectTimeout(3000);
            connection.setReadTimeout(10000);

            InputStream inputStream = null;
            FileOutputStream outputStream = null;
            try {
                inputStream = new BufferedInputStream(connection.getInputStream());
                outputStream = new FileOutputStream(partialFile);
                byte[] buffer = new byte[STREAM_BUFFER_SIZE];
                int read;
                while ((read = inputStream.read(buffer)) >= 0) {
                    outputStream.write(buffer, 0, read);
                }
            } finally {
                if (outputStream != null) {
                    try {
                        outputStream.close();
                    } catch (IOException ignore) {
                    }
                }
                if (inputStream != null) {
                    try {
                        inputStream.close();
                    } catch (IOException ignore) {
                    }
                }
            }

            if (checksumExpectation.getSha256() != null && !isSha256Match(partialFile, checksumExpectation.getSha256())) {
                if (partialFile.exists()) {
                    partialFile.delete();
                }
                return DownloadAttempt.failure(
                    sourceUrl,
                    checksumExpectation.getSource(),
                    "Checksum mismatch after download."
                );
            }

            try {
                Files.move(
                    partialFile.toPath(),
                    targetFile.toPath(),
                    StandardCopyOption.REPLACE_EXISTING,
                    StandardCopyOption.ATOMIC_MOVE
                );
            } catch (IOException atomicMoveException) {
                Files.move(partialFile.toPath(), targetFile.toPath(), StandardCopyOption.REPLACE_EXISTING);
            }
            log("Runtime download succeeded: " + targetFile.getAbsolutePath());
            return DownloadAttempt.success(sourceUrl, checksumExpectation.getSource());
        } catch (Exception exception) {
            if (partialFile.exists()) {
                partialFile.delete();
            }
            String message = exception.getClass().getSimpleName() + ": " + exception.getMessage();
            log("Runtime download failed from " + sourceUrl + ": " + message);
            return DownloadAttempt.failure(sourceUrl, checksumExpectation.getSource(), message);
        }
    }

    private ChecksumExpectation resolveExpectedChecksum(String sourceUrl, String fileName) {
        String parentUrl = parentUrl(sourceUrl);
        if (parentUrl == null || parentUrl.trim().isEmpty()) {
            return ChecksumExpectation.missing();
        }
        String checksumsTxtUrl = parentUrl + "/checksums.txt";
        String fromChecksumsTxt = readChecksumFromChecksumsTxt(checksumsTxtUrl, fileName);
        if (fromChecksumsTxt != null) {
            return new ChecksumExpectation(fromChecksumsTxt, checksumsTxtUrl);
        }
        String singleChecksumUrl = parentUrl + "/" + fileName + ".sha256";
        String singleChecksum = readSingleChecksum(singleChecksumUrl);
        if (singleChecksum != null) {
            return new ChecksumExpectation(singleChecksum, singleChecksumUrl);
        }
        return ChecksumExpectation.missing();
    }

    private String readChecksumFromChecksumsTxt(String checksumUrl, String fileName) {
        BufferedReader reader = null;
        try {
            reader = new BufferedReader(new InputStreamReader(new URL(checksumUrl).openStream(), "UTF-8"));
            String line;
            while ((line = reader.readLine()) != null) {
                String resolved = checksumFromLine(line, fileName);
                if (resolved != null) {
                    return resolved;
                }
            }
            return null;
        } catch (Exception ignored) {
            return null;
        } finally {
            if (reader != null) {
                try {
                    reader.close();
                } catch (IOException ignored) {
                }
            }
        }
    }

    private String readSingleChecksum(String checksumUrl) {
        BufferedReader reader = null;
        try {
            reader = new BufferedReader(new InputStreamReader(new URL(checksumUrl).openStream(), "UTF-8"));
            String line = reader.readLine();
            if (line == null) {
                return null;
            }
            String token = line.trim().split("\\s+")[0];
            return isSha256Hex(token) ? token.toLowerCase(Locale.ROOT) : null;
        } catch (Exception ignored) {
            return null;
        } finally {
            if (reader != null) {
                try {
                    reader.close();
                } catch (IOException ignored) {
                }
            }
        }
    }

    private String checksumFromLine(String line, String fileName) {
        String trimmed = line == null ? "" : line.trim();
        if (trimmed.isEmpty() || trimmed.startsWith("#")) {
            return null;
        }
        String[] columns = trimmed.split("\\s+");
        if (columns.length < 2) {
            return null;
        }

        String fileInLine = columns[1];
        if (fileInLine.endsWith("/")) {
            return null;
        }
        if (fileInLine.startsWith("*")) {
            fileInLine = fileInLine.substring(1);
        }
        if (fileInLine.startsWith("./")) {
            fileInLine = fileInLine.substring(2);
        }
        if (fileInLine.equals(fileName) && isSha256Hex(columns[0])) {
            return columns[0].toLowerCase(Locale.ROOT);
        }
        return null;
    }

    private boolean isSha256Match(File file, String expectedSha256) {
        try {
            return expectedSha256.equalsIgnoreCase(sha256Hex(file));
        } catch (Exception ignored) {
            return false;
        }
    }

    private String sha256Hex(File file) throws IOException, NoSuchAlgorithmException {
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        InputStream inputStream = null;
        try {
            inputStream = new BufferedInputStream(new FileInputStream(file));
            byte[] buffer = new byte[STREAM_BUFFER_SIZE];
            int read;
            while ((read = inputStream.read(buffer)) >= 0) {
                digest.update(buffer, 0, read);
            }
            byte[] bytes = digest.digest();
            StringBuilder sb = new StringBuilder(bytes.length * 2);
            for (byte value : bytes) {
                int unsigned = value & 0xFF;
                if (unsigned < 0x10) {
                    sb.append('0');
                }
                sb.append(Integer.toHexString(unsigned));
            }
            return sb.toString();
        } finally {
            if (inputStream != null) {
                try {
                    inputStream.close();
                } catch (IOException ignored) {
                }
            }
        }
    }

    private boolean isSha256Hex(String value) {
        if (value == null || value.length() < 64) {
            return false;
        }
        for (int i = 0; i < value.length(); i++) {
            char ch = value.charAt(i);
            if (!((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F'))) {
                return false;
            }
        }
        return true;
    }

    private String parentUrl(String sourceUrl) {
        if (sourceUrl == null) {
            return null;
        }
        int lastSlash = sourceUrl.lastIndexOf('/');
        if (lastSlash <= 0) {
            return null;
        }
        return sourceUrl.substring(0, lastSlash);
    }

    private void log(String message) {
        if ("1".equals(System.getenv(VERBOSE_ENV)) || "true".equalsIgnoreCase(System.getenv(VERBOSE_ENV))) {
            System.err.println("rpcctl runtime download: " + message);
        }
    }

    public static final class DownloadResult {
        private final File file;
        private final List<DownloadAttempt> attempts;
        private final boolean attempted;

        private DownloadResult(File file, List<DownloadAttempt> attempts, boolean attempted) {
            this.file = file;
            this.attempts = attempts;
            this.attempted = attempted;
        }

        public static DownloadResult success(File file, List<DownloadAttempt> attempts) {
            return new DownloadResult(file, Collections.unmodifiableList(new ArrayList<DownloadAttempt>(attempts)), true);
        }

        public static DownloadResult failed(List<DownloadAttempt> attempts) {
            return new DownloadResult(null, Collections.unmodifiableList(new ArrayList<DownloadAttempt>(attempts)), true);
        }

        public static DownloadResult notAttempted() {
            return new DownloadResult(null, Collections.<DownloadAttempt>emptyList(), false);
        }

        public File getFile() {
            return file;
        }

        public List<DownloadAttempt> getAttempts() {
            return attempts;
        }

        public boolean isAttempted() {
            return attempted;
        }

        public boolean isSuccess() {
            return file != null && file.isFile();
        }

        public String summarizeFailures() {
            if (attempts == null || attempts.isEmpty()) {
                return "";
            }
            StringBuilder builder = new StringBuilder();
            int limit = Math.min(3, attempts.size());
            for (int i = 0; i < limit; i++) {
                DownloadAttempt attempt = attempts.get(i);
                if (builder.length() > 0) {
                    builder.append(" | ");
                }
                builder.append(attempt.getSourceUrl()).append(" -> ").append(attempt.getMessage());
            }
            return builder.toString();
        }
    }

    public static final class DownloadAttempt {
        private final String sourceUrl;
        private final String checksumSource;
        private final boolean success;
        private final String message;

        private DownloadAttempt(String sourceUrl, String checksumSource, boolean success, String message) {
            this.sourceUrl = sourceUrl;
            this.checksumSource = checksumSource;
            this.success = success;
            this.message = message;
        }

        public static DownloadAttempt success(String sourceUrl, String checksumSource) {
            return new DownloadAttempt(sourceUrl, checksumSource, true, "ok");
        }

        public static DownloadAttempt failure(String sourceUrl, String checksumSource, String message) {
            return new DownloadAttempt(sourceUrl, checksumSource, false, message);
        }

        public String getSourceUrl() {
            return sourceUrl;
        }

        public String getChecksumSource() {
            return checksumSource;
        }

        public boolean isSuccess() {
            return success;
        }

        public String getMessage() {
            return message;
        }
    }

    private static final class ChecksumExpectation {
        private final String sha256;
        private final String source;

        private ChecksumExpectation(String sha256, String source) {
            this.sha256 = sha256;
            this.source = source;
        }

        static ChecksumExpectation missing() {
            return new ChecksumExpectation(null, null);
        }

        String getSha256() {
            return sha256;
        }

        String getSource() {
            return source;
        }
    }
}
