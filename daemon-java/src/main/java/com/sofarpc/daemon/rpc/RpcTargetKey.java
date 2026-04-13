package com.sofarpc.daemon.rpc;

import java.util.Objects;

/**
 * Immutable cache key for ConnectionManager. Includes timeout because SOFARPC binds the
 * client timeout to the ConsumerConfig — different timeouts must yield different consumers.
 *
 * @author wuwh
 */
public final class RpcTargetKey {

    private final String address;
    private final String interfaceId;
    private final int timeoutMs;

    public RpcTargetKey(String address, String interfaceId, int timeoutMs) {
        this.address = Objects.requireNonNull(address, "address");
        this.interfaceId = Objects.requireNonNull(interfaceId, "interfaceId");
        this.timeoutMs = timeoutMs;
    }

    public String getAddress() {
        return address;
    }

    public String getInterfaceId() {
        return interfaceId;
    }

    public int getTimeoutMs() {
        return timeoutMs;
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
        return timeoutMs == other.timeoutMs
                && address.equals(other.address)
                && interfaceId.equals(other.interfaceId);
    }

    @Override
    public int hashCode() {
        return Objects.hash(address, interfaceId, timeoutMs);
    }

    @Override
    public String toString() {
        return address + "::" + interfaceId + "::" + timeoutMs;
    }
}
