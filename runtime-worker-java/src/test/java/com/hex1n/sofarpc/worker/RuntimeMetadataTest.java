package com.hex1n.sofarpc.worker;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;

public class RuntimeMetadataTest {
    @Test
    void readsRuntimeVersionFromFilteredBuildMetadata() {
        String expected = System.getProperty("sofarpc.expectedRuntimeVersion");
        assertNotNull(expected);
        assertEquals(expected, RuntimeMetadata.runtimeVersion());
    }
}
