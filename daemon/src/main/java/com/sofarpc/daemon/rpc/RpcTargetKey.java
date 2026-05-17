package com.sofarpc.daemon.rpc;

import java.util.Objects;

/**
 * Immutable cache key for {@link ConnectionManager}. Identity is the stable RPC target
 * only: address + interface. The call timeout is a per-invocation concern applied via
 * RpcInvokeContext, so it is intentionally not part of the key — including it would
 * fragment the cache and defeat warm consumer reuse.
 *
 * @author wuwh
 */
public final class RpcTargetKey {

    private final String address;
    private final String interfaceId;

    public RpcTargetKey(String address, String interfaceId) {
        this.address = Objects.requireNonNull(address, "address");
        this.interfaceId = Objects.requireNonNull(interfaceId, "interfaceId");
    }

    public String getAddress() {
        return address;
    }

    public String getInterfaceId() {
        return interfaceId;
    }

    @Override
    public boolean equals(Object o) {
        if (this == o) {
            return true;
        }
        if (!(o instanceof RpcTargetKey)) {
            return false;
        }
        RpcTargetKey other = (RpcTargetKey) o;
        return address.equals(other.address)
                && interfaceId.equals(other.interfaceId);
    }

    @Override
    public int hashCode() {
        return Objects.hash(address, interfaceId);
    }

    @Override
    public String toString() {
        return address + "::" + interfaceId;
    }
}
