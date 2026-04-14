package com.sofarpc.daemon.rpc;

/**
 * Parameters of a single RPC call carried through the daemon layer.
 *
 * @author wuwh
 */
public final class RpcCallSpec {

    private final String address;
    private final String interfaceId;
    private final String method;
    private final String[] argTypes;
    private final Object[] args;
    private final int timeoutMs;

    public RpcCallSpec(String address,
                       String interfaceId,
                       String method,
                       String[] argTypes,
                       Object[] args,
                       int timeoutMs) {
        this.address = address;
        this.interfaceId = interfaceId;
        this.method = method;
        this.argTypes = argTypes;
        this.args = args;
        this.timeoutMs = timeoutMs;
    }

    public String getAddress() {
        return address;
    }

    public String getInterfaceId() {
        return interfaceId;
    }

    public String getMethod() {
        return method;
    }

    public String[] getArgTypes() {
        return argTypes;
    }

    public Object[] getArgs() {
        return args;
    }

    public int getTimeoutMs() {
        return timeoutMs;
    }

    public RpcTargetKey toTargetKey() {
        return new RpcTargetKey(address, interfaceId, timeoutMs);
    }

    @Override
    public String toString() {
        return "RpcCallSpec{" + address + " " + interfaceId + "#" + method + " timeoutMs=" + timeoutMs + '}';
    }
}
