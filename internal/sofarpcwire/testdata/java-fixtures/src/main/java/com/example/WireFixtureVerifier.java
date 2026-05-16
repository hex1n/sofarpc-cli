package com.example;

import com.alipay.hessian.generic.model.GenericCollection;
import com.alipay.hessian.generic.model.GenericMap;
import com.alipay.hessian.generic.model.GenericObject;
import com.alipay.sofa.rpc.codec.Serializer;
import com.alipay.sofa.rpc.codec.SerializerFactory;
import com.alipay.sofa.rpc.common.RpcConstants;
import com.alipay.sofa.rpc.core.request.SofaRequest;
import com.alipay.sofa.rpc.transport.ByteArrayWrapperByteBuf;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;

import java.io.IOException;
import java.lang.reflect.Field;
import java.lang.reflect.Modifier;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;

public final class WireFixtureVerifier {
    private static final ObjectMapper MAPPER = new ObjectMapper()
            .enable(SerializationFeature.ORDER_MAP_ENTRIES_BY_KEYS);

    private WireFixtureVerifier() {
    }

    public static void main(String[] args) throws Exception {
        Path goldenDir = args.length > 0
                ? Paths.get(args[0])
                : Paths.get("..", "golden");

        int requestFixtures = 0;
        int responseFixtures = 0;
        try (DirectoryStream<Path> stream = Files.newDirectoryStream(goldenDir, "*.json")) {
            for (Path fixturePath : stream) {
                JsonNode fixture = MAPPER.readTree(fixturePath.toFile());
                String kind = textAt(fixture, "kind", fixturePath);
                if ("request-content".equals(kind)) {
                    requestFixtures++;
                    verifyRequestFixture(fixturePath, fixture);
                } else if ("response-content".equals(kind)) {
                    responseFixtures++;
                } else {
                    throw new IllegalArgumentException(fixturePath + ": unknown kind " + kind);
                }
            }
        }

        if (requestFixtures == 0) {
            throw new IllegalStateException("no request-content fixtures in " + goldenDir);
        }
        if (responseFixtures == 0) {
            throw new IllegalStateException("no response-content fixtures in " + goldenDir);
        }
    }

    private static void verifyRequestFixture(Path fixturePath, JsonNode fixture) throws IOException {
        JsonNode want = fixture.path("want");
        SofaRequest request = decodeRequest(hex(textAt(fixture, "contentHex", fixturePath)));

        assertEquals(fixturePath, "want.service", textAt(want, "service", fixturePath), request.getInterfaceName());
        assertEquals(fixturePath, "want.method", textAt(want, "method", fixturePath), request.getMethodName());
        assertEquals(
                fixturePath,
                "want.targetServiceUniqueName",
                textAt(want, "targetServiceUniqueName", fixturePath),
                request.getTargetServiceUniqueName());

        JsonNode expectedParamTypes = want.path("paramTypes");
        JsonNode actualParamTypes = MAPPER.valueToTree(Arrays.asList(request.getMethodArgSigs()));
        assertJsonEquals(fixturePath, "want.paramTypes", expectedParamTypes, actualParamTypes);

        JsonNode expectedArgs = want.path("argsJson");
        if (expectedArgs.isMissingNode()) {
            throw new IllegalArgumentException(fixturePath + ": missing want.argsJson");
        }
        JsonNode actualArgs = MAPPER.valueToTree(canonicalizeArgs(request.getMethodArgs()));
        assertJsonEquals(fixturePath, "want.argsJson", expectedArgs, actualArgs);
    }

    private static SofaRequest decodeRequest(byte[] content) {
        Serializer serializer = SerializerFactory.getSerializer(RpcConstants.SERIALIZE_HESSIAN2);
        return (SofaRequest) serializer.decode(
                new ByteArrayWrapperByteBuf(content),
                SofaRequest.class,
                new LinkedHashMap<String, String>());
    }

    private static List<Object> canonicalizeArgs(Object[] args) {
        List<Object> canonical = new ArrayList<Object>();
        for (Object arg : args) {
            canonical.add(canonicalize(arg));
        }
        return canonical;
    }

