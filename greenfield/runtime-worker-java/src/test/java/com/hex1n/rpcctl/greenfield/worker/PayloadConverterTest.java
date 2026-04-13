package com.hex1n.rpcctl.greenfield.worker;

import com.alipay.hessian.generic.model.GenericObject;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

import java.util.Arrays;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

public class PayloadConverterTest {
    private final PayloadConverter converter = new PayloadConverter(new ObjectMapper());

    @Test
    void convertsGenericObjectPayload() throws Exception {
        Object[] values = converter.convertArguments(
            PayloadMode.GENERIC,
            Arrays.asList("com.example.User"),
            new ObjectMapper().readTree("[{\"@type\":\"com.example.User\",\"name\":\"alice\"}]")
        );

        assertTrue(values[0] instanceof GenericObject);
        GenericObject object = (GenericObject) values[0];
        assertEquals("com.example.User", object.getType());
        assertEquals("alice", object.getField("name"));
    }

    @Test
    void convertsRawMapPayload() throws Exception {
        Object[] values = converter.convertArguments(
            PayloadMode.RAW,
            Arrays.asList("java.util.Map"),
            new ObjectMapper().readTree("[{\"id\":1}]")
        );

        assertTrue(values[0] instanceof Map);
        assertEquals(1, ((Map<?, ?>) values[0]).get("id"));
    }

    @Test
    void resolvesArrayTypeNames() {
        assertEquals(String[].class, PayloadConverter.resolveType("java.lang.String[]"));
    }
}
