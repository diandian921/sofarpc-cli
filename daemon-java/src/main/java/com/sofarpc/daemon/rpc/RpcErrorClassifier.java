package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.core.exception.RpcErrorType;
import com.alipay.sofa.rpc.core.exception.SofaRouteException;
import com.alipay.sofa.rpc.core.exception.SofaRpcException;
import com.alipay.sofa.rpc.core.exception.SofaTimeOutException;
import com.sofarpc.daemon.protocol.ErrorCode;

import java.net.ConnectException;
import java.net.NoRouteToHostException;
import java.net.PortUnreachableException;
import java.net.SocketException;
import java.net.UnknownHostException;

/**
 * Maps SOFARPC exceptions to protocol ErrorCode values. Single source of truth for daemon.
 *
 * @author wuwh
 */
public final class RpcErrorClassifier {

    private RpcErrorClassifier() {
    }

    public static ErrorCode classify(Throwable t) {
        if (t instanceof SofaTimeOutException) {
            return ErrorCode.RPC_TIMEOUT;
        }
        if (t instanceof SofaRouteException) {
            return ErrorCode.CONNECT_FAILED;
        }
        if (t instanceof SofaRpcException) {
            return classifyByErrorType(((SofaRpcException) t).getErrorType());
        }
        ErrorCode reachability = classifyReachability(t);
        if (reachability != null) {
            return reachability;
        }
        Throwable cause = t.getCause();
        if (cause != null && cause != t) {
            ErrorCode nested = classifyReachability(cause);
            if (nested != null) {
                return nested;
            }
        }
        return classifyByMessage(t);
    }

    /**
     * Returns a CONNECT_FAILED code for anything that unambiguously means the target network
     * endpoint was unreachable at the transport layer. Returns null if the throwable is not
     * obviously a reachability failure and the caller should keep classifying.
     */
    private static ErrorCode classifyReachability(Throwable t) {
        if (t instanceof UnknownHostException
                || t instanceof NoRouteToHostException
                || t instanceof PortUnreachableException
                || t instanceof ConnectException) {
            return ErrorCode.CONNECT_FAILED;
        }
        if (t instanceof SocketException) {
            return ErrorCode.CONNECT_FAILED;
        }
        return null;
    }

    public static boolean isReachableException(Throwable t) {
        ErrorCode code = classify(t);
        return code != ErrorCode.CONNECT_FAILED && code != ErrorCode.RPC_TIMEOUT;
    }

    private static ErrorCode classifyByErrorType(int errorType) {
        switch (errorType) {
            case RpcErrorType.CLIENT_TIMEOUT:
                return ErrorCode.RPC_TIMEOUT;
            case RpcErrorType.CLIENT_ROUTER:
            case RpcErrorType.CLIENT_NETWORK:
            case RpcErrorType.SERVER_CLOSED:
                return ErrorCode.CONNECT_FAILED;
            default:
                return ErrorCode.INVOKE_FAILED;
        }
    }

    private static ErrorCode classifyByMessage(Throwable t) {
        String msg = t.getMessage();
        if (msg == null) {
            return ErrorCode.INVOKE_FAILED;
        }
        String lower = msg.toLowerCase();
        if (lower.contains("connect timed out")) {
            return ErrorCode.RPC_TIMEOUT;
        }
        if (lower.contains("connection refused")
                || lower.contains("no available provider")
                || lower.contains("unknown host")
                || lower.contains("no route to host")
                || lower.contains("network is unreachable")
                || lower.contains("connection reset")
                || lower.contains("broken pipe")) {
            return ErrorCode.CONNECT_FAILED;
        }
        return ErrorCode.INVOKE_FAILED;
    }
}
