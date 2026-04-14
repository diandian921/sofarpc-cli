package com.sofarpc.daemon.protocol;

/**
 * Wire error codes. Must stay in sync with protocol/schema/envelope.response.schema.json.
 *
 * @author wuwh
 */
public enum ErrorCode {
    SUCCESS,
    BAD_REQUEST,
    CONNECT_FAILED,
    RPC_TIMEOUT,
    INVOKE_FAILED,
    ASSERTION_FAILED,
    DAEMON_UNAVAILABLE,
    INTERNAL_ERROR
}
