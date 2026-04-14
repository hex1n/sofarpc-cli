package com.hex1n.sofarpc.worker;

import com.alipay.hessian.generic.model.GenericObject;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
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

    @Test
    void convertsRawPayloadUsingDeclaredDtoType() throws Exception {
        Object[] values = converter.convertArguments(
            PayloadMode.RAW,
            Arrays.asList("com.hex1n.sofarpc.worker.PayloadConverterTest$ImportRequest"),
            Arrays.asList("com.hex1n.sofarpc.worker.PayloadConverterTest$ImportRequest"),
            new ObjectMapper().readTree("[{\"fundItems\":[{\"fundCode\":\"F0001\"}]}]")
        );

        assertTrue(values[0] instanceof ImportRequest);
        ImportRequest request = (ImportRequest) values[0];
        assertEquals("F0001", request.getFundItems().get(0).getFundCode());
    }

    @Test
    void convertsSchemaPayloadUsingNestedGenericSignature() throws Exception {
        Object[] values = converter.convertArguments(
            PayloadMode.SCHEMA,
            Arrays.asList("java.util.List"),
            Arrays.asList("java.util.List<com.hex1n.sofarpc.worker.PayloadConverterTest$FundAssetItem>"),
            new ObjectMapper().readTree("[[{\"fundCode\":\"F001\"}]]")
        );

        assertTrue(values[0] instanceof List);
        Object item = ((List<?>) values[0]).get(0);
        assertTrue(item instanceof FundAssetItem);
        assertEquals("F001", ((FundAssetItem) item).getFundCode());
    }

    @Test
    void convertsSchemaPayloadUsingGenericMapSignature() throws Exception {
        Object[] values = converter.convertArguments(
            PayloadMode.SCHEMA,
            Arrays.asList("java.util.Map"),
            Arrays.asList("java.util.Map<java.lang.String, java.util.List<com.hex1n.sofarpc.worker.PayloadConverterTest$FundAssetItem>>"),
            new ObjectMapper().readTree("[{\"items\":[{\"fundCode\":\"F002\"}]}]")
        );

        assertTrue(values[0] instanceof Map);
        Object items = ((Map<?, ?>) values[0]).get("items");
        assertTrue(items instanceof ArrayList);
        assertTrue(((List<?>) items).get(0) instanceof FundAssetItem);
        assertEquals("F002", ((FundAssetItem) ((List<?>) items).get(0)).getFundCode());
    }

    public static class FundAssetItem {
        private String fundCode;

        public String getFundCode() {
            return fundCode;
        }

        public void setFundCode(String fundCode) {
            this.fundCode = fundCode;
        }
    }

    public static class ImportRequest {
        private List<FundAssetItem> fundItems;

        public List<FundAssetItem> getFundItems() {
            return fundItems;
        }

        public void setFundItems(List<FundAssetItem> fundItems) {
            this.fundItems = fundItems;
        }
    }
}
