package com.hex1n.sofarpcctl;

import org.junit.Assert;
import org.junit.Test;

public class StringValueResolverTest {

    @Test
    public void returnsFirstTrimmedNonBlankValue() {
        Assert.assertEquals(
            "runtime",
            StringValueResolver.firstNonBlank("  ", "", "runtime", "fallback")
        );
        Assert.assertEquals(
            "fallback",
            StringValueResolver.firstNonBlank(null, "  ", "\n\t", "fallback")
        );
        Assert.assertNull(StringValueResolver.firstNonBlank(null, " ", ""));
    }
}

