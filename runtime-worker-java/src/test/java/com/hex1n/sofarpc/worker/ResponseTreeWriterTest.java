package com.hex1n.sofarpc.worker;

import com.fasterxml.jackson.databind.JsonNode;
import org.junit.jupiter.api.Test;

import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

public class ResponseTreeWriterTest {
    private final ResponseTreeWriter writer = new ResponseTreeWriter(WorkerMappers.create());

    @Test
    void fallsBackToFieldSerializationWhenHelperGetterThrows() {
        Map<String, Object> item = new LinkedHashMap<String, Object>();
        item.put("fundCode", "F001");
        Map<String, Object> data = new LinkedHashMap<String, Object>();
        data.put("fundItems", Arrays.asList(item));

        JsonNode tree = writer.write(new Envelope(data));

        assertTrue(tree.get("success").asBoolean());
        assertEquals("F001", tree.get("data").get("fundItems").get(0).get("fundCode").asText());
        assertFalse(tree.has("dataOptional"));
        assertFalse(tree.has("dataOrThrow"));
    }

    @Test
    void preservesNormalBeanSerializationWhenNoFallbackIsNeeded() {
        JsonNode tree = writer.write(new PlainEnvelope(new PlainPayload("ok")));

        assertTrue(tree.get("success").asBoolean());
        assertEquals("ok", tree.get("data").get("message").asText());
    }

    private static final class Envelope {
        private final boolean success = true;
        private final Object data;

        private Envelope(Object data) {
            this.data = data;
        }

        public boolean isSuccess() {
            return success;
        }

        public Object getData() {
            return data;
        }

        public Optional<Object> getDataOptional() {
            return Optional.ofNullable(data);
        }

        public PlainPayload getDataOrThrow() {
            return (PlainPayload) data;
        }
    }

    private static final class PlainEnvelope {
        private final boolean success = true;
        private final PlainPayload data;

        private PlainEnvelope(PlainPayload data) {
            this.data = data;
        }

        public boolean isSuccess() {
            return success;
        }

        public PlainPayload getData() {
            return data;
        }
    }

    private static final class PlainPayload {
        private final String message;

        private PlainPayload(String message) {
            this.message = message;
        }

        public String getMessage() {
            return message;
        }
    }
}
