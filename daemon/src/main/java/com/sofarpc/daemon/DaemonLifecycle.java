package com.sofarpc.daemon;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * Coordinates shutdown between handlers (which request it) and the main thread (which waits).
 *
 * @author wuwh
 */
public final class DaemonLifecycle {

    private final CountDownLatch stopLatch = new CountDownLatch(1);
    private volatile long requestedGraceMs = 0L;

    public void requestShutdown(long graceMs) {
        this.requestedGraceMs = Math.max(0L, graceMs);
        stopLatch.countDown();
    }

    public void awaitShutdown() throws InterruptedException {
        stopLatch.await();
    }

    public boolean awaitShutdown(long timeout, TimeUnit unit) throws InterruptedException {
        return stopLatch.await(timeout, unit);
    }

    public long getRequestedGraceMs() {
        return requestedGraceMs;
    }
}
