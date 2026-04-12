package com.example;

import java.math.BigDecimal;
import java.util.List;

public class OrderReceipt {
    private boolean accepted;
    private String requestId;
    private String customerName;
    private String city;
    private Integer lineCount;
    private BigDecimal totalAmount;
    private List<String> skuSummary;
    private String channel;

    public boolean isAccepted() {
        return accepted;
    }

    public void setAccepted(boolean accepted) {
        this.accepted = accepted;
    }

    public String getRequestId() {
        return requestId;
    }

    public void setRequestId(String requestId) {
        this.requestId = requestId;
    }

    public String getCustomerName() {
        return customerName;
    }

    public void setCustomerName(String customerName) {
        this.customerName = customerName;
    }

    public String getCity() {
        return city;
    }

    public void setCity(String city) {
        this.city = city;
    }

    public Integer getLineCount() {
        return lineCount;
    }

    public void setLineCount(Integer lineCount) {
        this.lineCount = lineCount;
    }

    public BigDecimal getTotalAmount() {
        return totalAmount;
    }

    public void setTotalAmount(BigDecimal totalAmount) {
        this.totalAmount = totalAmount;
    }

    public List<String> getSkuSummary() {
        return skuSummary;
    }

    public void setSkuSummary(List<String> skuSummary) {
        this.skuSummary = skuSummary;
    }

    public String getChannel() {
        return channel;
    }

    public void setChannel(String channel) {
        this.channel = channel;
    }
}
