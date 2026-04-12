package com.hex1n.sofarpcctl;

import java.io.BufferedInputStream;
import java.io.BufferedReader;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.net.URL;
import java.net.URLConnection;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Locale;

public final class RuntimeDownloader {
    private static final int STREAM_BUFFER_SIZE = 8192;

    public File download(String sofaRpcVersion, String fileName, RuntimeAccessOptions accessOptions) {
        String baseUrl = normalizeBaseUrl(accessOptions.getRuntimeBaseUrl());
        if (baseUrl == null) {
            return null;
        }

        File targetFile = resolveCacheFile(sofaRpcVersion, fileName, accessOptions);
        if (targetFile.isFile()) {
            return targetFile;
        }

        String[] candidates = new String[] {
            baseUrl + "/" + fileName,
            baseUrl + "/sofa-rpc/" + sofaRpcVersion + "/" + fileName,
            baseUrl + "/runtimes/sofa-rpc/" + sofaRpcVersion + "/" + fileName
        };
        for (String candidate : candidates) {
            String expectedSha256 = resolveExpectedChecksum(candidate, fileName);
            if (tryDownload(candidate, targetFile, expectedSha256)) {
                return targetFile;
            }
        }
        return null;
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

    private boolean tryDownload(String sourceUrl, File targetFile, String expectedSha256) {
        File parent = targetFile.getParentFile();
        if (parent != null && !parent.isDirectory() && !parent.mkdirs()) {
            throw new CliException(
                ExitCodes.RPC_ERROR,
                "Failed to create runtime cache directory: " + parent.getPath()
            );
        }

        File partialFile = new File(targetFile.getPath() + ".part");
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

            if (expectedSha256 != null && !isSha256Match(partialFile, expectedSha256)) {
                partialFile.delete();
                throw new CliException(
                    ExitCodes.RPC_ERROR,
                    "Downloaded runtime checksum mismatch: " + sourceUrl
                );
            }

            if (!partialFile.renameTo(targetFile)) {
                throw new CliException(
                    ExitCodes.RPC_ERROR,
                    "Failed to move downloaded runtime into cache: " + targetFile.getPath()
                );
            }
            return true;
        } catch (Exception ignored) {
            if (partialFile.exists()) {
                partialFile.delete();
            }
            return false;
        }
    }

    private String resolveExpectedChecksum(String sourceUrl, String fileName) {
        String parentUrl = parentUrl(sourceUrl);
        if (parentUrl == null || parentUrl.trim().isEmpty()) {
            return null;
        }
        String fromChecksumsTxt = readChecksumFromChecksumsTxt(parentUrl + "/checksums.txt", fileName);
        if (fromChecksumsTxt != null) {
            return fromChecksumsTxt;
        }
        return readSingleChecksum(parentUrl + "/" + fileName + ".sha256");
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
}
