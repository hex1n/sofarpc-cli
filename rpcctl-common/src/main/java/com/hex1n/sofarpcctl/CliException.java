package com.hex1n.sofarpcctl;

public final class CliException extends RuntimeException {

    private final int exitCode;

    public CliException(int exitCode, String message) {
        super(message);
        this.exitCode = exitCode;
    }

    public CliException(int exitCode, String message, Throwable cause) {
        super(message, cause);
        this.exitCode = exitCode;
    }

    public int getExitCode() {
        return exitCode;
    }
}
