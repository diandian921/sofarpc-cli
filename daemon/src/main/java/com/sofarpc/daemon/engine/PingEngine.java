package com.sofarpc.daemon.engine;

import com.fasterxml.jackson.databind.JsonNode;
import com.sofarpc.daemon.protocol.ErrorCode;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;
import com.sofarpc.daemon.protocol.ResponseError;
import com.sofarpc.daemon.rpc.RpcErrorClassifier;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Implements ping: a lightweight TCP connect check against the target bolt address.
 * If the caller supplies a service, a service-level reachability check can be layered later;
 * V1 keeps it to TCP-level reachability which is what agents need to pre-flight an invoke.
 *
 * @author wuwh
 */
public final class PingEngine {

    private static final int DEFAULT_TIMEOUT_MS = 3000;

    public ResponseEnvelope run(RequestEnvelope req) {
        PingPayload payload;
        try {
            payload = parse(req.getPayload());
        } catch (IllegalArgumentException e) {
            return ResponseEnvelope.failure(req.getRequestId(), ErrorCode.BAD_REQUEST, e.getMessage());
        }
        InetSocketAddress endpoint = parseAddress(payload.address);
        long start = System.currentTimeMillis();
        try (Socket s = new Socket()) {
            s.connect(endpoint, payload.timeoutMs);
            long elapsed = System.currentTimeMillis() - start;
            Map<String, Object> data = new LinkedHashMap<>();
            data.put("reachable", true);
            data.put("elapsedMs", elapsed);
            data.put("address", payload.address);
            return ResponseEnvelope.success(req.getRequestId(), data);
        } catch (IOException e) {
            long elapsed = System.currentTimeMillis() - start;
            Map<String, Object> data = new LinkedHashMap<>();
            data.put("reachable", false);
            data.put("elapsedMs", elapsed);
            data.put("address", payload.address);
            ErrorCode code = RpcErrorClassifier.classify(e);
            if (code == ErrorCode.INVOKE_FAILED) {
                code = ErrorCode.CONNECT_FAILED;
            }
            Map<String, Object> details = new LinkedHashMap<>();
            details.put("address", payload.address);
            ResponseError err = new ResponseError(
                    e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName(),
                    e.getClass().getName(),
                    details);
            return ResponseEnvelope.failure(req.getRequestId(), code, err, data);
        }
    }

    private InetSocketAddress parseAddress(String address) {
        int colon = address.lastIndexOf(':');
        if (colon <= 0 || colon == address.length() - 1) {
            throw new IllegalArgumentException("invalid address: " + address);
        }
        String host = address.substring(0, colon);
        int port;
        try {
            port = Integer.parseInt(address.substring(colon + 1));
        } catch (NumberFormatException e) {
            throw new IllegalArgumentException("invalid port in address: " + address);
        }
        return new InetSocketAddress(host, port);
    }

    private PingPayload parse(JsonNode node) {
        if (node == null || !node.isObject()) {
            throw new IllegalArgumentException("payload must be an object");
        }
        if (!node.hasNonNull("address") || !node.get("address").isTextual()) {
            throw new IllegalArgumentException("address is required");
        }
        PingPayload p = new PingPayload();
        p.address = node.get("address").asText();
        p.timeoutMs = node.hasNonNull("rpcTimeoutMs") ? node.get("rpcTimeoutMs").asInt(DEFAULT_TIMEOUT_MS) : DEFAULT_TIMEOUT_MS;
        if (p.timeoutMs <= 0) {
            throw new IllegalArgumentException("rpcTimeoutMs must be positive");
        }
        return p;
    }

    private static final class PingPayload {
        String address;
        int timeoutMs;
    }
}
