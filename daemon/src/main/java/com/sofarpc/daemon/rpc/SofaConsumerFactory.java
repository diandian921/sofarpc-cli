package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.config.ConsumerConfig;

/**
 * Default {@link ConsumerFactory}: builds a bolt direct-url generic consumer and
 * eagerly refers it so the bounded cache stays warm. The call timeout is applied
 * per invocation via RpcInvokeContext, so it is not bound to the consumer here.
 *
 * @author wuwh
 */
final class SofaConsumerFactory implements ConsumerFactory {

    private static final String PROTOCOL_BOLT = "bolt";
    private static final String DIRECT_URL_PREFIX = "bolt://";
    private static final int CONNECTION_NUM = 1;
    private static final int RECONNECT_PERIOD = 0;
    @Override
    public ManagedConsumer create(RpcTargetKey key, int connectTimeoutMs) {
        ConsumerConfig<GenericService> config = new ConsumerConfig<GenericService>()
                .setInterfaceId(key.getInterfaceId())
                .setGeneric(true)
                .setProtocol(PROTOCOL_BOLT)
                .setDirectUrl(DIRECT_URL_PREFIX + key.getAddress())
                .setRegister(false)
                .setSubscribe(false)
                .setConnectTimeout(connectTimeoutMs)
                .setConnectionNum(CONNECTION_NUM)
                .setLazy(false)
                .setReconnectPeriod(RECONNECT_PERIOD);
        GenericService service = config.refer();
        return new SofaManagedConsumer(config, service);
    }
}
