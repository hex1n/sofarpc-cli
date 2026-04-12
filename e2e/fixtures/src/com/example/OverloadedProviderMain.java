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
        String host = System.getProperty("rpcctl.e2e.host", "127.0.0.1");
        String virtualHost = System.getProperty("rpcctl.e2e.virtualHost", host);
        int virtualPort = Integer.parseInt(System.getProperty("rpcctl.e2e.virtualPort", String.valueOf(port)));

        ServerConfig serverConfig = new ServerConfig()
            .setProtocol("bolt")
            .setHost(host)
            .setVirtualHost(virtualHost)
            .setVirtualPort(virtualPort)
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
