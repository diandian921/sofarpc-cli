package com.sofarpc.daemon.lifecycle;

import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.DaemonLifecycle;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledThreadPoolExecutor;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;

/**
 * Watches the last-activity timestamp on {@link DaemonContext}. When no activity arrives
 * within the configured TTL, triggers a graceful shutdown via {@link DaemonLifecycle}.
 *
 * @author wuwh
 */
public final class IdleTracker {

    private static final Logger LOG = LoggerFactory.getLogger(IdleTracker.class);

    private static final long CHECK_INTERVAL_MS = 30_000L;

    private final long idleTtlMs;
    private final DaemonContext ctx;
    private final DaemonLifecycle lifecycle;
    private ScheduledExecutorService scheduler;

    public IdleTracker(long idleTtlMs, DaemonContext ctx, DaemonLifecycle lifecycle) {
        this.idleTtlMs = idleTtlMs;
        this.ctx = ctx;
        this.lifecycle = lifecycle;
    }

    public synchronized void start() {
        if (idleTtlMs <= 0L) {
            LOG.info("idle TTL disabled");
            return;
        }
        ThreadFactory tf = r -> {
            Thread t = new Thread(r, "sofarpcd-idle");
            t.setDaemon(true);
            return t;
        };
        scheduler = new ScheduledThreadPoolExecutor(1, tf);
        scheduler.scheduleAtFixedRate(this::checkIdle, CHECK_INTERVAL_MS, CHECK_INTERVAL_MS, TimeUnit.MILLISECONDS);
        LOG.info("idle TTL enabled: {} ms", idleTtlMs);
    }

    public synchronized void stop() {
        if (scheduler != null) {
            scheduler.shutdownNow();
            scheduler = null;
        }
    }

    private void checkIdle() {
        try {
            if (ctx.getLiveConnections() > 0) {
                return;
            }
            long idleFor = System.currentTimeMillis() - ctx.getLastActivityMs();
            if (idleFor >= idleTtlMs) {
                LOG.info("idle for {} ms, triggering shutdown", idleFor);
                lifecycle.requestShutdown(0L);
            }
        } catch (RuntimeException e) {
            LOG.warn("idle check failed", e);
        }
    }
}
