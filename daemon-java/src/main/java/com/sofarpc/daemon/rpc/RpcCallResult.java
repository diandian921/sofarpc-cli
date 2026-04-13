package com.sofarpc.daemon.rpc;

/**
 * Outcome of a single SOFARPC generic call: either a flattened result or the raw throwable
 * that broke the call. Callers are responsible for turning this into a wire response.
 *
 * @author wuwh
 */
public final class RpcCallResult {

    private final boolean ok;
    private final Object result;
    private final Throwable error;
    private final long elapsedMs;

    private RpcCallResult(boolean ok, Object result, Throwable error, long elapsedMs) {
        this.ok = ok;
        this.result = result;
        this.error = error;
        this.elapsedMs = elapsedMs;
    }

    public static RpcCallResult success(Object result, long elapsedMs) {
        return new RpcCallResult(true, result, null, elapsedMs);
    }

    public static RpcCallResult failure(Throwable t, long elapsedMs) {
        return new RpcCallResult(false, null, t, elapsedMs);
    }

    public boolean isOk() {
        return ok;
    }

    public Object getResult() {
        return result;
    }

    public Throwable getError() {
        return error;
    }

    public long getElapsedMs() {
        return elapsedMs;
    }

    @Override
    public String toString() {
        return "RpcCallResult{ok=" + ok + ", elapsedMs=" + elapsedMs + '}';
    }
}
