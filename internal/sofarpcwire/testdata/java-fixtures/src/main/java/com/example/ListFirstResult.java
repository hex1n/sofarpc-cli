package com.example;

import java.io.Serializable;
import java.math.BigDecimal;
import java.util.List;

public class ListFirstResult implements Serializable {
    private static final long serialVersionUID = 1L;

    public List<FixtureItem> items;
    public BigDecimal laterAmount;

    public ListFirstResult() {
    }

    public ListFirstResult(List<FixtureItem> items, BigDecimal laterAmount) {
        this.items = items;
        this.laterAmount = laterAmount;
    }
}
