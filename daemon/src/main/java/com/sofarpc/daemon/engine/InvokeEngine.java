package com.sofarpc.daemon.engine;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.sofarpc.daemon.protocol.ErrorCode;
import com.sofarpc.daemon.protocol.JsonCodec;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;
import com.sofarpc.daemon.protocol.ResponseError;
import com.sofarpc.daemon.rpc.RpcCallResult;
import com.sofarpc.daemon.rpc.RpcCallSpec;
import com.sofarpc.daemon.rpc.RpcErrorClassifier;
import com.sofarpc.daemon.rpc.SofaRpcGateway;

import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Executes the invoke op: parses payload, calls the RPC gateway, runs assertions, and
 * shapes the response envelope. No network or framing concerns here.
 *
 * @author wuwh
 */
public final class InvokeEngine {

    private static final int DEFAULT_TIMEOUT_MS = 5000;

    private final SofaRpcGateway gateway;
    private final ObjectMapper mapper = JsonCodec.mapper();

    public InvokeEngine(SofaRpcGateway gateway) {
        this.gateway = gateway;
    }

    public ResponseEnvelope run(RequestEnvelope req) {
        InvokePayload payload;
        try {
            payload = parse(req.getPayload());
        } catch (IllegalArgumentException e) {
            return ResponseEnvelope.failure(req.getRequestId(), ErrorCode.BAD_REQUEST, e.getMessage());
        }
        RpcCallSpec spec = new RpcCallSpec(
                payload.address, payload.service, payload.method,
                payload.argTypes, payload.args, payload.timeoutMs);
        RpcCallResult result = gateway.call(spec);
        if (!result.isOk()) {
            return buildFailure(req, result, payload);
        }
        return buildSuccess(req, result, payload);
    }

    private ResponseEnvelope buildSuccess(RequestEnvelope req, RpcCallResult result, InvokePayload payload) {
        AssertionEvaluator.Result assertions = AssertionEvaluator.evaluate(result.getResult(), payload.assertions);
        Map<String, Object> data = new LinkedHashMap<>();
        data.put("result", result.getResult());
        data.put("elapsedMs", result.getElapsedMs());
        if (!assertions.getOutcomes().isEmpty()) {
            data.put("assertions", assertions.getOutcomes());
        }
        if (assertions.getFailedCount() > 0) {
            ResponseError err = new ResponseError(
                    assertions.getFailedCount() + " of " + assertions.getTotal() + " assertions failed");
            return ResponseEnvelope.failure(req.getRequestId(), ErrorCode.ASSERTION_FAILED, err, data);
        }
        return ResponseEnvelope.success(req.getRequestId(), data);
    }

    private ResponseEnvelope buildFailure(RequestEnvelope req, RpcCallResult result, InvokePayload payload) {
        Throwable t = result.getError();
        ErrorCode code = RpcErrorClassifier.classify(t);
        Map<String, Object> details = new LinkedHashMap<>();
        details.put("address", payload.address);
        details.put("service", payload.service);
        details.put("method", payload.method);
        details.put("rpcTimeoutMs", payload.timeoutMs);
        ResponseError err = new ResponseError(
                t.getMessage() != null ? t.getMessage() : t.getClass().getSimpleName(),
                t.getClass().getName(),
                details);
        return ResponseEnvelope.failure(req.getRequestId(), code, err, null);
    }

    private InvokePayload parse(JsonNode node) {
        if (node == null || !node.isObject()) {
            throw new IllegalArgumentException("payload must be an object");
        }
        InvokePayload p = new InvokePayload();
        p.address = readRequiredString(node, "address");
        p.service = readRequiredString(node, "service");
        p.method = readRequiredString(node, "method");
        p.argTypes = readStringArray(node, "argTypes");
        p.args = readArgs(node);
        if (p.argTypes.length != p.args.length) {
            throw new IllegalArgumentException(
                    "argTypes length (" + p.argTypes.length + ") does not match args length (" + p.args.length + ")");
        }
        p.timeoutMs = node.hasNonNull("rpcTimeoutMs") ? node.get("rpcTimeoutMs").asInt(DEFAULT_TIMEOUT_MS) : DEFAULT_TIMEOUT_MS;
        if (p.timeoutMs <= 0) {
            throw new IllegalArgumentException("rpcTimeoutMs must be positive");
        }
        p.assertions = readAssertions(node);
        return p;
    }

    private String readRequiredString(JsonNode node, String field) {
        if (!node.hasNonNull(field) || !node.get(field).isTextual()) {
            throw new IllegalArgumentException(field + " is required and must be a string");
        }
        String v = node.get(field).asText();
        if (v.isEmpty()) {
            throw new IllegalArgumentException(field + " must not be empty");
        }
        return v;
    }

    private String[] readStringArray(JsonNode node, String field) {
        if (!node.hasNonNull(field) || !node.get(field).isArray()) {
            throw new IllegalArgumentException(field + " must be an array of strings");
        }
        JsonNode arr = node.get(field);
        String[] out = new String[arr.size()];
        for (int i = 0; i < arr.size(); i++) {
            JsonNode item = arr.get(i);
            if (!item.isTextual()) {
                throw new IllegalArgumentException(field + "[" + i + "] must be a string");
            }
            out[i] = item.asText();
        }
        return out;
    }

    private Object[] readArgs(JsonNode node) {
        if (!node.hasNonNull("args") || !node.get("args").isArray()) {
            throw new IllegalArgumentException("args must be an array");
        }
        JsonNode arr = node.get("args");
        Object[] out = new Object[arr.size()];
        for (int i = 0; i < arr.size(); i++) {
            out[i] = jsonValue(arr.get(i));
        }
        return out;
    }

    static Object jsonValue(JsonNode node) {
        if (node == null || node.isNull()) {
            return null;
        }
        if (node.isTextual()) {
            return node.asText();
        }
        if (node.isBoolean()) {
            return node.booleanValue();
        }
        if (node.isIntegralNumber()) {
            if (node.canConvertToInt()) {
                return node.intValue();
            }
            if (node.canConvertToLong()) {
                return node.longValue();
            }
            return node.bigIntegerValue();
        }
        if (node.isFloatingPointNumber()) {
            if (node.isFloat() || node.isDouble()) {
                return node.doubleValue();
            }
            return node.decimalValue();
        }
        if (node.isArray()) {
            List<Object> values = new ArrayList<Object>();
            for (JsonNode item : node) {
                values.add(jsonValue(item));
            }
            return values;
        }
        if (node.isObject()) {
            Map<String, Object> values = new LinkedHashMap<String, Object>();
            Iterator<Map.Entry<String, JsonNode>> fields = node.fields();
            while (fields.hasNext()) {
                Map.Entry<String, JsonNode> field = fields.next();
                values.put(field.getKey(), jsonValue(field.getValue()));
            }
            return values;
        }
        return node.asText();
    }

    @SuppressWarnings("unchecked")
    private List<Map<String, Object>> readAssertions(JsonNode node) {
        if (!node.hasNonNull("assertions")) {
            return java.util.Collections.emptyList();
        }
        if (!node.get("assertions").isArray()) {
            throw new IllegalArgumentException("assertions must be an array");
        }
        return (List<Map<String, Object>>) (List<?>) mapper.convertValue(node.get("assertions"), List.class);
    }

    private static final class InvokePayload {
        String address;
        String service;
        String method;
        String[] argTypes;
        Object[] args;
        int timeoutMs;
        List<Map<String, Object>> assertions;
    }
}
