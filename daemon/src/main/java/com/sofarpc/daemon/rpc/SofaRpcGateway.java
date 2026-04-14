package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;
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
        try {
            GenericService svc = connectionManager.getOrCreate(spec.toTargetKey());
            Object raw = svc.$genericInvoke(spec.getMethod(), spec.getArgTypes(), spec.getArgs());
            Object flattened = ResultFlattener.flatten(raw);
            long elapsed = System.currentTimeMillis() - start;
            return RpcCallResult.success(flattened, elapsed);
        } catch (Throwable t) {
            long elapsed = System.currentTimeMillis() - start;
            LOG.debug("rpc call failed: {}", spec, t);
            return RpcCallResult.failure(t, elapsed);
        }
    }

    public ConnectionManager getConnectionManager() {
        return connectionManager;
    }
}
