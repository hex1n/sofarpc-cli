package com.hex1n.sofarpc.worker;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

final class ResponseTreeWriter {
    private final ObjectMapper primary;
    private final ObjectMapper fieldOnly;

    ResponseTreeWriter(ObjectMapper primary) {
        this.primary = primary;
        this.fieldOnly = WorkerMappers.createFieldOnly(primary);
    }

    JsonNode write(Object value) {
        try {
            return primary.valueToTree(value);
        } catch (RuntimeException primaryEx) {
            try {
                return fieldOnly.valueToTree(value);
            } catch (RuntimeException fallbackEx) {
                throw primaryEx;
            }
        }
    }
}
