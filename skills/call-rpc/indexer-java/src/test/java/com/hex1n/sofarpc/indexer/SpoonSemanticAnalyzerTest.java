package com.hex1n.sofarpc.indexer;

import org.junit.jupiter.api.Test;

import java.net.URISyntaxException;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.Arrays;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.junit.jupiter.api.Assertions.fail;

public class SpoonSemanticAnalyzerTest {
    @Test
    void extractsFacadeSignaturesAndDtoMetadata() throws Exception {
        Path fixtureRoot = fixturePath("fixture-project");
        Path sourceRoot = fixtureRoot.resolve("src/main/java");

        SpoonSemanticAnalyzer analyzer = new SpoonSemanticAnalyzer();
        SpoonSemanticAnalyzer.SemanticIndex index = analyzer.analyze(
            fixtureRoot,
            Arrays.asList(sourceRoot),
            Arrays.asList("必传", "required")
        );

        SpoonSemanticAnalyzer.SemanticClassInfo facade = find(index, "com.example.UserFacade");
        assertEquals("interface", facade.kind);
        assertEquals("src/main/java/com/example/UserFacade.java", facade.file);
        assertEquals(1, facade.methods.size());
        assertEquals("com.example.UserRequest", facade.methods.get(0).parameters.get(0).type);
        assertEquals("com.example.ResponseEnvelope", facade.methods.get(0).returnType);

        SpoonSemanticAnalyzer.SemanticClassInfo request = find(index, "com.example.UserRequest");
        assertEquals("class", request.kind);
        assertEquals("com.example.BaseRequest", request.superclass);
        assertTrue(request.fields.size() >= 2);
        assertTrue(hasField(request, "items", "java.util.List<com.example.Item>"));
        assertTrue(hasField(request, "status", "com.example.Status"));

        SpoonSemanticAnalyzer.SemanticClassInfo base = find(index, "com.example.BaseRequest");
        assertTrue(hasRequiredField(base, "tenantId"));

        SpoonSemanticAnalyzer.SemanticClassInfo response = find(index, "com.example.ResponseEnvelope");
        assertTrue(response.method_returns.contains("java.util.Optional<java.lang.String>"));

        SpoonSemanticAnalyzer.SemanticClassInfo status = find(index, "com.example.Status");
        assertEquals("enum", status.kind);
        assertEquals(Arrays.asList("ACTIVE", "INACTIVE"), status.enum_constants);
    }

    private SpoonSemanticAnalyzer.SemanticClassInfo find(SpoonSemanticAnalyzer.SemanticIndex index, String fqn) {
        for (SpoonSemanticAnalyzer.SemanticClassInfo info : index.classes) {
            if (fqn.equals(info.fqn)) {
                return info;
            }
        }
        fail("missing class " + fqn);
        return null;
    }

    private boolean hasField(SpoonSemanticAnalyzer.SemanticClassInfo info, String name, String type) {
        for (SpoonSemanticAnalyzer.SemanticFieldInfo field : info.fields) {
            if (name.equals(field.name) && type.equals(field.java_type)) {
                return true;
            }
        }
        return false;
    }

    private boolean hasRequiredField(SpoonSemanticAnalyzer.SemanticClassInfo info, String name) {
        for (SpoonSemanticAnalyzer.SemanticFieldInfo field : info.fields) {
            if (name.equals(field.name) && field.required) {
                return true;
            }
        }
        return false;
    }

    private Path fixturePath(String name) throws URISyntaxException {
        Path path = Paths.get(SpoonSemanticAnalyzerTest.class.getResource("/" + name).toURI());
        assertNotNull(path);
        return path;
    }
}
