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

    public static String allowedWireValues() {
        StringBuilder sb = new StringBuilder();
        Op[] all = values();
        for (int i = 0; i < all.length; i++) {
            if (i > 0) {
                sb.append(", ");
            }
            sb.append(all[i].wire);
        }
        return sb.toString();
    }
}
