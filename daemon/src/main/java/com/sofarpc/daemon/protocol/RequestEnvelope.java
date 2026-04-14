package com.sofarpc.daemon.protocol;

import com.fasterxml.jackson.databind.JsonNode;

import java.util.Collections;
import java.util.Map;

/**
 * Wire-level request envelope. Matches protocol/schema/envelope.request.schema.json exactly:
 * unknown top-level fields are rejected so the contract does not drift silently. Extension
 * must go through meta (free-form map) or payload (op-specific JsonNode).
 *
 * @author wuwh
 */
public class RequestEnvelope {

    private String requestId;
    private String op;
    private Map<String, Object> meta = Collections.emptyMap();
    private JsonNode payload;

    public String getRequestId() {
        return requestId;
    }

    public void setRequestId(String requestId) {
        this.requestId = requestId;
    }

    public String getOp() {
        return op;
    }

    public void setOp(String op) {
        this.op = op;
    }

    public Map<String, Object> getMeta() {
        return meta;
    }

    public void setMeta(Map<String, Object> meta) {
        this.meta = meta == null ? Collections.emptyMap() : meta;
    }

    public JsonNode getPayload() {
        return payload;
    }

    public void setPayload(JsonNode payload) {
        this.payload = payload;
    }

    @Override
    public String toString() {
        return "RequestEnvelope{requestId='" + requestId + "', op='" + op + "'}";
    }
}
