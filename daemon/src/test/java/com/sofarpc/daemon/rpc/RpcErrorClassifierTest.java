package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.core.exception.SofaTimeOutException;
import com.sofarpc.daemon.protocol.ErrorCode;
import org.junit.Test;

import java.io.IOException;
import java.net.ConnectException;
import java.net.NoRouteToHostException;
import java.net.PortUnreachableException;
import java.net.SocketException;
import java.net.UnknownHostException;

import static org.junit.Assert.assertEquals;

/**
 * Pins the error classification rules agents depend on for retry decisions.
 *
 * @author wuwh
 */
public class RpcErrorClassifierTest {

    @Test
    public void timeoutIsRpcTimeout() {
        assertEquals(ErrorCode.RPC_TIMEOUT,
                RpcErrorClassifier.classify(new SofaTimeOutException("timeout")));
    }

    @Test
    public void unknownHostIsConnectFailed() {
        assertEquals(ErrorCode.CONNECT_FAILED,
                RpcErrorClassifier.classify(new UnknownHostException("no-such-host")));
    }

    @Test
    public void noRouteIsConnectFailed() {
        assertEquals(ErrorCode.CONNECT_FAILED,
                RpcErrorClassifier.classify(new NoRouteToHostException("unreachable")));
    }

    @Test
    public void portUnreachableIsConnectFailed() {
        assertEquals(ErrorCode.CONNECT_FAILED,
                RpcErrorClassifier.classify(new PortUnreachableException("no port")));
    }

    @Test
    public void connectRefusedIsConnectFailed() {
        assertEquals(ErrorCode.CONNECT_FAILED,
                RpcErrorClassifier.classify(new ConnectException("Connection refused")));
    }

    @Test
    public void socketExceptionIsConnectFailed() {
        assertEquals(ErrorCode.CONNECT_FAILED,
                RpcErrorClassifier.classify(new SocketException("connection reset")));
    }

    @Test
    public void nestedUnknownHostIsConnectFailed() {
        RuntimeException wrapper = new RuntimeException("rpc call failed",
                new UnknownHostException("bad.example"));
        assertEquals(ErrorCode.CONNECT_FAILED, RpcErrorClassifier.classify(wrapper));
    }

    @Test
    public void bareIoExceptionFallsThroughToInvokeFailed() {
        assertEquals(ErrorCode.INVOKE_FAILED,
                RpcErrorClassifier.classify(new IOException("stream closed")));
    }

    @Test
    public void genericRuntimeIsInvokeFailed() {
        assertEquals(ErrorCode.INVOKE_FAILED,
                RpcErrorClassifier.classify(new RuntimeException("boom")));
    }
}
