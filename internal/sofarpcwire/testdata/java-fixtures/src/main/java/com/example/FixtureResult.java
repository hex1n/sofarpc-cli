package com.example;

import java.io.Serializable;
import java.math.BigInteger;
import java.util.List;
import java.util.Map;

public class FixtureResult implements Serializable {
    private static final long serialVersionUID = 1L;

    public boolean success;
    public String message;
    public FixtureItem primary;
    public List<FixtureItem> items;
    public Map<String, FixtureItem> itemByCode;
    public BigInteger count;
    public FixtureStatus status;

    public FixtureResult() {
    }

    public FixtureResult(
            boolean success,
            String message,
            FixtureItem primary,
            List<FixtureItem> items,
            Map<String, FixtureItem> itemByCode,
            BigInteger count,
            FixtureStatus status) {
        this.success = success;
        this.message = message;
        this.primary = primary;
        this.items = items;
        this.itemByCode = itemByCode;
        this.count = count;
        this.status = status;
    }
}
