package com.sofarpc.daemon.server;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.DaemonLifecycle;
import com.sofarpc.daemon.handler.Dispatcher;
import com.sofarpc.daemon.protocol.JsonCodec;
import com.sofarpc.daemon.rpc.ConnectionManager;
import com.sofarpc.daemon.rpc.SofaRpcGateway;
import org.junit.Test;

import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.DataInputStream;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

/**
 * Verifies the JSON-RPC compatibility path in the same length-prefixed TCP loop
 * used by legacy envelope requests.
 *
 * @author wuwh
 */
public class JsonRpcConnectionLoopTest {

    private final ObjectMapper mapper = JsonCodec.mapper();

    @Test
    public void statusRequiresHello() throws Exception {
        Harness h = startLoop("secret");
        try {
            JsonNode resp = h.call("{\"jsonrpc\":\"2.0\",\"id\":\"r1\",\"method\":\"engine.status\",\"params\":{}}");

            assertEquals("2.0", resp.get("jsonrpc").asText());
            assertEquals(-32001, resp.get("error").get("code").asInt());
        } finally {
            h.close();
        }
    }

    @Test
    public void helloThenStatusSucceeds() throws Exception {
        Harness h = startLoop("secret");
        try {
            JsonNode hello = h.call("{\"jsonrpc\":\"2.0\",\"id\":\"h1\",\"method\":\"engine.hello\",\"params\":{\"token\":\"secret\"}}");
            assertTrue(hello.has("result"));
            assertEquals("1", hello.get("result").get("protocolVersion").asText());

            JsonNode status = h.call("{\"jsonrpc\":\"2.0\",\"id\":\"s1\",\"method\":\"engine.status\",\"params\":{}}");
            assertTrue(status.has("result"));
            assertEquals("test", status.get("result").get("engineVersion").asText());
        } finally {
            h.close();
        }
    }

    private Harness startLoop(String token) throws Exception {
        ServerSocket server = new ServerSocket(0, 1, InetAddress.getLoopbackAddress());
        Socket client = new Socket(InetAddress.getLoopbackAddress(), server.getLocalPort());
        Socket accepted = server.accept();
        server.close();

        ConnectionManager manager = new ConnectionManager();
        DaemonContext ctx = new DaemonContext(
                System.currentTimeMillis(),
                "test",
                token,
                123L,
                new SofaRpcGateway(manager),
                new DaemonLifecycle());
        ctx.setPort(accepted.getLocalPort());
        Thread thread = new Thread(new ConnectionLoop(accepted, new Dispatcher(), ctx), "jsonrpc-loop-test");
        thread.start();
        return new Harness(client, thread);
    }

    private final class Harness {
        private final Socket socket;
        private final Thread thread;
        private final DataInputStream in;
        private final BufferedOutputStream out;

        private Harness(Socket socket, Thread thread) throws Exception {
            this.socket = socket;
            this.thread = thread;
            this.in = new DataInputStream(new BufferedInputStream(socket.getInputStream()));
            this.out = new BufferedOutputStream(socket.getOutputStream());
        }

        JsonNode call(String json) throws Exception {
            Framing.writeFrame(out, json.getBytes(StandardCharsets.UTF_8));
            byte[] resp = Framing.readFrame(in);
            return mapper.readTree(resp);
        }

        void close() throws Exception {
            socket.close();
            thread.join(1000L);
        }
    }
}
