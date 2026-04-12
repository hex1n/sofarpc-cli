package com.example;

import com.alipay.sofa.rpc.config.ApplicationConfig;
import com.alipay.sofa.rpc.config.ProviderConfig;
import com.alipay.sofa.rpc.config.ServerConfig;

import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;

public final class OrderProviderMain {

    private OrderProviderMain() {
    }

    public static void main(String[] args) throws Exception {
        int port = Integer.parseInt(System.getProperty("rpcctl.e2e.port", "12241"));

        ServerConfig serverConfig = new ServerConfig()
            .setProtocol("bolt")
            .setHost("127.0.0.1")
            .setPort(port)
            .setDaemon(false);

        ProviderConfig<OrderService> providerConfig = new ProviderConfig<OrderService>()
            .setApplication(new ApplicationConfig().setAppName("rpcctl-e2e-order-provider"))
            .setInterfaceId(OrderService.class.getName())
            .setUniqueId("order-service")
            .setRef(new OrderService() {
                @Override
                public OrderReceipt submit(OrderRequest request) {
                    OrderReceipt receipt = new OrderReceipt();
                    receipt.setAccepted(true);
                    receipt.setRequestId(request.getRequestId());
                    receipt.setCustomerName(request.getCustomer() == null ? null : request.getCustomer().getName());
                    receipt.setCity(request.getCustomer() == null || request.getCustomer().getAddress() == null
                        ? null
                        : request.getCustomer().getAddress().getCity());

                    List<OrderLine> lines = request.getLines();
                    receipt.setLineCount(lines == null ? Integer.valueOf(0) : Integer.valueOf(lines.size()));

                    BigDecimal totalAmount = BigDecimal.ZERO;
                    List<String> skuSummary = new ArrayList<String>();
                    if (lines != null) {
                        for (OrderLine line : lines) {
                            if (line.getPrice() != null && line.getQuantity() != null) {
                                totalAmount = totalAmount.add(line.getPrice().multiply(BigDecimal.valueOf(line.getQuantity().longValue())));
                            }
                            skuSummary.add(line.getSku() + "x" + line.getQuantity());
                        }
                    }
                    receipt.setTotalAmount(totalAmount);
                    receipt.setSkuSummary(skuSummary);

                    Map<String, String> attributes = request.getAttributes();
                    receipt.setChannel(attributes == null ? null : attributes.get("channel"));
                    return receipt;
                }
            })
            .setServer(serverConfig);

        providerConfig.export();
        System.out.println("order-provider-ready");
        new CountDownLatch(1).await();
    }
}
