package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;

/**
 * Bounded warm cache of SOFARPC generic consumers, one per stable target
 * (address + interface). The cap and LRU eviction prevent the unbounded
 * connection growth that previously caused long-run OOM; the call timeout is
 * applied per invocation via RpcInvokeContext, never baked into the key.
 *
 * @author wuwh
 */
public final class ConnectionManager {

    private static final Logger LOG = LoggerFactory.getLogger(ConnectionManager.class);

    private static final int DEFAULT_MAX_CONSUMERS = 256;
    private static final int CREATE_LOCK_STRIPES = 64;

    private final ConsumerFactory factory;
    private final int maxConsumers;
    private final Map<RpcTargetKey, CacheEntry> cache;
    private final Object[] createLocks = new Object[CREATE_LOCK_STRIPES];

    public ConnectionManager() {
        this(DEFAULT_MAX_CONSUMERS, new SofaConsumerFactory());
    }

    ConnectionManager(int maxConsumers, ConsumerFactory factory) {
        if (maxConsumers <= 0) {
            throw new IllegalArgumentException("maxConsumers must be > 0");
        }
        this.maxConsumers = maxConsumers;
        this.factory = Objects.requireNonNull(factory, "factory");
        for (int i = 0; i < createLocks.length; i++) {
            createLocks[i] = new Object();
        }
        this.cache = new LinkedHashMap<RpcTargetKey, CacheEntry>(16, 0.75f, true);
    }

    public ConsumerLease acquire(RpcTargetKey key, int connectTimeoutMs) {
        ConsumerLease lease = tryAcquireCached(key);
        if (lease != null) {
            return lease;
        }
        synchronized (createLock(key)) {
            lease = tryAcquireCached(key);
            if (lease != null) {
                return lease;
            }
            ManagedConsumer consumer = factory.create(key, connectTimeoutMs);
            synchronized (this) {
                CacheEntry existing = cache.get(key);
                if (existing != null) {
                    safeClose(key, consumer);
                    return existing.acquire();
                }
                CacheEntry created = new CacheEntry(key, consumer);
                cache.put(key, created);
                evictOverflow();
                return created.acquire();
            }
        }
    }

    public synchronized int size() {
        return cache.size();
    }

    public synchronized void destroyAll() {
        for (Map.Entry<RpcTargetKey, CacheEntry> entry : cache.entrySet()) {
            entry.getValue().evict();
        }
        cache.clear();
    }

    public synchronized void destroyByAddress(String address) {
        int removed = 0;
        Iterator<Map.Entry<RpcTargetKey, CacheEntry>> it = cache.entrySet().iterator();
        while (it.hasNext()) {
            Map.Entry<RpcTargetKey, CacheEntry> entry = it.next();
            if (entry.getKey().getAddress().equals(address)) {
                entry.getValue().evict();
                it.remove();
                removed++;
            }
        }
        if (removed > 0) {
            LOG.debug("destroyed {} consumer(s) for address {}", removed, address);
        }
    }

    private synchronized ConsumerLease tryAcquireCached(RpcTargetKey key) {
        CacheEntry entry = cache.get(key);
        if (entry == null) {
            return null;
        }
        return entry.acquire();
    }

    private void evictOverflow() {
        Iterator<Map.Entry<RpcTargetKey, CacheEntry>> it = cache.entrySet().iterator();
        while (cache.size() > maxConsumers && it.hasNext()) {
            Map.Entry<RpcTargetKey, CacheEntry> eldest = it.next();
            eldest.getValue().evict();
            it.remove();
        }
    }

    private Object createLock(RpcTargetKey key) {
        return createLocks[(key.hashCode() & 0x7fffffff) % createLocks.length];
    }

    private void safeClose(RpcTargetKey key, ManagedConsumer consumer) {
        try {
            consumer.close();
        } catch (RuntimeException e) {
            LOG.debug("close failed for {}: {}", key, e.getMessage());
        }
    }

    private final class CacheEntry {
        private final RpcTargetKey key;
        private final ManagedConsumer consumer;
        private int activeLeases;
        private boolean evicted;
        private boolean closed;

        private CacheEntry(RpcTargetKey key, ManagedConsumer consumer) {
            this.key = key;
            this.consumer = consumer;
        }

        private ConsumerLease acquire() {
            activeLeases++;
            return new Lease(this);
        }

        private void release() {
            synchronized (ConnectionManager.this) {
                activeLeases--;
                if (activeLeases == 0 && evicted) {
                    closeIfNeeded();
                }
            }
        }

        private void evict() {
            evicted = true;
            if (activeLeases == 0) {
                closeIfNeeded();
            }
        }

        private void closeIfNeeded() {
            if (!closed) {
                closed = true;
                safeClose(key, consumer);
            }
        }
    }

    private static final class Lease implements ConsumerLease {
        private final CacheEntry entry;
        private boolean closed;

        private Lease(CacheEntry entry) {
            this.entry = entry;
        }

        @Override
        public GenericService service() {
            return entry.consumer.service();
        }

        @Override
        public void close() {
            if (!closed) {
                closed = true;
                entry.release();
            }
        }
    }
}
