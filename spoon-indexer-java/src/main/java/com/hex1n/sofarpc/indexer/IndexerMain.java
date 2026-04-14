package com.hex1n.sofarpc.indexer;

import com.fasterxml.jackson.databind.ObjectMapper;

import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;

public final class IndexerMain {
    private final ObjectMapper mapper = new ObjectMapper();

    public static void main(String[] args) throws Exception {
        new IndexerMain().run(args);
    }

    private void run(String[] args) throws Exception {
        Path projectRoot = null;
        List<Path> sourceRoots = new ArrayList<Path>();
        List<String> requiredMarkers = new ArrayList<String>();
        for (int i = 0; i < args.length; i++) {
            String arg = args[i];
            if ("--project-root".equals(arg)) {
                projectRoot = Paths.get(requireValue(args, ++i, "--project-root"));
                continue;
            }
            if ("--source-root".equals(arg)) {
                sourceRoots.add(Paths.get(requireValue(args, ++i, "--source-root")));
                continue;
            }
            if ("--required-marker".equals(arg)) {
                requiredMarkers.add(requireValue(args, ++i, "--required-marker"));
                continue;
            }
            throw new IllegalArgumentException("unsupported argument: " + arg);
        }

        if (projectRoot == null) {
            throw new IllegalArgumentException("--project-root is required");
        }
        if (sourceRoots.isEmpty()) {
            throw new IllegalArgumentException("at least one --source-root is required");
        }

        SpoonSemanticAnalyzer analyzer = new SpoonSemanticAnalyzer();
        SpoonSemanticAnalyzer.SemanticIndex index = analyzer.analyze(projectRoot, sourceRoots, requiredMarkers);
        System.out.println(mapper.writeValueAsString(index));
    }

    private String requireValue(String[] args, int index, String flag) {
        if (index >= args.length) {
            throw new IllegalArgumentException(flag + " requires a value");
        }
        return args[index];
    }
}
