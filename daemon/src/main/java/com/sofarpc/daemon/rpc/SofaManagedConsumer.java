package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.config.ConsumerConfig;

/**
 * Production {@link ManagedConsumer} backed by a SOFARPC {@link ConsumerConfig}.
 * Closing calls {@code unRefer()} to release the bolt connection and unregister.
 *
 * @author wuwh
 */
final class SofaManagedConsumer implements ManagedConsumer {

    private final ConsumerConfig<GenericService> config;
    private final GenericService service;

    SofaManagedConsumer(ConsumerConfig<GenericService> config, GenericService service) {
        this.config = config;
        this.service = service;
    }

    @Override
    public GenericService service() {
        return service;
    }

    @Override
    public void close() {
        config.unRefer();
    }
}
