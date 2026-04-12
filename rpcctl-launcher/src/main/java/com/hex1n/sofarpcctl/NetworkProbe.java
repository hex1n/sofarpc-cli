package com.hex1n.sofarpcctl;

import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

public final class NetworkProbe {

    public ProbeSummary probe(String rawTarget, String defaultScheme, int timeoutMs) {
        if (rawTarget == null || rawTarget.trim().isEmpty()) {
            throw new CliException(ExitCodes.PARAMETER_ERROR, "Network probe target is empty.");
        }
        List<Endpoint> endpoints = parseEndpoints(rawTarget, defaultScheme);
        if (endpoints.isEmpty()) {
            throw new CliException(
                ExitCodes.PARAMETER_ERROR,
                "Failed to parse host:port from target: " + rawTarget
            );
        }

        List<EndpointResult> results = new ArrayList<EndpointResult>(endpoints.size());
        boolean anyReachable = false;
        for (Endpoint endpoint : endpoints) {
            EndpointResult result = probeEndpoint(endpoint, timeoutMs);
            if (result.isReachable()) {
                anyReachable = true;
            }
            results.add(result);
        }
        return new ProbeSummary(rawTarget.trim(), defaultScheme, anyReachable, Collections.unmodifiableList(results));
    }

    private EndpointResult probeEndpoint(Endpoint endpoint, int timeoutMs) {
        Socket socket = new Socket();
        try {
            socket.connect(new InetSocketAddress(endpoint.host, endpoint.port), timeoutMs);
            InetAddress inetAddress = socket.getInetAddress();
            return new EndpointResult(
                endpoint.rendered,
                endpoint.host,
                endpoint.port,
                true,
                inetAddress == null ? null : inetAddress.getHostAddress(),
                null
            );
        } catch (Exception exception) {
            return new EndpointResult(
                endpoint.rendered,
                endpoint.host,
                endpoint.port,
                false,
                null,
                exception.getClass().getSimpleName() + ": " + exception.getMessage()
            );
        } finally {
            try {
                socket.close();
            } catch (Exception ignore) {
            }
        }
    }

    private List<Endpoint> parseEndpoints(String rawTarget, String defaultScheme) {
        String target = rawTarget.trim();
        int schemeSeparator = target.indexOf("://");
        String scheme = schemeSeparator >= 0 ? target.substring(0, schemeSeparator) : defaultScheme;
        String remainder = schemeSeparator >= 0 ? target.substring(schemeSeparator + 3) : target;
        int pathSeparator = indexOfPathSeparator(remainder);
        String authority = pathSeparator >= 0 ? remainder.substring(0, pathSeparator) : remainder;
        String[] chunks = authority.split(",");
        List<Endpoint> endpoints = new ArrayList<Endpoint>(chunks.length);
        for (String chunk : chunks) {
            Endpoint endpoint = parseEndpoint(chunk, scheme);
            if (endpoint != null) {
                endpoints.add(endpoint);
            }
        }
        return endpoints;
    }

    private Endpoint parseEndpoint(String rawEndpoint, String scheme) {
        String endpoint = rawEndpoint == null ? "" : rawEndpoint.trim();
        if (endpoint.isEmpty()) {
            return null;
        }

        String host;
        int port;
        if (endpoint.startsWith("[")) {
            int closingBracket = endpoint.indexOf(']');
            if (closingBracket < 0 || closingBracket + 2 >= endpoint.length() || endpoint.charAt(closingBracket + 1) != ':') {
                throw new CliException(ExitCodes.PARAMETER_ERROR, "Invalid IPv6 endpoint: " + rawEndpoint);
            }
            host = endpoint.substring(1, closingBracket);
            port = parsePort(endpoint.substring(closingBracket + 2), rawEndpoint);
        } else {
            int separator = endpoint.lastIndexOf(':');
            if (separator <= 0 || separator == endpoint.length() - 1) {
                throw new CliException(ExitCodes.PARAMETER_ERROR, "Endpoint is missing host:port: " + rawEndpoint);
            }
            host = endpoint.substring(0, separator).trim();
            port = parsePort(endpoint.substring(separator + 1), rawEndpoint);
        }

        String rendered = (scheme == null || scheme.trim().isEmpty())
            ? host + ":" + port
            : scheme + "://" + host + ":" + port;
        return new Endpoint(rendered, host, port);
    }

    private int parsePort(String rawPort, String rawEndpoint) {
        try {
            return Integer.parseInt(rawPort.trim());
        } catch (Exception exception) {
            throw new CliException(ExitCodes.PARAMETER_ERROR, "Invalid port in endpoint: " + rawEndpoint, exception);
        }
    }

    private int indexOfPathSeparator(String value) {
        int slash = value.indexOf('/');
        int question = value.indexOf('?');
        int hash = value.indexOf('#');
        int candidate = -1;
        if (slash >= 0) {
            candidate = slash;
        }
        if (question >= 0 && (candidate < 0 || question < candidate)) {
            candidate = question;
        }
        if (hash >= 0 && (candidate < 0 || hash < candidate)) {
            candidate = hash;
        }
        return candidate;
    }

    private static final class Endpoint {
        private final String rendered;
        private final String host;
        private final int port;

        Endpoint(String rendered, String host, int port) {
            this.rendered = rendered;
            this.host = host;
            this.port = port;
        }
    }

    public static final class ProbeSummary {
        private final String target;
        private final String defaultScheme;
        private final boolean reachable;
        private final List<EndpointResult> endpoints;

        ProbeSummary(String target, String defaultScheme, boolean reachable, List<EndpointResult> endpoints) {
            this.target = target;
            this.defaultScheme = defaultScheme;
            this.reachable = reachable;
            this.endpoints = endpoints;
        }

        public String getTarget() {
            return target;
        }

        public String getDefaultScheme() {
            return defaultScheme;
        }

        public boolean isReachable() {
            return reachable;
        }

        public List<EndpointResult> getEndpoints() {
            return endpoints;
        }
    }

    public static final class EndpointResult {
        private final String target;
        private final String host;
        private final int port;
        private final boolean reachable;
        private final String resolvedAddress;
        private final String error;

        EndpointResult(
            String target,
            String host,
            int port,
            boolean reachable,
            String resolvedAddress,
            String error
        ) {
            this.target = target;
            this.host = host;
            this.port = port;
            this.reachable = reachable;
            this.resolvedAddress = resolvedAddress;
            this.error = error;
        }

        public String getTarget() {
            return target;
        }

        public String getHost() {
            return host;
        }

        public int getPort() {
            return port;
        }

        public boolean isReachable() {
            return reachable;
        }

        public String getResolvedAddress() {
            return resolvedAddress;
        }

        public String getError() {
            return error;
        }
    }
}
