package com.hex1n.sofarpc.indexer;

import spoon.Launcher;
import spoon.reflect.CtModel;
import spoon.reflect.code.CtComment;
import spoon.reflect.declaration.CtAnnotation;
import spoon.reflect.declaration.CtClass;
import spoon.reflect.declaration.CtElement;
import spoon.reflect.declaration.CtEnum;
import spoon.reflect.declaration.CtField;
import spoon.reflect.declaration.CtInterface;
import spoon.reflect.declaration.CtMethod;
import spoon.reflect.declaration.CtParameter;
import spoon.reflect.declaration.CtType;
import spoon.reflect.reference.CtArrayTypeReference;
import spoon.reflect.reference.CtTypeReference;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

final class SpoonSemanticAnalyzer {
    private static final Set<String> REQUIRED_ANNOTATIONS = new LinkedHashSet<String>(
        Arrays.asList("NotNull", "NonNull", "NotEmpty", "NotBlank")
    );

    SemanticIndex analyze(Path projectRoot, List<Path> sourceRoots, List<String> requiredMarkers) {
        Launcher launcher = new Launcher();
        launcher.getEnvironment().setNoClasspath(true);
        launcher.getEnvironment().setCommentEnabled(true);
        launcher.getEnvironment().setComplianceLevel(8);
        for (Path sourceRoot : sourceRoots) {
            launcher.addInputResource(sourceRoot.toString());
        }
        launcher.buildModel();

        CtModel model = launcher.getModel();
        List<CtType<?>> sortedTypes = new ArrayList<CtType<?>>(model.getAllTypes());
        Collections.sort(sortedTypes, new Comparator<CtType<?>>() {
            @Override
            public int compare(CtType<?> left, CtType<?> right) {
                return safeQualifiedName(left).compareTo(safeQualifiedName(right));
            }
        });

        SemanticIndex index = new SemanticIndex();
        for (CtType<?> type : sortedTypes) {
            SemanticClassInfo info = extractType(projectRoot, type, requiredMarkers);
            if (info != null) {
                index.classes.add(info);
            }
        }
        return index;
    }

    private SemanticClassInfo extractType(Path projectRoot, CtType<?> type, List<String> requiredMarkers) {
        if (!(type instanceof CtClass) && !(type instanceof CtInterface) && !(type instanceof CtEnum)) {
            return null;
        }
        String qualifiedName = safeQualifiedName(type);
        if (qualifiedName.isEmpty()) {
            return null;
        }

        SemanticClassInfo info = new SemanticClassInfo();
        info.fqn = qualifiedName;
        info.simple_name = type.getSimpleName();
        info.file = relativePath(projectRoot, type);
        info.same_pkg_prefix = type.getPackage() != null ? type.getPackage().getQualifiedName() : "";
        info.imports = new LinkedHashMap<String, String>();

        if (type instanceof CtInterface) {
            info.kind = "interface";
            for (CtMethod<?> method : declaredMethods(type)) {
                SemanticMethodInfo methodInfo = new SemanticMethodInfo();
                methodInfo.name = method.getSimpleName();
                methodInfo.javadoc = docComment(method);
                methodInfo.returnType = renderType(method.getType());
                for (CtParameter<?> parameter : method.getParameters()) {
                    SemanticParameterInfo parameterInfo = new SemanticParameterInfo();
                    parameterInfo.name = parameter.getSimpleName();
                    parameterInfo.type = renderType(parameter.getType());
                    methodInfo.parameters.add(parameterInfo);
                }
                info.methods.add(methodInfo);
            }
            return info;
        }

        if (type instanceof CtEnum) {
            info.kind = "enum";
            CtEnum<?> enumType = (CtEnum<?>) type;
            for (spoon.reflect.declaration.CtEnumValue<?> value : enumType.getEnumValues()) {
                info.enum_constants.add(value.getSimpleName());
            }
            return info;
        }

        CtClass<?> clazz = (CtClass<?>) type;
        info.kind = "class";
        if (clazz.getSuperclass() != null) {
            info.superclass = renderType(clazz.getSuperclass());
        }

        for (CtField<?> field : clazz.getFields()) {
            if (field.isImplicit() || field.isStatic() || field.hasModifier(spoon.reflect.declaration.ModifierKind.TRANSIENT)) {
                continue;
            }
            if ("serialVersionUID".equals(field.getSimpleName())) {
                continue;
            }
            SemanticFieldInfo fieldInfo = new SemanticFieldInfo();
            fieldInfo.name = field.getSimpleName();
            fieldInfo.java_type = renderType(field.getType());
            fieldInfo.comment = docComment(field);
            fieldInfo.required = isRequired(field, fieldInfo.comment, requiredMarkers);
            info.fields.add(fieldInfo);
        }

        for (CtMethod<?> method : declaredMethods(clazz)) {
            if (method.isStatic() || method.hasModifier(spoon.reflect.declaration.ModifierKind.PRIVATE)) {
                continue;
            }
            if (method.getType() == null) {
                continue;
            }
            info.method_returns.add(renderType(method.getType()));
        }
        return info;
    }

