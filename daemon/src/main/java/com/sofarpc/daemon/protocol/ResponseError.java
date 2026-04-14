package com.sofarpc.daemon.protocol;

import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.Map;

/**
 * Structured error block returned alongside non-SUCCESS responses.
 *
 * @author wuwh
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ResponseError {

    private String message;
    private String cause;
    private Map<String, Object> details;

    public ResponseError() {
    }

    public ResponseError(String message) {
        this.message = message;
    }

    public ResponseError(String message, String cause, Map<String, Object> details) {
        this.message = message;
        this.cause = cause;
        this.details = details;
    }

    public String getMessage() {
        return message;
    }

    public void setMessage(String message) {
        this.message = message;
    }

    public String getCause() {
        return cause;
    }

    public void setCause(String cause) {
        this.cause = cause;
    }

    public Map<String, Object> getDetails() {
        return details;
    }

    public void setDetails(Map<String, Object> details) {
        this.details = details;
    }

    @Override
    public String toString() {
        return "ResponseError{message='" + message + "', cause='" + cause + "', details=" + details + '}';
    }
}