    private static Object canonicalize(Object value) {
        if (value == null
                || value instanceof String
                || value instanceof Boolean
                || value instanceof Integer
                || value instanceof Double
                || value instanceof Float) {
            return value;
        }
        if (value instanceof Long) {
            long number = ((Long) value).longValue();
            if (number >= Integer.MIN_VALUE && number <= Integer.MAX_VALUE) {
                return Integer.valueOf((int) number);
            }
            return value;
        }
        if (value instanceof Short || value instanceof Byte) {
            return Integer.valueOf(((Number) value).intValue());
        }
        if (value instanceof BigDecimal) {
            return canonicalNumberObject("java.math.BigDecimal", ((BigDecimal) value).toPlainString());
        }
        if (value instanceof BigInteger) {
            return canonicalNumberObject("java.math.BigInteger", value.toString());
        }
        if (value instanceof Enum<?>) {
            return ((Enum<?>) value).name();
        }
        if (value instanceof GenericObject) {
            GenericObject generic = (GenericObject) value;
            if (isGenericNumber(generic, "java.math.BigDecimal") || isGenericNumber(generic, "java.math.BigInteger")) {
                return canonicalNumberObject(generic.getType(), String.valueOf(generic.getField("value")));
            }
            Map<String, Object> out = new LinkedHashMap<String, Object>();
            Map<String, Object> fields = new TreeMap<String, Object>();
            for (String fieldName : generic.getFieldNames()) {
                fields.put(fieldName, canonicalize(generic.getField(fieldName)));
            }
            out.put("type", generic.getType());
            out.put("fields", fields);
            return out;
        }
        if (value instanceof GenericCollection) {
            GenericCollection generic = (GenericCollection) value;
            Map<String, Object> out = new LinkedHashMap<String, Object>();
            out.put("type", generic.getType());
            out.put("items", canonicalizeCollection(generic.getCollection()));
            return out;
        }
        if (value instanceof GenericMap) {
            GenericMap generic = (GenericMap) value;
            Map<String, Object> out = new LinkedHashMap<String, Object>();
            out.put("type", generic.getType());
            out.put("map", canonicalizeMap(generic.getMap()));
            return out;
        }
        if (value instanceof Collection<?>) {
            Map<String, Object> out = new LinkedHashMap<String, Object>();
            out.put("type", value.getClass().getName());
            out.put("items", canonicalizeCollection((Collection<?>) value));
            return out;
        }
        if (value instanceof Map<?, ?>) {
            return canonicalizeMap((Map<?, ?>) value);
        }
        if (value.getClass().isArray()) {
            Object[] values = (Object[]) value;
            return canonicalizeCollection(Arrays.asList(values));
        }
        if (value.getClass().getName().startsWith("com.example.")) {
            return canonicalizePojo(value);
        }

        throw new IllegalArgumentException("unsupported decoded value type: " + value.getClass().getName());
    }

    private static Map<String, Object> canonicalNumberObject(String type, String value) {
        Map<String, Object> out = new LinkedHashMap<String, Object>();
        out.put("type", type);
        out.put("value", value);
        return out;
    }

    private static boolean isGenericNumber(GenericObject generic, String type) {
        return type.equals(generic.getType()) && generic.hasField("value");
    }

    private static Map<String, Object> canonicalizePojo(Object value) {
        Map<String, Object> out = new LinkedHashMap<String, Object>();
        Map<String, Object> fields = new TreeMap<String, Object>();
        for (Field field : value.getClass().getFields()) {
            if (Modifier.isStatic(field.getModifiers())) {
                continue;
            }
            try {
                fields.put(field.getName(), canonicalize(field.get(value)));
            } catch (IllegalAccessException e) {
                throw new IllegalArgumentException(
                        "cannot read field " + value.getClass().getName() + "." + field.getName(), e);
            }
        }
        out.put("type", value.getClass().getName());
        out.put("fields", fields);
        return out;
    }

    private static List<Object> canonicalizeCollection(Collection<?> collection) {
        List<Object> items = new ArrayList<Object>();
        for (Object item : collection) {
            items.add(canonicalize(item));
        }
        return items;
    }

    private static Map<String, Object> canonicalizeMap(Map<?, ?> map) {
        Map<String, Object> out = new TreeMap<String, Object>();
        for (Map.Entry<?, ?> entry : map.entrySet()) {
            out.put(String.valueOf(entry.getKey()), canonicalize(entry.getValue()));
        }
        return out;
    }

    private static String textAt(JsonNode node, String field, Path fixturePath) {
        JsonNode value = node.path(field);
        if (!value.isTextual()) {
            throw new IllegalArgumentException(fixturePath + ": missing string field " + field);
        }
        return value.asText();
    }

    private static void assertEquals(Path fixturePath, String field, String expected, String actual) {
        if (!expected.equals(actual)) {
            throw new IllegalArgumentException(
                    fixturePath + ": " + field + " mismatch: expected " + expected + ", got " + actual);
        }
    }

    private static void assertJsonEquals(Path fixturePath, String field, JsonNode expected, JsonNode actual)
            throws IOException {
        if (!expected.equals(actual)) {
            throw new IllegalArgumentException(
                    fixturePath + ": " + field + " mismatch\nexpected:\n"
                            + MAPPER.writerWithDefaultPrettyPrinter().writeValueAsString(expected)
                            + "\nactual:\n"
                            + MAPPER.writerWithDefaultPrettyPrinter().writeValueAsString(actual));
        }
    }

    private static byte[] hex(String value) {
        if ((value.length() % 2) != 0) {
            throw new IllegalArgumentException("hex length must be even");
        }
        byte[] bytes = new byte[value.length() / 2];
        for (int i = 0; i < bytes.length; i++) {
            int hi = Character.digit(value.charAt(i * 2), 16);
            int lo = Character.digit(value.charAt(i * 2 + 1), 16);
            if (hi < 0 || lo < 0) {
                throw new IllegalArgumentException("invalid hex at byte " + i);
            }
            bytes[i] = (byte) ((hi << 4) | lo);
        }
        return bytes;
    }
}
