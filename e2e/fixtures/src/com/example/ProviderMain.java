package com.example;

import com.alipay.sofa.rpc.config.ApplicationConfig;
import com.alipay.sofa.rpc.config.ProviderConfig;
import com.alipay.sofa.rpc.config.ServerConfig;

import java.util.concurrent.CountDownLatch;

public final class ProviderMain {

    private ProviderMain() {
    }

    public static void main(String[] args) throws Exception {
        int port = Integer.parseInt(System.getProperty("rpcctl.e2e.port", "12200"));

        ServerConfig serverConfig = new ServerConfig()
            .setProtocol("bolt")
            .setHost("127.0.0.1")
            .setPort(port)
            .setDaemon(false);

        ProviderConfig<UserService> providerConfig = new ProviderConfig<UserService>()
            .setApplication(new ApplicationConfig().setAppName("rpcctl-e2e-provider"))
            .setInterfaceId(UserService.class.getName())
            .setUniqueId("user-service")
            .setRef(new UserService() {
                @Override
                public String getUser(Long id) {
                    return "user-" + id;
                }
            })
            .setServer(serverConfig);

        providerConfig.export();
        System.out.println("provider-ready");
        new CountDownLatch(1).await();
    }
}