    private List<CtMethod<?>> declaredMethods(CtType<?> type) {
        List<CtMethod<?>> methods = new ArrayList<CtMethod<?>>();
        for (CtMethod<?> method : type.getMethods()) {
            if (method.getDeclaringType() == type) {
                methods.add(method);
            }
        }
        Collections.sort(methods, new Comparator<CtMethod<?>>() {
            @Override
            public int compare(CtMethod<?> left, CtMethod<?> right) {
                int byName = left.getSimpleName().compareTo(right.getSimpleName());
                if (byName != 0) {
                    return byName;
                }
                return Integer.compare(left.getParameters().size(), right.getParameters().size());
            }
        });
        return methods;
    }

    private String relativePath(Path projectRoot, CtType<?> type) {
        if (type.getPosition() == null || !type.getPosition().isValidPosition() || type.getPosition().getFile() == null) {
            return "";
        }
        Path file = type.getPosition().getFile().toPath().toAbsolutePath().normalize();
        try {
            return projectRoot.toAbsolutePath().normalize().relativize(file).toString().replace('\\', '/');
        } catch (IllegalArgumentException ignored) {
            return file.toString().replace('\\', '/');
        }
    }

    private String docComment(CtElement element) {
        if (element == null) {
            return "";
        }
        String doc = element.getDocComment();
        if (doc != null && !doc.trim().isEmpty()) {
            return doc.trim();
        }
        for (CtComment comment : element.getComments()) {
            if (comment.getCommentType() == CtComment.CommentType.JAVADOC) {
                return comment.getContent().trim();
            }
        }
        return "";
    }

    private boolean isRequired(CtField<?> field, String comment, List<String> requiredMarkers) {
        if (containsRequiredMarker(comment, requiredMarkers)) {
            return true;
        }
        for (CtAnnotation<?> annotation : field.getAnnotations()) {
            CtTypeReference<?> ref = annotation.getAnnotationType();
            if (ref != null && REQUIRED_ANNOTATIONS.contains(ref.getSimpleName())) {
                return true;
            }
        }
        return false;
    }

    private boolean containsRequiredMarker(String text, List<String> requiredMarkers) {
        if (text == null || text.isEmpty()) {
            return false;
        }
        String lower = text.toLowerCase();
        for (String marker : requiredMarkers) {
            if (marker != null && !marker.trim().isEmpty() && lower.contains(marker.toLowerCase())) {
                return true;
            }
        }
        return false;
    }

    private String renderType(CtTypeReference<?> type) {
        if (type == null) {
            return "void";
        }
        if (type instanceof CtArrayTypeReference) {
            CtArrayTypeReference<?> arrayType = (CtArrayTypeReference<?>) type;
            return renderType(arrayType.getComponentType()) + "[]";
        }
        String base = qualifiedName(type);
        List<CtTypeReference<?>> actualTypeArguments = type.getActualTypeArguments();
        if (actualTypeArguments == null || actualTypeArguments.isEmpty()) {
            return base;
        }
        List<String> rendered = new ArrayList<String>(actualTypeArguments.size());
        for (CtTypeReference<?> actualTypeArgument : actualTypeArguments) {
            if (actualTypeArgument == null) {
                rendered.add("?");
            } else {
                rendered.add(renderType(actualTypeArgument));
            }
        }
        return base + "<" + join(rendered) + ">";
    }

    private String qualifiedName(CtTypeReference<?> type) {
        String qualifiedName = type.getQualifiedName();
        if (qualifiedName != null && !qualifiedName.trim().isEmpty() && !"<nulltype>".equals(qualifiedName)) {
            return qualifiedName.replace('$', '.');
        }
        String simpleName = type.getSimpleName();
        return simpleName == null ? "Object" : simpleName;
    }

    private String join(List<String> values) {
        StringBuilder builder = new StringBuilder();
        for (int i = 0; i < values.size(); i++) {
            if (i > 0) {
                builder.append(", ");
            }
            builder.append(values.get(i));
        }
        return builder.toString();
    }

    private String safeQualifiedName(CtType<?> type) {
        String qualifiedName = type.getQualifiedName();
        return qualifiedName == null ? "" : qualifiedName;
    }

    static final class SemanticIndex {
        public List<SemanticClassInfo> classes = new ArrayList<SemanticClassInfo>();
    }

    static final class SemanticClassInfo {
        public String fqn;
        public String simple_name;
        public String file;
        public String kind;
        public String superclass;
        public Map<String, String> imports = new LinkedHashMap<String, String>();
        public String same_pkg_prefix = "";
        public List<SemanticFieldInfo> fields = new ArrayList<SemanticFieldInfo>();
        public List<String> enum_constants = new ArrayList<String>();
        public List<SemanticMethodInfo> methods = new ArrayList<SemanticMethodInfo>();
        public List<String> method_returns = new ArrayList<String>();
    }

    static final class SemanticFieldInfo {
        public String name;
        public String java_type;
        public String comment = "";
        public boolean required;
    }

    static final class SemanticMethodInfo {
        public String name;
        public String javadoc = "";
        public String returnType = "void";
        public List<SemanticParameterInfo> parameters = new ArrayList<SemanticParameterInfo>();
    }

    static final class SemanticParameterInfo {
        public String name;
        public String type;
    }
}
