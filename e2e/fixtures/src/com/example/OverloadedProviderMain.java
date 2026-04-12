package com.example;

import com.alipay.sofa.rpc.config.ApplicationConfig;
import com.alipay.sofa.rpc.config.ProviderConfig;
import com.alipay.sofa.rpc.config.ServerConfig;

import java.util.concurrent.CountDownLatch;

public final class OverloadedProviderMain {

    private OverloadedProviderMain() {
    }

    public static void main(String[] args) throws Exception {
        int port = Integer.parseInt(System.getProperty("rpcctl.e2e.port", "12242"));

        ServerConfig serverConfig = new ServerConfig()
            .setProtocol("bolt")
            .setHost("127.0.0.1")
            .setPort(port)
            .setDaemon(false);

        ProviderConfig<OverloadedService> providerConfig = new ProviderConfig<OverloadedService>()
            .setApplication(new ApplicationConfig().setAppName("rpcctl-e2e-overloaded-provider"))
            .setInterfaceId(OverloadedService.class.getName())
            .setUniqueId("overloaded-service")
            .setRef(new OverloadedService() {
                @Override
                public String ping(String value) {
                    return "ping-1:" + value;
                }

                @Override
                public String ping(String value, Integer times) {
                    return "ping-2:" + value + ":" + times;
                }

                @Override
                public String lookup(Long id) {
                    return "id:" + id;
                }

                @Override
                public String lookup(String key) {
                    return "key:" + key;
                }
            })
            .setServer(serverConfig);

        providerConfig.export();
        System.out.println("overloaded-provider-ready");
        new CountDownLatch(1).await();
    }
}
