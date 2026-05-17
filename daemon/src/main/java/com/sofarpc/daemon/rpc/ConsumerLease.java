package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;

/**
 * A short-lived lease for a cached SOFARPC consumer. Callers must close the lease
 * after invocation so the cache can defer eviction until in-flight calls finish.
 *
 * @author wuwh
 */
public interface ConsumerLease extends AutoCloseable {

    GenericService service();

    @Override
    void close();
}
