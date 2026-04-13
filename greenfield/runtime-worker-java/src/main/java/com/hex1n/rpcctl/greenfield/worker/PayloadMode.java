package com.hex1n.rpcctl.greenfield.worker;

public enum PayloadMode {
    RAW("raw"),
    GENERIC("generic"),
    SCHEMA("schema");

    private final String value;

    PayloadMode(String value) {
        this.value = value;
    }

    public String value() {
        return value;
    }

    public static PayloadMode fromValue(String raw) {
        if (raw == null || raw.trim().isEmpty()) {
            return RAW;
        }
        for (PayloadMode candidate : values()) {
            if (candidate.value.equalsIgnoreCase(raw)) {
                return candidate;
            }
        }
        throw new IllegalArgumentException("unsupported payload mode: " + raw);
    }
}
