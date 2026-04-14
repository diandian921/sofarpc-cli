package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.config.ConsumerConfig;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.Iterator;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Caches GenericService instances per target. All performance gains of the daemon
 * depend on this cache staying warm across requests.
 *
 * @author wuwh
 */
public final class ConnectionManager {

    private static final Logger LOG = LoggerFactory.getLogger(ConnectionManager.class);

    private static final String PROTOCOL_BOLT = "bolt";
    private static final String DIRECT_URL_PREFIX = "bolt://";
    private static final int CONNECTION_NUM = 1;
    private static final int RECONNECT_PERIOD = 0;

    private final Map<RpcTargetKey, ConsumerConfig<GenericService>> configs = new ConcurrentHashMap<>();
    private final Map<RpcTargetKey, GenericService> services = new ConcurrentHashMap<>();

    public GenericService getOrCreate(RpcTargetKey key) {
        return services.computeIfAbsent(key, this::createConsumer);
    }

    public int size() {
        return services.size();
    }

    public void destroyAll() {
        for (Map.Entry<RpcTargetKey, ConsumerConfig<GenericService>> entry : configs.entrySet()) {
            try {
                entry.getValue().unRefer();
            } catch (RuntimeException e) {
                LOG.debug("unRefer failed for {}: {}", entry.getKey(), e.getMessage());
            }
        }
        configs.clear();
        services.clear();
    }

    public void destroyByAddress(String address) {
        Iterator<Map.Entry<RpcTargetKey, ConsumerConfig<GenericService>>> it = configs.entrySet().iterator();
        while (it.hasNext()) {
            Map.Entry<RpcTargetKey, ConsumerConfig<GenericService>> entry = it.next();
            if (entry.getKey().getAddress().equals(address)) {
                try {
                    entry.getValue().unRefer();
                } catch (RuntimeException e) {
                    LOG.debug("unRefer failed for {}: {}", entry.getKey(), e.getMessage());
                }
                services.remove(entry.getKey());
                it.remove();
            }
        }
    }

    private GenericService createConsumer(RpcTargetKey key) {
        ConsumerConfig<GenericService> config = new ConsumerConfig<GenericService>()
                .setInterfaceId(key.getInterfaceId())
                .setGeneric(true)
                .setProtocol(PROTOCOL_BOLT)
                .setDirectUrl(DIRECT_URL_PREFIX + key.getAddress())
                .setRegister(false)
                .setSubscribe(false)
                .setTimeout(key.getTimeoutMs())
                .setConnectTimeout(key.getTimeoutMs())
                .setConnectionNum(CONNECTION_NUM)
                .setLazy(false)
                .setReconnectPeriod(RECONNECT_PERIOD);
        configs.put(key, config);
        return config.refer();
    }
}
