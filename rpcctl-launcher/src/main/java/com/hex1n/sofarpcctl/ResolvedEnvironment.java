package com.hex1n.sofarpcctl;

final class ResolvedEnvironment {

    private final RpcCtlConfig.EnvironmentConfig environmentConfig;
    private final String source;
    private final String envName;

    ResolvedEnvironment(RpcCtlConfig.EnvironmentConfig environmentConfig, String source, String envName) {
        this.environmentConfig = environmentConfig;
        this.source = source;
        this.envName = envName;
    }

    RpcCtlConfig.EnvironmentConfig getEnvironmentConfig() {
        return environmentConfig;
    }

    String getSource() {
        return source;
    }

    String getEnvName() {
        return envName;
    }
}
