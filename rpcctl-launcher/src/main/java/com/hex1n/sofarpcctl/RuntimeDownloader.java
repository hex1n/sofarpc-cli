package com.hex1n.sofarpcctl;

import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.net.URL;
import java.net.URLConnection;
import java.nio.file.Path;
import java.nio.file.Paths;

public final class RuntimeDownloader {

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
            if (tryDownload(candidate, targetFile)) {
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

    private boolean tryDownload(String sourceUrl, File targetFile) {
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
            InputStream inputStream = connection.getInputStream();
            FileOutputStream outputStream = new FileOutputStream(partialFile);
            byte[] buffer = new byte[8192];
            int read;
            while ((read = inputStream.read(buffer)) >= 0) {
                outputStream.write(buffer, 0, read);
            }
            outputStream.close();
            inputStream.close();
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
}
