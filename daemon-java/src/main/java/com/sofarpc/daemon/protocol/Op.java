package com.sofarpc.daemon.protocol;

/**
 * Operation codes accepted by the daemon.
 *
 * @author wuwh
 */
public enum Op {
    INVOKE("invoke"),
    PING("ping"),
    HEALTH("health"),
    SHUTDOWN("shutdown");

    private final String wire;

    Op(String wire) {
        this.wire = wire;
    }

    public String wire() {
        return wire;
    }

    public static Op fromWire(String wire) {
        if (wire == null) {
            return null;
        }
        for (Op op : values()) {
            if (op.wire.equals(wire)) {
                return op;
            }
        }
        return null;
    }
}
