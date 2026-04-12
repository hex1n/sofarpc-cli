package com.hex1n.sofarpcctl;

final class RuntimeProcessOutput {

    private final int exitCode;
    private final String stdout;
    private final String stderr;

    RuntimeProcessOutput(int exitCode, String stdout, String stderr) {
        this.exitCode = exitCode;
        this.stdout = stdout;
        this.stderr = stderr;
    }

    int getExitCode() {
        return exitCode;
    }

    String getStdout() {
        return stdout;
    }

    String getStderr() {
        return stderr;
    }
}
