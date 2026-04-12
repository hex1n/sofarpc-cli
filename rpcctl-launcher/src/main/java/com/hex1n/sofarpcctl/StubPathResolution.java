package com.hex1n.sofarpcctl;

import java.util.List;

final class StubPathResolution {

    private final List<String> paths;
    private final String source;

    StubPathResolution(List<String> paths, String source) {
        this.paths = paths;
        this.source = source;
    }

    List<String> getPaths() {
        return paths;
    }

    String getSource() {
        return source;
    }
}
