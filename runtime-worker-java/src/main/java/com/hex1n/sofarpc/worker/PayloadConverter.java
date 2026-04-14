package com.hex1n.sofarpc.worker;

import com.alipay.hessian.generic.model.GenericObject;
import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.type.TypeFactory;

import java.lang.reflect.Array;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

public final class PayloadConverter {
    private final ObjectMapper mapper;
    private final TypeFactory typeFactory;

    public PayloadConverter(ObjectMapper mapper) {
        this.mapper = mapper;
        this.typeFactory = mapper.getTypeFactory();
    }

    public Object[] convertArguments(PayloadMode mode, List<String> paramTypes, List<String> paramTypeSignatures, JsonNode args) {
        if (args == null || !args.isArray()) {
            throw new IllegalArgumentException("args must be a JSON array");
        }
        Object[] converted = new Object[args.size()];
        for (int i = 0; i < args.size(); i++) {
            String typeName = i < paramTypes.size() ? paramTypes.get(i) : null;
            String signature = i < paramTypeSignatures.size() ? paramTypeSignatures.get(i) : null;
            converted[i] = convertArgument(mode, typeName, signature, args.get(i));
        }
        return converted;
    }

    private Object convertArgument(PayloadMode mode, String typeName, String signature, JsonNode node) {
        switch (mode) {
            case RAW:
                if (typeName == null || typeName.trim().isEmpty()) {
                    return mapper.convertValue(node, Object.class);
                }
                return mapper.convertValue(node, resolveType(typeName));
            case GENERIC:
                return toGenericValue(node);
            case SCHEMA:
                if ((signature == null || signature.trim().isEmpty())
                    && (typeName == null || typeName.trim().isEmpty())) {
                    return mapper.convertValue(node, Object.class);
                }
                return mapper.convertValue(node, resolveSchemaType(firstNonBlank(signature, typeName)));
            default:
                throw new IllegalArgumentException("unsupported payload mode: " + mode);
        }
    }

    private String firstNonBlank(String primary, String fallback) {
        if (primary != null && !primary.trim().isEmpty()) {
            return primary.trim();
        }
        return fallback;
    }

    private Object toGenericValue(JsonNode node) {
        if (node == null || node.isNull()) {
            return null;
        }
        if (node.isTextual()) {
            return node.textValue();
        }
        if (node.isBoolean()) {
            return node.booleanValue();
        }
        if (node.isIntegralNumber()) {
            return node.longValue();
        }
        if (node.isFloatingPointNumber()) {
            return node.doubleValue();
        }
        if (node.isArray()) {
            List<Object> items = new ArrayList<Object>(node.size());
            for (JsonNode item : node) {
                items.add(toGenericValue(item));
            }
            return items;
        }
        if (node.isObject()) {
            JsonNode typeNode = node.get("@type");
            if (typeNode != null && typeNode.isTextual()) {
                GenericObject object = new GenericObject(typeNode.asText());
                Iterator<Map.Entry<String, JsonNode>> fields = node.fields();
                while (fields.hasNext()) {
                    Map.Entry<String, JsonNode> field = fields.next();
                    if (!"@type".equals(field.getKey())) {
                        object.putField(field.getKey(), toGenericValue(field.getValue()));
                    }
                }
                return object;
            }
            Map<String, Object> mapped = new LinkedHashMap<String, Object>();
            Iterator<Map.Entry<String, JsonNode>> fields = node.fields();
            while (fields.hasNext()) {
                Map.Entry<String, JsonNode> field = fields.next();
                mapped.put(field.getKey(), toGenericValue(field.getValue()));
            }
            return mapped;
        }
        return mapper.convertValue(node, Object.class);
    }

    static Class<?> resolveType(String typeName) {
        try {
            switch (typeName) {
                case "boolean":
                    return boolean.class;
                case "byte":
                    return byte.class;
                case "short":
                    return short.class;
                case "int":
                    return int.class;
                case "long":
                    return long.class;
                case "float":
                    return float.class;
                case "double":
                    return double.class;
                case "char":
                    return char.class;
                default:
                    if (typeName.endsWith("[]")) {
                        Class<?> componentType = resolveType(typeName.substring(0, typeName.length() - 2));
                        return Array.newInstance(componentType, 0).getClass();
                    }
                    return Class.forName(typeName);
            }
        } catch (ClassNotFoundException ex) {
            throw new IllegalArgumentException("unable to resolve type " + typeName, ex);
        }
    }

    private JavaType resolveSchemaType(String typeName) {
        String normalized = normalizeSchemaType(typeName);
        int dimensions = 0;
        while (normalized.endsWith("[]")) {
            dimensions++;
            normalized = normalized.substring(0, normalized.length() - 2).trim();
        }
        JavaType resolved = resolveBaseSchemaType(normalized);
        for (int i = 0; i < dimensions; i++) {
            resolved = typeFactory.constructArrayType(resolved);
        }
        return resolved;
    }

    private JavaType resolveBaseSchemaType(String typeName) {
        int genericStart = typeName.indexOf('<');
        if (genericStart < 0) {
            return typeFactory.constructType(resolveRawSchemaType(typeName));
        }
        String rawTypeName = typeName.substring(0, genericStart).trim();
        String argsPart = typeName.substring(genericStart + 1, typeName.lastIndexOf('>'));
        List<String> argNames = splitGenericArguments(argsPart);
        JavaType[] args = new JavaType[argNames.size()];
        for (int i = 0; i < argNames.size(); i++) {
            args[i] = resolveSchemaType(argNames.get(i));
        }
        return typeFactory.constructParametricType(resolveRawSchemaType(rawTypeName), args);
    }

    private Class<?> resolveRawSchemaType(String typeName) {
        String normalized = normalizeSchemaType(typeName);
        if ("?".equals(normalized) || normalized.isEmpty()) {
            return Object.class;
        }
        if (normalized.startsWith("? extends ")) {
            return resolveRawSchemaType(normalized.substring("? extends ".length()));
        }
        if (normalized.startsWith("? super ")) {
            return resolveRawSchemaType(normalized.substring("? super ".length()));
        }
        try {
            return resolveType(normalized);
        } catch (IllegalArgumentException ex) {
            if (normalized.indexOf('.') < 0) {
                return Object.class;
            }
            throw ex;
        }
    }

    private String normalizeSchemaType(String typeName) {
        return typeName == null ? "" : typeName.trim();
    }

    private List<String> splitGenericArguments(String raw) {
        List<String> args = new ArrayList<String>();
        StringBuilder current = new StringBuilder();
        int depth = 0;
        for (int i = 0; i < raw.length(); i++) {
            char ch = raw.charAt(i);
            if (ch == '<') {
                depth++;
                current.append(ch);
                continue;
            }
            if (ch == '>') {
                depth--;
                current.append(ch);
                continue;
            }
            if (ch == ',' && depth == 0) {
                args.add(current.toString().trim());
                current.setLength(0);
                continue;
            }
            current.append(ch);
        }
        if (current.length() > 0) {
            args.add(current.toString().trim());
        }
        return args;
    }
}
