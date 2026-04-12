package com.hex1n.sofarpcctl;

import java.io.File;

final class JavaBinaryResolver {

    File resolve() {
        File javaBinary = new File(System.getProperty("java.home"), "bin/java");
        if (javaBinary.isFile()) {
            return javaBinary;
        }
        File windowsJavaBinary = new File(System.getProperty("java.home"), "bin/java.exe");
        if (windowsJavaBinary.isFile()) {
            return windowsJavaBinary;
        }
        return javaBinary;
    }
}

