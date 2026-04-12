package com.example;

import com.alipay.sofa.rpc.config.ApplicationConfig;
import com.alipay.sofa.rpc.config.ProviderConfig;
import com.alipay.sofa.rpc.config.RegistryConfig;
import com.alipay.sofa.rpc.config.ServerConfig;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;

public final class PayloadProviderRegistryMain {

    private PayloadProviderRegistryMain() {
    }

    public static void main(String[] args) throws Exception {
        int port = Integer.parseInt(System.getProperty("rpcctl.e2e.port", "12203"));
        String host = System.getProperty("rpcctl.e2e.host", "127.0.0.1");
        String virtualHost = System.getProperty("rpcctl.e2e.virtualHost", host);
        int virtualPort = Integer.parseInt(System.getProperty("rpcctl.e2e.virtualPort", String.valueOf(port)));
        String zkAddress = System.getProperty("rpcctl.e2e.zkAddress", "127.0.0.1:2181");

        ServerConfig serverConfig = new ServerConfig()
            .setProtocol("bolt")
            .setHost(host)
            .setVirtualHost(virtualHost)
            .setVirtualPort(virtualPort)
            .setPort(port)
            .setDaemon(false);

        RegistryConfig registryConfig = new RegistryConfig()
            .setProtocol("zookeeper")
            .setAddress(zkAddress)
            .setRegister(true)
            .setSubscribe(false);

        ProviderConfig<PayloadService> providerConfig = new ProviderConfig<PayloadService>()
            .setApplication(new ApplicationConfig().setAppName("rpcctl-e2e-payload-provider"))
            .setInterfaceId(PayloadService.class.getName())
            .setUniqueId("payload-service")
            .setRef(new PayloadService() {
                @Override
                public Map<String, Object> submit(Map<String, Object> payload) {
                    Map<String, Object> response = new LinkedHashMap<String, Object>();
                    response.put("accepted", Boolean.TRUE);
                    response.put("requestId", payload.get("requestId"));

                    Map<String, Object> customer = castMap(payload.get("customer"));
                    List<Map<String, Object>> lines = castListOfMaps(payload.get("lines"));
                    Map<String, Object> meta = castMap(payload.get("meta"));

                    response.put("customerName", customer == null ? null : customer.get("name"));
                    response.put("city", customer == null ? null : castMap(customer.get("address")).get("city"));
                    response.put("lineCount", Integer.valueOf(lines == null ? 0 : lines.size()));

                    List<String> skuSummary = new ArrayList<String>();
                    if (lines != null) {
                        for (Map<String, Object> line : lines) {
                            skuSummary.add(line.get("sku") + "x" + line.get("quantity"));
                        }
                    }
                    response.put("skuSummary", skuSummary);
                    response.put("channel", meta == null ? null : meta.get("channel"));
                    response.put("raw", payload);
                    return response;
                }
            })
            .setRegister(true)
            .setRegistry(registryConfig)
            .setServer(serverConfig);

        providerConfig.export();
        System.out.println("payload-provider-registry-ready");
        new CountDownLatch(1).await();
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> castMap(Object value) {
        if (value instanceof Map) {
            return (Map<String, Object>) value;
        }
        return null;
    }

    @SuppressWarnings("unchecked")
    private static List<Map<String, Object>> castListOfMaps(Object value) {
        if (!(value instanceof List)) {
            return null;
        }
        List<?> input = (List<?>) value;
        List<Map<String, Object>> maps = new ArrayList<Map<String, Object>>(input.size());
        for (Object item : input) {
            maps.add((Map<String, Object>) item);
        }
        return maps;
    }
}
