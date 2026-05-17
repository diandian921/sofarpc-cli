package com.sofarpc.daemon;

import com.sofarpc.daemon.engine.InvokeEngine;
import com.sofarpc.daemon.engine.PingEngine;
import com.sofarpc.daemon.handler.Dispatcher;
import com.sofarpc.daemon.handler.HealthHandler;
import com.sofarpc.daemon.handler.InvokeHandler;
import com.sofarpc.daemon.handler.PingHandler;
import com.sofarpc.daemon.handler.ShutdownHandler;
import com.sofarpc.daemon.lifecycle.IdleTracker;
import com.sofarpc.daemon.protocol.Op;
import com.sofarpc.daemon.rpc.ConnectionManager;
import com.sofarpc.daemon.rpc.SofaRpcGateway;
import com.sofarpc.daemon.server.TcpServer;
import com.sofarpc.daemon.state.StateFile;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.lang.management.ManagementFactory;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.concurrent.TimeUnit;

/**
 * Daemon entry point. Boots TCP server, registers handlers, writes state.json once ready,
 * and blocks until {@link DaemonLifecycle} signals shutdown.
 *
 * @author wuwh
 */
public final class Main {

    private static final Logger LOG = LoggerFactory.getLogger(Main.class);

    private static final String DEFAULT_BUILD_VERSION = "1.0.0";
    private static final long DEFAULT_IDLE_TTL_MS = TimeUnit.MINUTES.toMillis(15);

    public static void main(String[] args) {
        DaemonOptions opts = DaemonOptions.parse(args);

        long startedAtMs = System.currentTimeMillis();
        long pid = currentPid();
        String token = readToken(opts.tokenFile);
        ConnectionManager connectionManager = new ConnectionManager();
        SofaRpcGateway gateway = new SofaRpcGateway(connectionManager);
        DaemonLifecycle lifecycle = new DaemonLifecycle();
        DaemonContext ctx = new DaemonContext(startedAtMs, opts.buildVersion, token, pid, gateway, lifecycle);

        Dispatcher dispatcher = new Dispatcher()
                .register(Op.INVOKE, new InvokeHandler(new InvokeEngine(gateway)))
                .register(Op.PING, new PingHandler(new PingEngine()))
                .register(Op.HEALTH, new HealthHandler())
                .register(Op.SHUTDOWN, new ShutdownHandler());

        TcpServer server = new TcpServer(opts.port, dispatcher, ctx);
        StateFile stateFile = new StateFile(opts.stateFile);
        IdleTracker idle = new IdleTracker(opts.idleTtlMs, ctx, lifecycle);

        Runtime.getRuntime().addShutdownHook(new Thread(() -> {
            stateFile.deleteQuietly();
            idle.stop();
            server.stop();
            connectionManager.destroyAll();
        }, "sofarpcd-shutdown"));

        try {
            int port = server.start();
            ctx.setPort(port);
            stateFile.writeReady(pid, port, opts.buildVersion, startedAtMs);
            idle.start();
            LOG.info("daemon ready: pid={} port={} buildVersion={}", pid, port, opts.buildVersion);
            lifecycle.awaitShutdown();
            LOG.info("shutdown requested, grace={} ms", lifecycle.getRequestedGraceMs());
            if (lifecycle.getRequestedGraceMs() > 0L) {
                Thread.sleep(lifecycle.getRequestedGraceMs());
            }
        } catch (Exception e) {
            LOG.error("daemon failed to start or run", e);
            stateFile.deleteQuietly();
            System.exit(1);
        }
        System.exit(0);
    }

    private static long currentPid() {
        String name = ManagementFactory.getRuntimeMXBean().getName();
        int at = name.indexOf('@');
        if (at <= 0) {
            return -1L;
        }
        try {
            return Long.parseLong(name.substring(0, at));
        } catch (NumberFormatException e) {
            return -1L;
        }
    }

    private static String readToken(Path tokenFile) {
        if (tokenFile == null) {
            return null;
        }
        try {
            return new String(Files.readAllBytes(tokenFile), StandardCharsets.UTF_8).trim();
        } catch (Exception e) {
            throw new IllegalArgumentException("cannot read token file: " + tokenFile, e);
        }
    }

    private static final class DaemonOptions {
        int port;
        long idleTtlMs = DEFAULT_IDLE_TTL_MS;
        String buildVersion = DEFAULT_BUILD_VERSION;
        Path stateFile;
        Path tokenFile;

        static DaemonOptions parse(String[] args) {
            DaemonOptions opts = new DaemonOptions();
            opts.stateFile = defaultStateFile();
            for (int i = 0; i < args.length; i++) {
                String arg = args[i];
                switch (arg) {
                    case "--port":
                        opts.port = Integer.parseInt(args[++i]);
                        break;
                    case "--state-file":
                        opts.stateFile = Paths.get(args[++i]);
                        break;
                    case "--token-file":
                        opts.tokenFile = Paths.get(args[++i]);
                        break;
                    case "--host":
                        ++i;
                        break;
                    case "--log-file":
                        ++i;
                        break;
                    case "--cache-dir":
                        ++i;
                        break;
                    case "--max-concurrent-invokes":
                        ++i;
                        break;
                    case "--idle-ttl-ms":
                        opts.idleTtlMs = Long.parseLong(args[++i]);
                        break;
                    case "--build-version":
                        opts.buildVersion = args[++i];
                        break;
                    default:
                        throw new IllegalArgumentException("unknown option: " + arg);
                }
            }
            return opts;
        }

        private static Path defaultStateFile() {
            return Paths.get(System.getProperty("user.home"), ".sofarpc", "daemon", "state.json");
        }
    }
}
