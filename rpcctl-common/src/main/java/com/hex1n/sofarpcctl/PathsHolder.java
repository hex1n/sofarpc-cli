package com.hex1n.sofarpcctl;

public final class PathsHolder {

    private PathsHolder() {
    }

    public static String workingDirectorySentinel() {
        return new java.io.File(".").getAbsolutePath();
    }
}
