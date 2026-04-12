package com.hex1n.sofarpcctl;

import org.junit.Assert;
import org.junit.Test;

import java.net.URL;
import java.util.Arrays;
import java.util.Collections;
import java.util.Map;

public class StubMetadataImporterTest {

    @Test
    public void importServicesKeepsMethodOverloads() {
        URL codeSource = TestOverloadedStub.class.getProtectionDomain().getCodeSource().getLocation();
        String serviceClassName = TestOverloadedStub.class.getName();
        StubMetadataImporter.ImportResult result = new StubMetadataImporter().importServices(
            Collections.singletonList(codeSource.getPath()),
            Collections.singletonList(serviceClassName),
            Collections.singletonMap(serviceClassName, "user-service")
        );

        MetadataCatalog.ServiceMetadata service = result.getServices().get(serviceClassName);
        MetadataCatalog.MethodMetadata find = service.getMethods().get("find");
        MetadataCatalog.MethodMetadata deleteUser = service.getMethods().get("deleteUser");

        Assert.assertNotNull(service);
        Assert.assertEquals("user-service", service.getUniqueId());
        Assert.assertEquals(2, result.getImportedOverloadCount());
        Assert.assertNotNull(find);
        Assert.assertEquals("read", find.getRisk());
        Assert.assertTrue(find.hasOverloads());
        Assert.assertEquals(2, find.getOverloads().size());
        Assert.assertEquals(Arrays.asList("java.lang.Long"), find.getOverloads().get(0).getParamTypes());
        Assert.assertEquals(Arrays.asList("java.lang.String"), find.getOverloads().get(1).getParamTypes());
        Assert.assertNotNull(deleteUser);
        Assert.assertEquals("dangerous", deleteUser.getRisk());
        Assert.assertEquals(Collections.singletonList("java.lang.Long"), deleteUser.getParamTypes());
    }
}

interface TestOverloadedStub {
    String find(Long id);
    String find(String name);
    void deleteUser(Long id);
}
