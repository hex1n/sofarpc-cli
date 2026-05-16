package com.example;

import java.io.Serializable;
import java.math.BigDecimal;
import java.util.List;

public class FixtureRequest implements Serializable {
    private static final long serialVersionUID = 1L;

    public long id;
    public BigDecimal amount;
    public List<FixtureItem> items;
    public FixtureStatus status;

    public FixtureRequest() {
    }
}
