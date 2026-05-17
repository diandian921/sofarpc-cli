package com.sofarpc.daemon.rpc;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotEquals;

/**
 * Pins consumer cache identity: timeout is a per-call concern and must not fragment
 * the cache. Two calls to the same address/interface must hit the same cached consumer
 * regardless of timeout, otherwise warm reuse is defeated and the cache grows unbounded.
 *
 * @author wuwh
 */
public class RpcTargetKeyTest {

    private static final String ADDR = "10.0.0.1:12200";
    private static final String IFACE = "com.company.user.api.UserQueryService";

    @Test
    public void sameTargetIsEqualRegardlessOfTimeout() {
        RpcTargetKey a = new RpcTargetKey(ADDR, IFACE);
        RpcTargetKey b = new RpcTargetKey(ADDR, IFACE);

        assertEquals(a, b);
        assertEquals(a.hashCode(), b.hashCode());
    }

    @Test
    public void differentAddressIsNotEqual() {
        RpcTargetKey a = new RpcTargetKey(ADDR, IFACE);
        RpcTargetKey b = new RpcTargetKey("10.0.0.2:12200", IFACE);

        assertNotEquals(a, b);
    }

    @Test
    public void differentInterfaceIsNotEqual() {
        RpcTargetKey a = new RpcTargetKey(ADDR, IFACE);
        RpcTargetKey b = new RpcTargetKey(ADDR, "com.company.user.api.OtherService");

        assertNotEquals(a, b);
    }
}
