package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;

/**
 * A cached SOFARPC generic consumer plus the means to release it. Closing must
 * release the underlying connection and unregister the consumer so an evicted
 * entry does not leak connections.
 *
 * @author wuwh
 */
public interface ManagedConsumer {

    GenericService service();

    void close();
}
