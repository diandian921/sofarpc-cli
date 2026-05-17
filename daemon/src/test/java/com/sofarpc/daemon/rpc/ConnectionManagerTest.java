package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;
import org.junit.Test;

import java.util.ArrayList;
import java.util.List;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;

/**
 * Pins the bounded warm-reuse contract of ConnectionManager: one consumer per stable
 * target, reused across calls, with a hard cap and eviction that releases the consumer.
 * This is the regression guard for the unbounded/timeout-keyed cache that leaked
 * connections on long runs.
 *
 * @author wuwh
 */
public class ConnectionManagerTest {

    private static final String IFACE = "com.company.user.api.UserQueryService";

    private static final class FakeConsumer implements ManagedConsumer {
        private final GenericService svc = mock(GenericService.class);
        private boolean closed;

        @Override
        public GenericService service() {
            return svc;
        }

        @Override
        public void close() {
            closed = true;
        }
    }

    private static final class RecordingFactory implements ConsumerFactory {
        private final List<RpcTargetKey> created = new ArrayList<>();
        private final List<FakeConsumer> consumers = new ArrayList<>();
        private final List<Integer> connectTimeouts = new ArrayList<>();

        @Override
        public ManagedConsumer create(RpcTargetKey key, int connectTimeoutMs) {
            created.add(key);
            connectTimeouts.add(connectTimeoutMs);
            FakeConsumer c = new FakeConsumer();
            consumers.add(c);
            return c;
        }
    }

    @Test
    public void sameTargetReusesConsumerAndCreatesOnce() {
        RecordingFactory factory = new RecordingFactory();
        ConnectionManager cm = new ConnectionManager(256, factory);
        RpcTargetKey key = new RpcTargetKey("10.0.0.1:12200", IFACE);

        GenericService first;
        GenericService second;
        try (ConsumerLease lease = cm.acquire(key, 1000)) {
            first = lease.service();
        }
        try (ConsumerLease lease = cm.acquire(key, 2000)) {
            second = lease.service();
        }

        assertSame(first, second);
        assertEquals(1, factory.created.size());
        assertEquals(1, cm.size());
    }

    @Test
    public void boundedCacheEvictsLeastRecentlyUsedAndClosesIt() {
        RecordingFactory factory = new RecordingFactory();
        ConnectionManager cm = new ConnectionManager(2, factory);

        cm.acquire(new RpcTargetKey("addr-1:12200", IFACE), 1000).close();
        cm.acquire(new RpcTargetKey("addr-2:12200", IFACE), 1000).close();
        cm.acquire(new RpcTargetKey("addr-3:12200", IFACE), 1000).close();

        assertEquals(2, cm.size());
        assertTrue("evicted LRU consumer must be closed", factory.consumers.get(0).closed);
        assertFalse(factory.consumers.get(1).closed);
        assertFalse(factory.consumers.get(2).closed);
    }

    @Test
    public void activeEvictedConsumerIsClosedOnlyAfterLeaseReturns() {
        RecordingFactory factory = new RecordingFactory();
        ConnectionManager cm = new ConnectionManager(1, factory);

        ConsumerLease active = cm.acquire(new RpcTargetKey("addr-1:12200", IFACE), 1000);
        cm.acquire(new RpcTargetKey("addr-2:12200", IFACE), 1000).close();

        assertEquals(1, cm.size());
        assertFalse("in-flight consumer must not be closed by eviction", factory.consumers.get(0).closed);

        active.close();

        assertTrue("evicted consumer closes after the last lease returns", factory.consumers.get(0).closed);
        assertFalse(factory.consumers.get(1).closed);
    }

    @Test
    public void destroyAllClosesEveryConsumer() {
        RecordingFactory factory = new RecordingFactory();
        ConnectionManager cm = new ConnectionManager(256, factory);
        cm.acquire(new RpcTargetKey("addr-1:12200", IFACE), 1000).close();
        cm.acquire(new RpcTargetKey("addr-2:12200", IFACE), 1000).close();

        cm.destroyAll();

        assertEquals(0, cm.size());
        assertTrue(factory.consumers.get(0).closed);
        assertTrue(factory.consumers.get(1).closed);
    }

    @Test
    public void destroyByAddressClosesOnlyMatchingTarget() {
        RecordingFactory factory = new RecordingFactory();
        ConnectionManager cm = new ConnectionManager(256, factory);
        cm.acquire(new RpcTargetKey("addr-1:12200", IFACE), 1000).close();
        cm.acquire(new RpcTargetKey("addr-2:12200", IFACE), 1000).close();

        cm.destroyByAddress("addr-1:12200");

        assertEquals(1, cm.size());
        assertTrue(factory.consumers.get(0).closed);
        assertFalse(factory.consumers.get(1).closed);
    }

    @Test
    public void passesConnectTimeoutToFactory() {
        RecordingFactory factory = new RecordingFactory();
        ConnectionManager cm = new ConnectionManager(256, factory);

        cm.acquire(new RpcTargetKey("addr-1:12200", IFACE), 321).close();

        assertEquals(Integer.valueOf(321), factory.connectTimeouts.get(0));
    }

    @Test(expected = IllegalArgumentException.class)
    public void rejectsNonPositiveMaxConsumers() {
        new ConnectionManager(0, new RecordingFactory());
    }
}
