package com.hex1n.sofarpcctl;

import org.junit.Assert;
import org.junit.Test;

import java.net.ServerSocket;

public class NetworkProbeTest {

    @Test
    public void probeMarksReachableEndpointAndStripsQuerySuffix() throws Exception {
        ServerSocket serverSocket = new ServerSocket(0);
        try {
            int port = serverSocket.getLocalPort();

            NetworkProbe.ProbeSummary summary =
                new NetworkProbe().probe("bolt://127.0.0.1:" + port + "?foo=bar", "bolt", 1000);

            Assert.assertTrue(summary.isReachable());
            Assert.assertEquals(1, summary.getEndpoints().size());
            Assert.assertEquals("bolt://127.0.0.1:" + port, summary.getEndpoints().get(0).getTarget());
            Assert.assertTrue(summary.getEndpoints().get(0).isReachable());
        } finally {
            serverSocket.close();
        }
    }

    @Test
    public void probeRejectsMalformedEndpoint() {
        try {
            new NetworkProbe().probe("bolt://127.0.0.1", "bolt", 1000);
            Assert.fail("Expected malformed endpoint to fail.");
        } catch (CliException exception) {
            Assert.assertEquals(ExitCodes.PARAMETER_ERROR, exception.getExitCode());
            Assert.assertTrue(exception.getMessage().contains("host:port"));
        }
    }
}
