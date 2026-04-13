package com.sofarpc.daemon.server;

import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.handler.Dispatcher;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Loopback-only TCP server. Each accepted connection runs in its own worker thread.
 *
 * @author wuwh
 */
public final class TcpServer {

    private static final Logger LOG = LoggerFactory.getLogger(TcpServer.class);

    private static final int CORE_POOL_SIZE = 2;
    private static final int MAX_POOL_SIZE = 32;
    private static final long KEEP_ALIVE_SECONDS = 60L;
    private static final int QUEUE_CAPACITY = 256;

    private final int requestedPort;
    private final Dispatcher dispatcher;
    private final DaemonContext ctx;

    private ServerSocket serverSocket;
    private Thread acceptThread;
    private ExecutorService workers;
    private volatile boolean running;

    public TcpServer(int requestedPort, Dispatcher dispatcher, DaemonContext ctx) {
        this.requestedPort = requestedPort;
        this.dispatcher = dispatcher;
        this.ctx = ctx;
    }

    public synchronized int start() throws IOException {
        if (running) {
            throw new IllegalStateException("server already running");
        }
        serverSocket = new ServerSocket(requestedPort, 64, InetAddress.getLoopbackAddress());
        workers = buildWorkerPool();
        running = true;
        acceptThread = new Thread(this::acceptLoop, "sofarpcd-accept");
        acceptThread.setDaemon(false);
        acceptThread.start();
        int bound = serverSocket.getLocalPort();
        LOG.info("TCP server listening on 127.0.0.1:{}", bound);
        return bound;
    }

    public synchronized void stop() {
        running = false;
        if (serverSocket != null) {
            try {
                serverSocket.close();
            } catch (IOException e) {
                LOG.debug("close listening socket failed: {}", e.getMessage());
            }
        }
        if (workers != null) {
            workers.shutdown();
            try {
                if (!workers.awaitTermination(5, TimeUnit.SECONDS)) {
                    workers.shutdownNow();
                }
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                workers.shutdownNow();
            }
        }
    }

    private void acceptLoop() {
        while (running) {
            Socket client;
            try {
                client = serverSocket.accept();
            } catch (IOException e) {
                if (running) {
                    LOG.warn("accept failed: {}", e.getMessage());
                }
                return;
            }
            submitOrReject(client);
        }
    }

    /**
     * Hands {@code client} to the worker pool. If the pool's bounded queue is full the task
     * is rejected and the socket is closed — we explicitly do NOT use CallerRunsPolicy here,
     * because the caller is the accept thread and ConnectionLoop is a long-lived per-connection
     * loop. Running it inline would block all subsequent accept() calls behind one slow client.
     * Closing the socket gives the peer an immediate RST so it can retry elsewhere.
     */
    private void submitOrReject(Socket client) {
        try {
            workers.execute(new ConnectionLoop(client, dispatcher, ctx));
        } catch (RejectedExecutionException rejected) {
            LOG.warn("worker pool saturated, dropping connection from {}", client.getRemoteSocketAddress());
            try {
                client.close();
            } catch (IOException closeEx) {
                LOG.debug("close rejected socket failed: {}", closeEx.getMessage());
            }
        }
    }

    private ExecutorService buildWorkerPool() {
        AtomicInteger seq = new AtomicInteger();
        ThreadFactory tf = r -> {
            Thread t = new Thread(r, "sofarpcd-worker-" + seq.incrementAndGet());
            t.setDaemon(false);
            return t;
        };
        return new ThreadPoolExecutor(
                CORE_POOL_SIZE,
                MAX_POOL_SIZE,
                KEEP_ALIVE_SECONDS,
                TimeUnit.SECONDS,
                new LinkedBlockingQueue<>(QUEUE_CAPACITY),
                tf,
                new ThreadPoolExecutor.AbortPolicy());
    }
}
