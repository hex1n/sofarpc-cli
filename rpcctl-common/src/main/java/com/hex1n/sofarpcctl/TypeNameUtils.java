package com.hex1n.sofarpcctl;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

public final class TypeNameUtils {

    private static final Map<String, String> PRIMITIVE_DESCRIPTORS;
    private static final Map<String, Class<?>> PRIMITIVE_TYPES;
    private static final Map<String, String> SIGNATURE_ALIASES;

    static {
        Map<String, String> primitiveDescriptors = new HashMap<String, String>();
        primitiveDescriptors.put("boolean", "Z");
        primitiveDescriptors.put("byte", "B");
        primitiveDescriptors.put("char", "C");
        primitiveDescriptors.put("short", "S");
        primitiveDescriptors.put("int", "I");
        primitiveDescriptors.put("long", "J");
        primitiveDescriptors.put("float", "F");
        primitiveDescriptors.put("double", "D");
        PRIMITIVE_DESCRIPTORS = Collections.unmodifiableMap(primitiveDescriptors);

        Map<String, Class<?>> primitiveTypes = new HashMap<String, Class<?>>();
        primitiveTypes.put("boolean", boolean.class);
        primitiveTypes.put("byte", byte.class);
        primitiveTypes.put("char", char.class);
        primitiveTypes.put("short", short.class);
        primitiveTypes.put("int", int.class);
        primitiveTypes.put("long", long.class);
        primitiveTypes.put("float", float.class);
        primitiveTypes.put("double", double.class);
        PRIMITIVE_TYPES = Collections.unmodifiableMap(primitiveTypes);

        Map<String, String> signatureAliases = new HashMap<String, String>();
        signatureAliases.put("java.util.ArrayList", "java.util.List");
        signatureAliases.put("java.util.LinkedList", "java.util.List");
        signatureAliases.put("java.util.HashSet", "java.util.Set");
        signatureAliases.put("java.util.LinkedHashSet", "java.util.Set");
        signatureAliases.put("java.util.TreeSet", "java.util.Set");
        signatureAliases.put("java.util.HashMap", "java.util.Map");
        signatureAliases.put("java.util.LinkedHashMap", "java.util.Map");
        signatureAliases.put("java.util.TreeMap", "java.util.Map");
        SIGNATURE_ALIASES = Collections.unmodifiableMap(signatureAliases);
    }

    private TypeNameUtils() {
    }

    public static List<String> parseTypes(String raw) {
        if (raw == null || raw.trim().isEmpty()) {
            return Collections.emptyList();
        }
        String[] chunks = raw.split(",");
        List<String> parsed = new ArrayList<String>(chunks.length);
        for (String chunk : chunks) {
            String candidate = chunk.trim();
            if (!candidate.isEmpty()) {
                parsed.add(candidate);
            }
        }
        return parsed;
    }

    public static String normalizeForRpc(String rawType) {
        if (rawType == null) {
            return null;
        }
        String cleaned = stripGenerics(rawType.trim());
        if (cleaned.isEmpty()) {
            return null;
        }
        if (cleaned.endsWith("...")) {
            cleaned = cleaned.substring(0, cleaned.length() - 3) + "[]";
        }
        if (cleaned.startsWith("[")) {
            return cleaned;
        }

        int dimensions = 0;
        while (cleaned.endsWith("[]")) {
            cleaned = cleaned.substring(0, cleaned.length() - 2).trim();
            dimensions++;
        }
        if (dimensions == 0) {
            return cleaned;
        }

        StringBuilder builder = new StringBuilder(dimensions + cleaned.length() + 2);
        for (int i = 0; i < dimensions; i++) {
            builder.append('[');
        }
        String primitiveDescriptor = PRIMITIVE_DESCRIPTORS.get(cleaned);
        if (primitiveDescriptor != null) {
            builder.append(primitiveDescriptor);
        } else {
            builder.append('L').append(cleaned).append(';');
        }
        return builder.toString();
    }

    public static String normalizeForSignature(String rawType) {
        String normalized = normalizeForRpc(rawType);
        if (normalized == null) {
            return null;
        }
        if (normalized.startsWith("[")) {
            return normalized;
        }
        String alias = SIGNATURE_ALIASES.get(normalized);
        return alias == null ? normalized : alias;
    }

