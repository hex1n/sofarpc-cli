package com.example;

import com.alipay.sofa.rpc.codec.Serializer;
import com.alipay.sofa.rpc.codec.SerializerFactory;
import com.alipay.sofa.rpc.common.RpcConstants;
import com.alipay.sofa.rpc.core.response.SofaResponse;
import com.alipay.sofa.rpc.transport.AbstractByteBuf;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;

import java.io.IOException;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.Map;

public final class WireFixtureGenerator {
    private static final ObjectMapper MAPPER = new ObjectMapper()
            .enable(SerializationFeature.ORDER_MAP_ENTRIES_BY_KEYS);

    private WireFixtureGenerator() {
    }

    public static void main(String[] args) throws Exception {
        Path outputDir = args.length > 0
                ? Paths.get(args[0])
                : Paths.get("..", "golden");
        Files.createDirectories(outputDir);
        writeResponseSuccessFixture(outputDir);
        writeResponseErrorFixture(outputDir);
    }

    private static void writeResponseSuccessFixture(Path outputDir) throws IOException {
        FixtureItem primary = new FixtureItem("primary", new BigDecimal("1000.50"), FixtureStatus.ACTIVE.name());
        ArrayList<FixtureItem> items = new ArrayList<FixtureItem>();
        items.add(primary);
        Map<String, FixtureItem> itemByCode = new LinkedHashMap<String, FixtureItem>();
        itemByCode.put("primary", primary);

        FixtureResult result = new FixtureResult(
                true,
                "ok",
                primary,
                items,
                itemByCode,
                new BigInteger("42"),
                FixtureStatus.ACTIVE);

        SofaResponse response = new SofaResponse();
        response.setAppResponse(result);
        response.addResponseProp("traceId", "fixture-trace");

        writeFixture(outputDir, "response-success-dto.json", object(
                "name", "Java-encoded success response with DTO, enum, BigDecimal, list and map",
                "kind", "response-content",
                "contentHex", hex(encode(response)),
                "want", object(
                        "isError", false,
                        "appResponseType", "com.example.FixtureResult",
                        "appResponseJson", object(
                                "type", "com.example.FixtureResult",
                                "fields", object(
                                        "success", true,
                                        "message", "ok",
                                        "primary", fixtureItemJson(),
                                        "items", Arrays.asList(fixtureItemJson()),
                                        "itemByCode", object(
                                                "type", "java.util.LinkedHashMap",
                                                "map", object("primary", fixtureItemJson())),
                                        "count", object("type", "java.math.BigInteger"),
                                        "status", object(
                                                "type", "com.example.FixtureStatus",
                                                "fields", object("name", "ACTIVE")))),
                        "responseProps", object("traceId", "fixture-trace"))));
    }

    private static void writeResponseErrorFixture(Path outputDir) throws IOException {
        SofaResponse response = new SofaResponse();
        response.setErrorMsg("fixture error");
        response.addResponseProp("traceId", "fixture-error-trace");

        writeFixture(outputDir, "response-error.json", object(
                "name", "Java-encoded SofaResponse error",
                "kind", "response-content",
                "contentHex", hex(encode(response)),
                "want", object(
                        "isError", true,
                        "errorMsg", "fixture error",
                        "responseProps", object("traceId", "fixture-error-trace"))));
    }

    private static Map<String, Object> fixtureItemJson() {
        return object(
                "type", "com.example.FixtureItem",
                "fields", object(
                        "code", "primary",
                        "amount", object(
                                "type", "java.math.BigDecimal",
                                "value", "1000.50"),
                        "status", "ACTIVE"));
    }

    private static void writeFixture(Path outputDir, String fileName, Map<String, Object> fixture) throws IOException {
        String json = MAPPER.writerWithDefaultPrettyPrinter().writeValueAsString(fixture) + "\n";
        Files.write(outputDir.resolve(fileName), json.getBytes(StandardCharsets.UTF_8));
    }

    private static Map<String, Object> object(Object... keyValues) {
        Map<String, Object> out = new LinkedHashMap<String, Object>();
        if ((keyValues.length % 2) != 0) {
            throw new IllegalArgumentException("object requires key/value pairs");
        }
        for (int i = 0; i < keyValues.length; i += 2) {
            out.put((String) keyValues[i], keyValues[i + 1]);
        }
        return out;
    }

    private static byte[] encode(Object value) {
        Serializer serializer = SerializerFactory.getSerializer(RpcConstants.SERIALIZE_HESSIAN2);
        AbstractByteBuf buffer = serializer.encode(value, new LinkedHashMap<String, String>());
        try {
            return Arrays.copyOf(buffer.array(), buffer.readableBytes());
        } finally {
            buffer.release();
        }
    }

    private static String hex(byte[] bytes) {
        char[] chars = new char[bytes.length * 2];
        final char[] alphabet = "0123456789abcdef".toCharArray();
        for (int i = 0; i < bytes.length; i++) {
            int value = bytes[i] & 0xff;
            chars[i * 2] = alphabet[value >>> 4];
            chars[i * 2 + 1] = alphabet[value & 0x0f];
        }
        return new String(chars);
    }
}
