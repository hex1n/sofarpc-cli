package com.example.demo;

import com.alipay.sofa.rpc.config.ApplicationConfig;
import com.alipay.sofa.rpc.config.ProviderConfig;
import com.alipay.sofa.rpc.config.RegistryConfig;
import com.alipay.sofa.rpc.config.ServerConfig;

public class ProviderMain {

    public static void main(String[] args) throws Exception {
        int port = Integer.parseInt(System.getProperty("provider.port", "21200"));

        ServerConfig serverConfig = new ServerConfig()
                .setProtocol("bolt")
                .setPort(port)
                .setDaemon(false);

        // LocalRegistry still needs a backing file even with register=false.
        RegistryConfig registryConfig = new RegistryConfig()
                .setProtocol("local")
                .setFile(System.getProperty("provider.registry.file"))
                .setRegister(false)
                .setSubscribe(false);

        ProviderConfig<EchoFacade> providerConfig = new ProviderConfig<EchoFacade>()
                .setInterfaceId(EchoFacade.class.getName())
                .setApplication(new ApplicationConfig().setAppName("demo-provider"))
                .setRef(new EchoFacadeImpl())
                .setServer(serverConfig)
                .setRegistry(registryConfig);

        providerConfig.export();
        System.out.println("PROVIDER_READY port=" + port);
        System.out.flush();
        Thread.currentThread().join();
    }
}
