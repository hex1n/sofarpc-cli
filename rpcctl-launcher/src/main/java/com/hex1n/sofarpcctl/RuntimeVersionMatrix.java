package com.hex1n.sofarpcctl;

import java.io.BufferedReader;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

public final class RuntimeVersionMatrix {

    private static final String RESOURCE_PATH = "/runtime-support/sofa-rpc.versions";
    private volatile List<String> cachedVersions;

    public List<String> listSupportedVersions() {
        List<String> versions = cachedVersions;
        if (versions != null) {
            return versions;
        }
        versions = loadSupportedVersions();
        cachedVersions = versions;
        return versions;
    }

    public boolean isDeclaredSupported(String version) {
        if (version == null || version.trim().isEmpty()) {
            return false;
        }
        return listSupportedVersions().contains(version.trim());
    }

    public String describeSupportedVersions() {
        List<String> versions = listSupportedVersions();
        if (versions.isEmpty()) {
            return "unknown";
        }
        StringBuilder builder = new StringBuilder();
        for (int i = 0; i < versions.size(); i++) {
            if (i > 0) {
                builder.append(", ");
            }
            builder.append(versions.get(i));
        }
        return builder.toString();
    }

    private List<String> loadSupportedVersions() {
        InputStream inputStream = RuntimeVersionMatrix.class.getResourceAsStream(RESOURCE_PATH);
        if (inputStream == null) {
            return Collections.emptyList();
        }

        BufferedReader reader = null;
        try {
            reader = new BufferedReader(new InputStreamReader(inputStream, StandardCharsets.UTF_8));
            List<String> versions = new ArrayList<String>();
            String line;
            while ((line = reader.readLine()) != null) {
                String trimmed = line.trim();
                if (trimmed.isEmpty() || trimmed.startsWith("#")) {
                    continue;
                }
                versions.add(trimmed);
            }
            return Collections.unmodifiableList(versions);
        } catch (Exception ignored) {
            return Collections.emptyList();
        } finally {
            if (reader != null) {
                try {
                    reader.close();
                } catch (Exception ignore) {
                }
            } else {
                try {
                    inputStream.close();
                } catch (Exception ignore) {
                }
            }
        }
    }
}
