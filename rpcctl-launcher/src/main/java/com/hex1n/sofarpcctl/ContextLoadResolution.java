package com.hex1n.sofarpcctl;

final class ContextLoadResolution {

    private final LoadedContext loadedContext;
    private final String source;

    ContextLoadResolution(LoadedContext loadedContext, String source) {
        this.loadedContext = loadedContext;
        this.source = source;
    }

    LoadedContext getLoadedContext() {
        return loadedContext;
    }

    String getSource() {
        return source;
    }
}
