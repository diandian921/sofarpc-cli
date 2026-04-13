package com.sofarpc.cli.core;

import com.alipay.sofa.rpc.core.exception.RpcErrorType;
import com.alipay.sofa.rpc.core.exception.SofaRpcException;
import com.alipay.sofa.rpc.core.exception.SofaRouteException;
import com.alipay.sofa.rpc.core.exception.SofaTimeOutException;

import java.net.ConnectException;

/**
 * Classify RPC exceptions into exit codes.
 * Distinguishes connection-level failures (exit 2) from invocation-level failures (exit 1).
 *
 * @author wuwh
 */
public final class ExceptionClassifier {

    private ExceptionClassifier() {
    }

    /**
     * Determine the appropriate exit code for the given exception.
     *
     * @param e the exception thrown during RPC invocation
     * @return ExitCodes.CONNECT_FAIL or ExitCodes.INVOKE_FAIL
     */
    public static int classify(Exception e) {
        if (e instanceof SofaTimeOutException) {
            return ExitCodes.CONNECT_FAIL;
        }
        if (e instanceof SofaRouteException) {
            return ExitCodes.CONNECT_FAIL;
        }
        if (e instanceof SofaRpcException) {
            return classifyByErrorType(((SofaRpcException) e).getErrorType());
        }
        if (e instanceof ConnectException) {
            return ExitCodes.CONNECT_FAIL;
        }
        return classifyByMessage(e);
    }

    /**
     * Check if an exception indicates the server is reachable.
     * Used by PingCommand: connection failure means DOWN, invocation failure means UP.
     *
     * @param e the exception from a ping attempt
     * @return true if the server responded (even with an error), false if unreachable
     */
    public static boolean isServerReachable(Exception e) {
        return classify(e) != ExitCodes.CONNECT_FAIL;
    }

    private static int classifyByErrorType(int errorType) {
        switch (errorType) {
            case RpcErrorType.CLIENT_TIMEOUT:
            case RpcErrorType.CLIENT_ROUTER:
            case RpcErrorType.CLIENT_NETWORK:
            case RpcErrorType.SERVER_CLOSED:
                return ExitCodes.CONNECT_FAIL;
            default:
                return ExitCodes.INVOKE_FAIL;
        }
    }

    private static int classifyByMessage(Exception e) {
        String msg = e.getMessage();
        if (msg == null) {
            return ExitCodes.INVOKE_FAIL;
        }
        String lower = msg.toLowerCase();
        if (lower.contains("connection refused")
            || lower.contains("connect timed out")
            || lower.contains("no available provider")) {
            return ExitCodes.CONNECT_FAIL;
        }
        return ExitCodes.INVOKE_FAIL;
    }
}
