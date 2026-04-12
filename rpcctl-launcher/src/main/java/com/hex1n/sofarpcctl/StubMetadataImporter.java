package com.hex1n.sofarpcctl;

import java.io.File;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.net.URL;
import java.net.URLClassLoader;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

public final class StubMetadataImporter {

    public StubMetadataImporter() {
    }

    public ImportResult importServices(
        List<String> stubPaths,
        List<String> serviceClassNames,
        Map<String, String> uniqueIds
    ) {
        if (serviceClassNames == null || serviceClassNames.isEmpty()) {
            return new ImportResult();
        }

        URL[] urls = new URL[stubPaths == null ? 0 : stubPaths.size()];
        try {
            for (int index = 0; index < urls.length; index++) {
                urls[index] = new File(stubPaths.get(index)).toURI().toURL();
            }
        } catch (Exception exception) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Failed to resolve stub classpath entries.",
                exception
            );
        }

        URLClassLoader classLoader = new URLClassLoader(urls, StubMetadataImporter.class.getClassLoader());
        ImportResult result = new ImportResult();
        try {
            for (String serviceClassName : serviceClassNames) {
                Class<?> serviceClass = Class.forName(serviceClassName, false, classLoader);
                MetadataCatalog.ServiceMetadata serviceMetadata = new MetadataCatalog.ServiceMetadata();
                serviceMetadata.setDescription("Imported from stub class " + serviceClassName);
                serviceMetadata.setUniqueId(uniqueIds.get(serviceClassName));
                serviceMetadata.setMethods(importMethods(serviceClass, result));
                result.getServices().put(serviceClassName, serviceMetadata);
            }
            return result;
        } catch (CliException exception) {
            throw exception;
        } catch (Exception exception) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Failed to import metadata from stub classes.",
                exception
            );
        } finally {
            try {
                classLoader.close();
            } catch (Exception ignored) {
            }
        }
    }

    private Map<String, MetadataCatalog.MethodMetadata> importMethods(
        Class<?> serviceClass,
        ImportResult result
    ) {
        Map<String, List<Method>> methodsByName = new LinkedHashMap<String, List<Method>>();
        for (Method method : serviceClass.getMethods()) {
            if (!isRpcMethod(method)) {
                continue;
            }
            String methodName = method.getName();
            List<Method> methods = methodsByName.get(methodName);
            if (methods == null) {
                methods = new ArrayList<Method>();
                methodsByName.put(methodName, methods);
            }
            if (!containsSignature(methods, method)) {
                methods.add(method);
            }
        }

        List<String> sortedMethodNames = new ArrayList<String>(methodsByName.keySet());
        Collections.sort(sortedMethodNames);

        Map<String, MetadataCatalog.MethodMetadata> imported = new LinkedHashMap<String, MetadataCatalog.MethodMetadata>();
        for (String methodName : sortedMethodNames) {
            List<Method> methods = methodsByName.get(methodName);
            if (methods.size() > 1) {
                result.getSkippedOverloads().add(serviceClass.getName() + "#" + methodName);
                continue;
            }
            Method method = methods.get(0);
            MetadataCatalog.MethodMetadata methodMetadata = new MetadataCatalog.MethodMetadata();
            methodMetadata.setDescription(buildSignature(method));
            methodMetadata.setRisk(inferRisk(method.getName()));
            methodMetadata.setParamTypes(parameterTypeNames(method));
            imported.put(method.getName(), methodMetadata);
        }
        return imported;
    }

    private boolean isRpcMethod(Method method) {
        return method != null
            && Modifier.isPublic(method.getModifiers())
            && !Modifier.isStatic(method.getModifiers())
            && !method.isSynthetic()
            && method.getDeclaringClass() != Object.class;
    }

    private boolean containsSignature(List<Method> methods, Method candidate) {
        String signature = buildSignature(candidate);
        for (Method method : methods) {
            if (signature.equals(buildSignature(method))) {
                return true;
            }
        }
        return false;
    }

    private String buildSignature(Method method) {
        StringBuilder builder = new StringBuilder();
        builder.append(typeName(method.getReturnType()))
            .append(' ')
            .append(method.getName())
            .append('(');
        Class<?>[] parameterTypes = method.getParameterTypes();
        for (int index = 0; index < parameterTypes.length; index++) {
            if (index > 0) {
                builder.append(", ");
            }
            builder.append(typeName(parameterTypes[index]));
        }
        builder.append(')');
        return builder.toString();
    }

    private List<String> parameterTypeNames(Method method) {
        Class<?>[] parameterTypes = method.getParameterTypes();
        List<String> names = new ArrayList<String>(parameterTypes.length);
        for (Class<?> parameterType : parameterTypes) {
            names.add(typeName(parameterType));
        }
        return names;
    }

    private String typeName(Class<?> type) {
        if (type.isArray()) {
            return typeName(type.getComponentType()) + "[]";
        }
        return type.getName();
    }

    private String inferRisk(String methodName) {
        String normalized = methodName == null ? "" : methodName.trim().toLowerCase();
        if (normalized.startsWith("delete")
            || normalized.startsWith("remove")
            || normalized.startsWith("destroy")
            || normalized.startsWith("purge")) {
            return "dangerous";
        }
        if (normalized.startsWith("get")
            || normalized.startsWith("find")
            || normalized.startsWith("list")
            || normalized.startsWith("query")
            || normalized.startsWith("search")
            || normalized.startsWith("describe")
            || normalized.startsWith("detail")
            || normalized.startsWith("fetch")
            || normalized.startsWith("load")
            || normalized.startsWith("check")
            || normalized.startsWith("count")
            || normalized.startsWith("is")
            || normalized.startsWith("has")) {
            return "read";
        }
        return "write";
    }

    public static final class ImportResult {
        private final Map<String, MetadataCatalog.ServiceMetadata> services =
            new LinkedHashMap<String, MetadataCatalog.ServiceMetadata>();
        private final List<String> skippedOverloads = new ArrayList<String>();

        public Map<String, MetadataCatalog.ServiceMetadata> getServices() {
            return services;
        }

        public List<String> getSkippedOverloads() {
            return skippedOverloads;
        }
    }
}
