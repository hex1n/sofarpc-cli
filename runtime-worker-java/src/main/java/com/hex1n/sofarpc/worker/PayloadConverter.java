package com.hex1n.sofarpc.worker;

import com.alipay.hessian.generic.model.GenericObject;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import java.lang.reflect.Array;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

public final class PayloadConverter {
    private final ObjectMapper mapper;

    public PayloadConverter(ObjectMapper mapper) {
        this.mapper = mapper;
    }

    public Object[] convertArguments(PayloadMode mode, List<String> paramTypes, JsonNode args) {
        if (args == null || !args.isArray()) {
            throw new IllegalArgumentException("args must be a JSON array");
        }
        Object[] converted = new Object[args.size()];
        for (int i = 0; i < args.size(); i++) {
            String typeName = i < paramTypes.size() ? paramTypes.get(i) : null;
            converted[i] = convertArgument(mode, typeName, args.get(i));
        }
        return converted;
    }

    private Object convertArgument(PayloadMode mode, String typeName, JsonNode node) {
        switch (mode) {
            case RAW:
                return mapper.convertValue(node, Object.class);
            case GENERIC:
                return toGenericValue(node);
            case SCHEMA:
                if (typeName == null || typeName.trim().isEmpty()) {
                    return mapper.convertValue(node, Object.class);
                }
                return mapper.convertValue(node, resolveType(typeName));
            default:
                throw new IllegalArgumentException("unsupported payload mode: " + mode);
        }
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
}
