package com.sofarpc.daemon.rpc;

/**
 * Creates a {@link ManagedConsumer} for a target. Abstracted so the cache lifecycle
 * (reuse, bounding, eviction) can be tested without opening real connections.
 *
 * @author wuwh
 */
public interface ConsumerFactory {

    ManagedConsumer create(RpcTargetKey key, int connectTimeoutMs);
}
