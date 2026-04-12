package com.hex1n.sofarpcctl;

import com.alipay.hessian.generic.model.GenericObject;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;

import java.lang.reflect.Array;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Date;
import java.util.IdentityHashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.UUID;

public final class InvocationPayloads {

    private InvocationPayloads() {
    }

    public static ResolvedPayloads resolve(List<String> rawTypes, String argsJson) {
        ArrayNode jsonArgs = parseArgs(argsJson);
        if (rawTypes.isEmpty() && jsonArgs.size() > 0) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Argument types are required when --args is not empty."
            );
        }
        if (rawTypes.size() != jsonArgs.size()) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Parameter count mismatch: types=" + rawTypes.size() + ", args=" + jsonArgs.size()
            );
        }

        List<String> rpcTypes = new ArrayList<String>(rawTypes.size());
        Object[] arguments = new Object[rawTypes.size()];
        boolean genericCallRequired = false;
        for (int index = 0; index < rawTypes.size(); index++) {
            String rawType = rawTypes.get(index);
            String rpcType = TypeNameUtils.normalizeForRpc(rawType);
            Object argument = convert(jsonArgs.get(index), rawType);
            rpcTypes.add(rpcType);
            arguments[index] = argument;
            if (!TypeNameUtils.isLocallyResolvable(rawType) || containsGenericObject(argument)) {
                genericCallRequired = true;
            }
        }
        return new ResolvedPayloads(rpcTypes, arguments, genericCallRequired);
    }

    public static Object normalizeResult(Object value) {
        return normalizeResult(value, new IdentityHashMap<Object, Boolean>());
    }

    private static ArrayNode parseArgs(String argsJson) {
        String candidate = argsJson == null || argsJson.trim().isEmpty() ? "[]" : argsJson.trim();
        try {
            JsonNode jsonNode = ConfigLoader.json().readTree(candidate);
            if (jsonNode == null || jsonNode.isNull()) {
                return ConfigLoader.json().createArrayNode();
            }
            if (!jsonNode.isArray()) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "--args must be a JSON array."
                );
            }
            return (ArrayNode) jsonNode;
        } catch (Exception exception) {
            if (exception instanceof CliException) {
                throw (CliException) exception;
            }
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Failed to parse --args JSON array.",
                exception
            );
        }
    }

    private static Object convert(JsonNode node, String declaredType) {
        if (node == null || node.isNull()) {
            return null;
        }

        String effectiveType = declaredType;
        if (node.isObject() && node.has("@type")) {
            effectiveType = node.get("@type").asText();
        }

        if (TypeNameUtils.isArrayType(effectiveType)) {
            return convertArray(node, effectiveType);
        }
        if (TypeNameUtils.isCollectionType(effectiveType)) {
            return convertCollection(node);
        }
        if (TypeNameUtils.isMapType(effectiveType)) {
            return convertMap(node);
        }
        if (TypeNameUtils.isSimpleScalar(effectiveType)) {
            return convertScalar(node, effectiveType);
        }
        if (node.isObject()) {
            return convertGenericObject((ObjectNode) node, effectiveType);
        }
        if (node.isArray()) {
            return convertCollection(node);
        }
        return ConfigLoader.json().convertValue(node, Object.class);
    }

    private static Object convertScalar(JsonNode node, String declaredType) {
        String normalizedType = TypeNameUtils.normalizeForRpc(declaredType);
        if (normalizedType == null || "java.lang.String".equals(normalizedType)) {
            return node.isTextual() ? node.asText() : node.toString();
        }
        if ("boolean".equals(normalizedType) || "java.lang.Boolean".equals(normalizedType)) {
            return node.asBoolean();
        }
        if ("byte".equals(normalizedType) || "java.lang.Byte".equals(normalizedType)) {
            return Byte.valueOf((byte) node.asInt());
        }
        if ("char".equals(normalizedType) || "java.lang.Character".equals(normalizedType)) {
            String text = node.asText();
            if (text.length() != 1) {
                throw new CliException(
                    ExitCodes.PARAMETER_ERROR,
                    "Expected a single character value for type " + declaredType
                );
            }
            return Character.valueOf(text.charAt(0));
        }
        if ("short".equals(normalizedType) || "java.lang.Short".equals(normalizedType)) {
            return Short.valueOf((short) node.asInt());
        }
        if ("int".equals(normalizedType) || "java.lang.Integer".equals(normalizedType)) {
            return Integer.valueOf(node.asInt());
        }
        if ("long".equals(normalizedType) || "java.lang.Long".equals(normalizedType)) {
            return Long.valueOf(node.asLong());
        }
        if ("float".equals(normalizedType) || "java.lang.Float".equals(normalizedType)) {
            return Float.valueOf((float) node.asDouble());
        }
        if ("double".equals(normalizedType) || "java.lang.Double".equals(normalizedType)) {
            return Double.valueOf(node.asDouble());
        }
        if ("java.math.BigDecimal".equals(normalizedType)) {
            return node.isNumber() ? node.decimalValue() : new BigDecimal(node.asText());
        }
        if ("java.math.BigInteger".equals(normalizedType)) {
            return node.isNumber() ? node.bigIntegerValue() : new BigInteger(node.asText());
        }
        if ("java.util.Date".equals(normalizedType)) {
            if (node.isNumber()) {
                return new Date(node.asLong());
            }
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "java.util.Date must be provided as epoch milliseconds."
            );
        }
        if ("java.util.UUID".equals(normalizedType)) {
            return UUID.fromString(node.asText());
        }
        return ConfigLoader.json().convertValue(node, Object.class);
    }

    private static Object convertArray(JsonNode node, String declaredType) {
        if (!node.isArray()) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Expected a JSON array for type " + declaredType
            );
        }
        ArrayNode arrayNode = (ArrayNode) node;
        String componentType = TypeNameUtils.componentType(declaredType);
        List<Object> values = new ArrayList<Object>(arrayNode.size());
        for (int index = 0; index < arrayNode.size(); index++) {
            values.add(convert(arrayNode.get(index), componentType));
        }

        try {
            Class<?> componentClass = TypeNameUtils.classFor(componentType);
            Object array = Array.newInstance(componentClass, values.size());
            for (int index = 0; index < values.size(); index++) {
                Array.set(array, index, values.get(index));
            }
            return array;
        } catch (Exception exception) {
            return values.toArray(new Object[0]);
        }
    }

    private static Object convertCollection(JsonNode node) {
        if (!node.isArray()) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Expected a JSON array for collection type."
            );
        }
        List<Object> values = new ArrayList<Object>(node.size());
        for (int index = 0; index < node.size(); index++) {
            values.add(convert(node.get(index), null));
        }
        return values;
    }

    private static Object convertMap(JsonNode node) {
        if (!node.isObject()) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Expected a JSON object for map type."
            );
        }
        Map<String, Object> values = new LinkedHashMap<String, Object>();
        java.util.Iterator<String> fieldNames = node.fieldNames();
        while (fieldNames.hasNext()) {
            String fieldName = fieldNames.next();
            JsonNode fieldValue = node.get(fieldName);
            values.put(fieldName, convert(fieldValue, null));
        }
        return values;
    }

    private static GenericObject convertGenericObject(ObjectNode node, String declaredType) {
        if (declaredType == null || declaredType.trim().isEmpty()) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Complex objects require a declared type or an @type field."
            );
        }
        GenericObject genericObject = new GenericObject(TypeNameUtils.normalizeForRpc(declaredType));
        java.util.Iterator<String> fieldNames = node.fieldNames();
        while (fieldNames.hasNext()) {
            String fieldName = fieldNames.next();
            if ("@type".equals(fieldName)) {
                continue;
            }
            JsonNode fieldValue = node.get(fieldName);
            genericObject.putField(fieldName, convert(fieldValue, null));
        }
        return genericObject;
    }

    private static boolean containsGenericObject(Object value) {
        if (value == null) {
            return false;
        }
        if (value instanceof GenericObject) {
            return true;
        }
        if (value instanceof Map) {
            for (Object nested : ((Map<?, ?>) value).values()) {
                if (containsGenericObject(nested)) {
                    return true;
                }
            }
        }
        if (value instanceof Collection) {
            for (Object nested : (Collection<?>) value) {
                if (containsGenericObject(nested)) {
                    return true;
                }
            }
        }
        if (value.getClass().isArray()) {
            int length = Array.getLength(value);
            for (int index = 0; index < length; index++) {
                if (containsGenericObject(Array.get(value, index))) {
                    return true;
                }
            }
        }
        return false;
    }

    private static Object normalizeResult(Object value, IdentityHashMap<Object, Boolean> visited) {
        if (value == null
            || value instanceof String
            || value instanceof Number
            || value instanceof Boolean
            || value instanceof Character) {
            return value;
        }
        if (visited.containsKey(value)) {
            return "[cycle]";
        }
        visited.put(value, Boolean.TRUE);

        if (value instanceof GenericObject) {
            return normalizeGenericObject((GenericObject) value, visited);
        }
        if (value instanceof Map) {
            Map<String, Object> normalized = new LinkedHashMap<String, Object>();
            for (Map.Entry<?, ?> entry : ((Map<?, ?>) value).entrySet()) {
                normalized.put(String.valueOf(entry.getKey()), normalizeResult(entry.getValue(), visited));
            }
            return normalized;
        }
        if (value instanceof Collection) {
            List<Object> normalized = new ArrayList<Object>();
            for (Object item : (Collection<?>) value) {
                normalized.add(normalizeResult(item, visited));
            }
            return normalized;
        }
        if (value.getClass().isArray()) {
            int length = Array.getLength(value);
            List<Object> normalized = new ArrayList<Object>(length);
            for (int index = 0; index < length; index++) {
                normalized.add(normalizeResult(Array.get(value, index), visited));
            }
            return normalized;
        }
        try {
            return ConfigLoader.json().convertValue(value, Object.class);
        } catch (IllegalArgumentException exception) {
            return String.valueOf(value);
        }
    }

    private static Map<String, Object> normalizeGenericObject(GenericObject value, IdentityHashMap<Object, Boolean> visited) {
        Map<String, Object> normalized = new LinkedHashMap<String, Object>();
        normalized.put("@type", readGenericObjectType(value));
        Map<String, Object> fields = readGenericObjectFields(value);
        for (Map.Entry<String, Object> entry : fields.entrySet()) {
            normalized.put(entry.getKey(), normalizeResult(entry.getValue(), visited));
        }
        return normalized;
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> readGenericObjectFields(GenericObject value) {
        try {
            Method method = value.getClass().getMethod("getFields");
            return (Map<String, Object>) method.invoke(value);
        } catch (Exception ignored) {
            try {
                Field field = value.getClass().getDeclaredField("fields");
                field.setAccessible(true);
                return (Map<String, Object>) field.get(value);
            } catch (Exception exception) {
                return new LinkedHashMap<String, Object>();
            }
        }
    }

    private static Object readGenericObjectType(GenericObject value) {
        try {
            Method method = value.getClass().getMethod("getType");
            return method.invoke(value);
        } catch (Exception ignored) {
            try {
                Field field = value.getClass().getDeclaredField("type");
                field.setAccessible(true);
                return field.get(value);
            } catch (Exception exception) {
                return value.getClass().getName();
            }
        }
    }

    public static final class ResolvedPayloads {
        private final List<String> paramTypes;
        private final Object[] arguments;
        private final boolean genericCallRequired;

        public ResolvedPayloads(List<String> paramTypes, Object[] arguments, boolean genericCallRequired) {
            this.paramTypes = paramTypes;
            this.arguments = arguments;
            this.genericCallRequired = genericCallRequired;
        }

        public List<String> getParamTypes() {
            return paramTypes;
        }

        public String[] getParamTypesArray() {
            return paramTypes.toArray(new String[0]);
        }

        public Object[] getArguments() {
            return arguments;
        }

        public boolean isGenericCallRequired() {
            return genericCallRequired;
        }
    }
}
