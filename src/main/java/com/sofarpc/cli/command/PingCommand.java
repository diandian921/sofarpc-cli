package com.sofarpc.cli.command;

import com.alipay.sofa.rpc.api.GenericService;
import com.sofarpc.cli.core.GlobalConfig;
import com.sofarpc.cli.core.RpcClientFactory;
import com.sofarpc.cli.core.ServerStore;
import com.sofarpc.cli.output.JsonPrinter;
import picocli.CommandLine.Command;
import picocli.CommandLine.Option;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Ping command: smoke test via generic invocation of a non-existent method.
 * If the server responds (even with a "method not found" error), it's UP.
 *
 * @author wuweihua
 */
@Command(name = "ping", description = "Smoke test: check if a service is reachable via RPC.")
public class PingCommand implements Runnable {

    private static final int EXIT_DOWN = 1;
    private static final int EXIT_CONNECT_FAIL = 2;
    private static final int EXIT_ALIAS_NOT_FOUND = 4;

    @Option(names = "--server", required = true, description = "Server alias.")
    private String server;

    @Option(names = "--service", required = true, description = "Full qualified interface name.")
    private String service;

    @Option(names = "--timeout", description = "Timeout in milliseconds.")
    private Integer timeout;

    @Override
    public void run() {
        int effectiveTimeout = timeout != null ? timeout : GlobalConfig.getInstance().getTimeout();

        ServerStore store = new ServerStore();
        String address = store.resolveAddress(server);
        if (address == null) {
            System.err.println("❌ 服务别名不存在: " + server);
            System.exit(EXIT_ALIAS_NOT_FOUND);
            return;
        }

        Map<String, Object> result = new LinkedHashMap<>();
        result.put("server", server);
        result.put("address", address);
        result.put("service", service);

        long start = System.currentTimeMillis();
        try {
            GenericService genericService = RpcClientFactory.getOrCreate(server, address, service, effectiveTimeout);
            // Invoke a non-existent method; any response (including error) means the service is UP
            genericService.$genericInvoke("__sofarpc_cli_ping__", new String[0], new Object[0]);
            // If we get here without exception, service is UP (unlikely but possible)
            long latency = System.currentTimeMillis() - start;
            result.put("status", "UP");
            result.put("latencyMs", latency);
        } catch (Exception e) {
            long latency = System.currentTimeMillis() - start;
            String msg = e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName();
            // If we got a response from the server (method not found, etc.), service is UP
            if (isServerReachable(e)) {
                result.put("status", "UP");
                result.put("latencyMs", latency);
            } else {
                result.put("status", "DOWN");
                result.put("latencyMs", latency);
                result.put("error", msg);
                JsonPrinter.print(result);
                System.exit(EXIT_CONNECT_FAIL);
                return;
            }
        }

        JsonPrinter.print(result);
    }

    /**
     * Determine if the exception indicates the server is reachable.
     * Connection refused / timeout means DOWN; method-not-found or business errors mean UP.
     */
    private boolean isServerReachable(Exception e) {
        String msg = e.getMessage() != null ? e.getMessage().toLowerCase() : "";
        String className = e.getClass().getName().toLowerCase();
        // Connection-level failures
        if (msg.contains("connection refused")
            || msg.contains("connect timed out")
            || msg.contains("no available provider")
            || msg.contains("timeout")
            || className.contains("timeoutexception")
            || className.contains("connectexception")) {
            return false;
        }
        // Everything else (method not found, service error, etc.) means the server responded
        return true;
    }
}