    public static List<String> normalizeParamTypes(List<String> rawTypes) {
        if (rawTypes == null || rawTypes.isEmpty()) {
            return Collections.emptyList();
        }
        List<String> normalized = new ArrayList<String>(rawTypes.size());
        for (String rawType : rawTypes) {
            normalized.add(normalizeForSignature(rawType));
        }
        return normalized;
    }

    public static String componentType(String rawType) {
        String normalized = normalizeForRpc(rawType);
        if (normalized == null || !normalized.startsWith("[")) {
            return normalized;
        }
        String remainder = normalized.substring(1);
        if (remainder.length() == 1) {
            return primitiveNameFromDescriptor(remainder.charAt(0));
        }
        if (remainder.startsWith("L") && remainder.endsWith(";")) {
            return remainder.substring(1, remainder.length() - 1);
        }
        return remainder;
    }

    public static boolean isArrayType(String rawType) {
        String normalized = normalizeForRpc(rawType);
        return normalized != null && normalized.startsWith("[");
    }

    public static boolean isCollectionType(String rawType) {
        String normalized = normalizeForSignature(rawType);
        return "java.util.Collection".equals(normalized)
            || "java.util.List".equals(normalized)
            || "java.util.ArrayList".equals(normalized)
            || "java.util.LinkedList".equals(normalized)
            || "java.util.Set".equals(normalized)
            || "java.util.HashSet".equals(normalized)
            || "java.util.LinkedHashSet".equals(normalized)
            || "java.lang.Iterable".equals(normalized);
    }

    public static boolean isMapType(String rawType) {
        String normalized = normalizeForSignature(rawType);
        return "java.util.Map".equals(normalized)
            || "java.util.HashMap".equals(normalized)
            || "java.util.LinkedHashMap".equals(normalized)
            || "java.util.TreeMap".equals(normalized);
    }

    public static boolean isRawFriendlyType(String rawType) {
        return isSimpleScalar(rawType)
            || isMapType(rawType)
            || isCollectionType(rawType)
            || isArrayType(rawType);
    }

    public static boolean isSimpleScalar(String rawType) {
        String normalized = normalizeForRpc(rawType);
        return normalized != null
            && (PRIMITIVE_TYPES.containsKey(normalized)
            || "java.lang.Boolean".equals(normalized)
            || "java.lang.Byte".equals(normalized)
            || "java.lang.Character".equals(normalized)
            || "java.lang.Short".equals(normalized)
            || "java.lang.Integer".equals(normalized)
            || "java.lang.Long".equals(normalized)
            || "java.lang.Float".equals(normalized)
            || "java.lang.Double".equals(normalized)
            || "java.lang.String".equals(normalized)
            || "java.math.BigDecimal".equals(normalized)
            || "java.math.BigInteger".equals(normalized)
            || "java.util.Date".equals(normalized)
            || "java.util.UUID".equals(normalized));
    }

    public static boolean isLocallyResolvable(String rawType) {
        String normalized = normalizeForRpc(rawType);
        if (normalized == null) {
            return true;
        }
        if (PRIMITIVE_TYPES.containsKey(normalized)) {
            return true;
        }
        try {
            Class.forName(normalized);
            return true;
        } catch (ClassNotFoundException exception) {
            return false;
        }
    }

    public static Class<?> classFor(String rawType) throws ClassNotFoundException {
        String normalized = normalizeForRpc(rawType);
        if (normalized == null) {
            return Object.class;
        }
        Class<?> primitiveClass = PRIMITIVE_TYPES.get(normalized);
        if (primitiveClass != null) {
            return primitiveClass;
        }
        return Class.forName(normalized);
    }

    private static String stripGenerics(String rawType) {
        StringBuilder builder = new StringBuilder(rawType.length());
        int depth = 0;
        for (int index = 0; index < rawType.length(); index++) {
            char current = rawType.charAt(index);
            if (current == '<') {
                depth++;
                continue;
            }
            if (current == '>') {
                depth--;
                continue;
            }
            if (depth == 0) {
                builder.append(current);
            }
        }
        return builder.toString().replace(" ", "");
    }

    private static String primitiveNameFromDescriptor(char descriptor) {
        switch (descriptor) {
            case 'Z':
                return "boolean";
            case 'B':
                return "byte";
            case 'C':
                return "char";
            case 'S':
                return "short";
            case 'I':
                return "int";
            case 'J':
                return "long";
            case 'F':
                return "float";
            case 'D':
                return "double";
            default:
                return String.valueOf(descriptor);
        }
    }
}
