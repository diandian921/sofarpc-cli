package com.sofarpc.daemon.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Wire-level response envelope. Matches protocol/schema/envelope.response.schema.json.
 *
 * @author wuwh
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
public class ResponseEnvelope {

    private String requestId;
    private boolean ok;
    private String code;
    private Object data;
    private ResponseError error;
    private Map<String, Object> meta = new LinkedHashMap<>();

    public static ResponseEnvelope success(String requestId, Object data) {
        ResponseEnvelope env = new ResponseEnvelope();
        env.requestId = requestId;
        env.ok = true;
        env.code = ErrorCode.SUCCESS.name();
        env.data = data;
        env.error = null;
        return env;
    }

    public static ResponseEnvelope failure(String requestId, ErrorCode code, ResponseError error, Object data) {
        ResponseEnvelope env = new ResponseEnvelope();
        env.requestId = requestId;
        env.ok = false;
        env.code = code.name();
        env.data = data;
        env.error = error;
        return env;
    }

    public static ResponseEnvelope failure(String requestId, ErrorCode code, String message) {
        return failure(requestId, code, new ResponseError(message), null);
    }

    public String getRequestId() {
        return requestId;
    }

    public void setRequestId(String requestId) {
        this.requestId = requestId;
    }

    public boolean isOk() {
        return ok;
    }

    public void setOk(boolean ok) {
        this.ok = ok;
    }

    public String getCode() {
        return code;
    }

    public void setCode(String code) {
        this.code = code;
    }

    public Object getData() {
        return data;
    }

    public void setData(Object data) {
        this.data = data;
    }

    public ResponseError getError() {
        return error;
    }

    public void setError(ResponseError error) {
        this.error = error;
    }

    public Map<String, Object> getMeta() {
        return meta;
    }

    public void setMeta(Map<String, Object> meta) {
        this.meta = meta == null ? Collections.emptyMap() : meta;
    }

    @Override
    public String toString() {
        return "ResponseEnvelope{requestId='" + requestId + "', ok=" + ok + ", code='" + code + "'}";
    }
}
