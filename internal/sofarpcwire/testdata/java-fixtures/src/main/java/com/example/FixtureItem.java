package com.example;

import java.io.Serializable;
import java.math.BigDecimal;

public class FixtureItem implements Serializable {
    private static final long serialVersionUID = 1L;

    public String code;
    public BigDecimal amount;
    public String status;

    public FixtureItem() {
    }

    public FixtureItem(String code, BigDecimal amount, String status) {
        this.code = code;
        this.amount = amount;
        this.status = status;
    }
}
