package com.sofarpc.daemon;

import com.sofarpc.daemon.rpc.SofaRpcGateway;

import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Shared runtime state passed to handlers. Holds references to long-lived singletons
 * (rpc gateway, lifecycle) plus lightweight counters consumed by health / idle TTL.
 *
 * @author wuwh
 */
public final class DaemonContext {

    private final long startedAtMs;
    private final String buildVersion;
    private final long pid;
    private final SofaRpcGateway rpcGateway;
    private final DaemonLifecycle lifecycle;

    private final AtomicInteger liveConnections = new AtomicInteger();
    private final AtomicLong lastActivityMs = new AtomicLong();

    private volatile int port;

    public DaemonContext(long startedAtMs,
                         String buildVersion,
                         long pid,
                         SofaRpcGateway rpcGateway,
                         DaemonLifecycle lifecycle) {
        this.startedAtMs = startedAtMs;
        this.buildVersion = buildVersion;
        this.pid = pid;
        this.rpcGateway = rpcGateway;
        this.lifecycle = lifecycle;
        this.lastActivityMs.set(startedAtMs);
    }

    public long getStartedAtMs() {
        return startedAtMs;
    }

    public String getBuildVersion() {
        return buildVersion;
    }

    public long getPid() {
        return pid;
    }

    public int getPort() {
        return port;
    }

    public void setPort(int port) {
        this.port = port;
    }

    public SofaRpcGateway getRpcGateway() {
        return rpcGateway;
    }

    public DaemonLifecycle getLifecycle() {
        return lifecycle;
    }

    public int getLiveConnections() {
        return liveConnections.get();
    }

    public long getLastActivityMs() {
        return lastActivityMs.get();
    }

    public void connectionOpened() {
        liveConnections.incrementAndGet();
        markActivity();
    }

    public void connectionClosed() {
        liveConnections.decrementAndGet();
    }

    public void markActivity() {
        lastActivityMs.set(System.currentTimeMillis());
    }
}
