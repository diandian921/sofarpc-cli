package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.context.RpcInvokeContext;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Thin façade the daemon uses to perform SOFARPC generic calls. Delegates connection
 * caching to {@link ConnectionManager} and result shaping to {@link ResultFlattener}.
 *
 * @author wuwh
 */
public final class SofaRpcGateway {

    private static final Logger LOG = LoggerFactory.getLogger(SofaRpcGateway.class);

    private final ConnectionManager connectionManager;

    public SofaRpcGateway(ConnectionManager connectionManager) {
        this.connectionManager = connectionManager;
    }

    public RpcCallResult call(RpcCallSpec spec) {
        long start = System.currentTimeMillis();
        int connectTimeoutMs = remainingTimeoutMs(spec, start);
        if (connectTimeoutMs <= 0) {
            return timeoutBeforeInvoke(start);
        }
        try (ConsumerLease lease = connectionManager.acquire(spec.toTargetKey(), connectTimeoutMs)) {
            int rpcTimeoutMs = remainingTimeoutMs(spec, start);
            if (rpcTimeoutMs <= 0) {
                return timeoutBeforeInvoke(start);
            }
            GenericService svc = lease.service();
            RpcInvokeContext.getContext().setTimeout(rpcTimeoutMs);
            Object[] args = GenericArgumentConverter.convert(spec.getArgTypes(), spec.getArgs());
            Object raw = svc.$genericInvoke(spec.getMethod(), spec.getArgTypes(), args);
            Object flattened = ResultFlattener.flatten(raw);
            long elapsed = System.currentTimeMillis() - start;
            return RpcCallResult.success(flattened, elapsed);
        } catch (Throwable t) {
            long elapsed = System.currentTimeMillis() - start;
            LOG.debug("rpc call failed: {}", spec, t);
            return RpcCallResult.failure(t, elapsed);
        } finally {
            RpcInvokeContext.removeContext();
        }
    }

    public ConnectionManager getConnectionManager() {
        return connectionManager;
    }

    private int remainingTimeoutMs(RpcCallSpec spec, long start) {
        if (spec.getTimeoutMs() <= 0) {
            return 0;
        }
        long elapsed = System.currentTimeMillis() - start;
        long remaining = (long) spec.getTimeoutMs() - elapsed;
        if (remaining <= 0L) {
            return 0;
        }
        return remaining > Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) remaining;
    }

    private RpcCallResult timeoutBeforeInvoke(long start) {
        return RpcCallResult.failure(new RuntimeException("connect timed out before rpc invoke"),
                System.currentTimeMillis() - start);
    }
}
