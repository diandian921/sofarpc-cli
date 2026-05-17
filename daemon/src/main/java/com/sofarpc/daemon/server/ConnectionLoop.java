package com.sofarpc.daemon.server;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.JsonNode;
import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.handler.Dispatcher;
import com.sofarpc.daemon.protocol.ErrorCode;
import com.sofarpc.daemon.protocol.JsonCodec;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.DataInputStream;
import java.io.IOException;
import java.io.OutputStream;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Reads length-prefixed JSON requests from one TCP connection and writes envelope responses back.
 *
 * @author wuwh
 */
public final class ConnectionLoop implements Runnable {

    private static final Logger LOG = LoggerFactory.getLogger(ConnectionLoop.class);

    private final Socket socket;
    private final Dispatcher dispatcher;
    private final DaemonContext ctx;
    private final ObjectMapper mapper;
    private boolean jsonRpcAuthenticated;

    public ConnectionLoop(Socket socket, Dispatcher dispatcher, DaemonContext ctx) {
        this.socket = socket;
        this.dispatcher = dispatcher;
        this.ctx = ctx;
        this.mapper = JsonCodec.mapper();
    }

    @Override
    public void run() {
        ctx.connectionOpened();
        try (Socket s = socket;
             DataInputStream in = new DataInputStream(new BufferedInputStream(s.getInputStream()));
             OutputStream out = new BufferedOutputStream(s.getOutputStream())) {
            while (!Thread.currentThread().isInterrupted()) {
                byte[] frame = Framing.readFrame(in);
                if (frame == null) {
                    return;
                }
                ctx.markActivity();
                byte[] resp = handleOne(frame);
                writeResponse(out, resp);
            }
        } catch (IOException e) {
            LOG.debug("connection closed: {}", e.getMessage());
        } finally {
            ctx.connectionClosed();
        }
    }

    private byte[] handleOne(byte[] frame) {
        JsonNode root;
        try {
            root = mapper.readTree(frame);
        } catch (IOException parseEx) {
            LOG.warn("malformed request frame", parseEx);
            return encode(ResponseEnvelope.failure(null, ErrorCode.BAD_REQUEST, "malformed JSON: " + parseEx.getMessage()));
        }
        if (isJsonRpc(root)) {
            return encode(handleJsonRpc(root));
        }

        RequestEnvelope req;
        try {
            req = mapper.treeToValue(root, RequestEnvelope.class);
        } catch (IOException parseEx) {
            LOG.warn("malformed request envelope", parseEx);
            return encode(ResponseEnvelope.failure(null, ErrorCode.BAD_REQUEST, "malformed JSON: " + parseEx.getMessage()));
        }
        try {
            return encode(dispatcher.dispatch(req, ctx));
        } catch (RuntimeException dispatchEx) {
            LOG.error("dispatch failed for {}", req, dispatchEx);
            return encode(ResponseEnvelope.failure(req.getRequestId(), ErrorCode.INTERNAL_ERROR,
                    dispatchEx.getClass().getSimpleName() + ": " + dispatchEx.getMessage()));
        }
    }

    private boolean isJsonRpc(JsonNode root) {
        return root != null && root.isObject() && (root.has("jsonrpc") || root.has("method"));
    }

    private Map<String, Object> handleJsonRpc(JsonNode root) {
        Object id = root.has("id") ? mapper.convertValue(root.get("id"), Object.class) : null;
        if (!root.hasNonNull("method") || !root.get("method").isTextual()) {
            return jsonRpcError(id, -32600, "method is required", null);
        }
        String method = root.get("method").asText();
        JsonNode params = root.has("params") ? root.get("params") : mapper.createObjectNode();
        try {
            if ("engine.hello".equals(method)) {
                return handleHello(id, params);
            }
            if (!jsonRpcAuthenticated) {
                return jsonRpcError(id, -32001, "engine.hello is required before calling " + method, null);
            }
            switch (method) {
                case "engine.status":
                    return jsonRpcSuccess(id, statusData());
                case "engine.shutdown":
                    return jsonRpcSuccess(id, dispatchAsEnvelope(id, "shutdown", params));
                case "sofarpc.ping":
                    return jsonRpcSuccess(id, dispatchAsEnvelope(id, "ping", params));
                case "sofarpc.invoke":
                    return jsonRpcSuccess(id, dispatchAsEnvelope(id, "invoke", params));
                default:
                    return jsonRpcError(id, -32601, "method not found: " + method, null);
            }
        } catch (IllegalArgumentException e) {
            return jsonRpcError(id, -32602, e.getMessage(), null);
        } catch (RuntimeException e) {
            LOG.error("json-rpc dispatch failed for method={}", method, e);
            return jsonRpcError(id, -32603, e.getClass().getSimpleName() + ": " + e.getMessage(), null);
        }
    }

    private Map<String, Object> handleHello(Object id, JsonNode params) {
        String token = null;
        if (params != null && params.isObject() && params.hasNonNull("token") && params.get("token").isTextual()) {
            token = params.get("token").asText();
        }
        if (!ctx.acceptsToken(token)) {
            return jsonRpcError(id, -32001, "invalid engine token", null);
        }
        jsonRpcAuthenticated = true;
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("ok", true);
        result.put("engineVersion", ctx.getBuildVersion());
        result.put("protocolVersion", "1");
        result.put("pid", ctx.getPid());
        result.put("port", ctx.getPort());
        return jsonRpcSuccess(id, result);
    }

    private ResponseEnvelope dispatchAsEnvelope(Object id, String op, JsonNode params) {
        RequestEnvelope req = new RequestEnvelope();
        req.setRequestId(id == null ? null : String.valueOf(id));
        req.setOp(op);
        req.setPayload(params);
        return dispatcher.dispatch(req, ctx);
    }

    private Map<String, Object> statusData() {
        Map<String, Object> data = new LinkedHashMap<>();
        data.put("pid", ctx.getPid());
        data.put("engineVersion", ctx.getBuildVersion());
        data.put("protocolVersion", "1");
        data.put("startedAtMs", ctx.getStartedAtMs());
        data.put("port", ctx.getPort());
        data.put("connections", ctx.getLiveConnections());
        data.put("cachedRpcTargets", ctx.getRpcGateway().getConnectionManager().size());
        return data;
    }

    private Map<String, Object> jsonRpcSuccess(Object id, Object result) {
        Map<String, Object> resp = new LinkedHashMap<>();
        resp.put("jsonrpc", "2.0");
        resp.put("id", id);
        resp.put("result", result);
        return resp;
    }

    private Map<String, Object> jsonRpcError(Object id, int code, String message, Object data) {
        Map<String, Object> err = new LinkedHashMap<>();
        err.put("code", code);
        err.put("message", message);
        if (data != null) {
            err.put("data", data);
        }
        Map<String, Object> resp = new LinkedHashMap<>();
        resp.put("jsonrpc", "2.0");
        resp.put("id", id);
        resp.put("error", err);
        return resp;
    }

    private byte[] encode(Object resp) {
        try {
            return mapper.writeValueAsString(resp).getBytes(StandardCharsets.UTF_8);
        } catch (IOException e) {
            throw new IllegalStateException("encode response", e);
        }
    }

    private void writeResponse(OutputStream out, byte[] body) throws IOException {
        Framing.writeFrame(out, body);
    }
}
