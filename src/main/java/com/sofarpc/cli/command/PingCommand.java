package com.sofarpc.cli.command;

import com.alipay.sofa.rpc.api.GenericService;
import com.sofarpc.cli.core.ExceptionClassifier;
import com.sofarpc.cli.core.ExitCodes;
import com.sofarpc.cli.core.GlobalConfig;
import com.sofarpc.cli.core.RpcClientFactory;
import com.sofarpc.cli.core.ServerStore;
import com.sofarpc.cli.output.JsonPrinter;
import com.sofarpc.cli.service.OutputFormatter;
import picocli.CommandLine.Command;
import picocli.CommandLine.Option;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.Callable;

/**
 * Ping command: smoke test via generic invocation of a non-existent method.
 * If the server responds (even with a "method not found" error), it's UP.
 *
 * @author wuweihua
 */
@Command(name = "ping", mixinStandardHelpOptions = true,
    description = "Smoke test: check if a service is reachable via RPC.")
public class PingCommand implements Callable<Integer> {

    @Option(names = "--server", required = true, description = "Server alias.")
    private String server;

    @Option(names = "--service", required = true, description = "Full qualified interface name.")
    private String service;

    @Option(names = "--timeout", description = "Timeout in milliseconds.")
    private Integer timeout;

    @Option(names = "--json", description = "Output in JSON format.")
    private boolean json;

    @Override
    public Integer call() {
        int effectiveTimeout = timeout != null ? timeout : GlobalConfig.getInstance().getTimeout();
        if (effectiveTimeout <= 0) {
            OutputFormatter.printError(
                "timeout 必须为正整数，当前值: " + effectiveTimeout, ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }

        String address;
        try {
            address = new ServerStore().resolveAddress(server);
        } catch (Exception e) {
            OutputFormatter.printError(
                "读取服务配置失败: " + e.getMessage(), ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }
        if (address == null) {
            OutputFormatter.printError(
                "服务别名不存在: " + server, ExitCodes.ALIAS_NOT_FOUND, json);
            return ExitCodes.ALIAS_NOT_FOUND;
        }

        Map<String, Object> result = new LinkedHashMap<>();
        result.put("server", server);
        result.put("address", address);
        result.put("service", service);

        long start = System.currentTimeMillis();
        try {
            GenericService genericService = RpcClientFactory.getOrCreate(
                server, address, service, effectiveTimeout);
            genericService.$genericInvoke("__sofarpc_cli_ping__", new String[0], new Object[0]);
            long latency = System.currentTimeMillis() - start;
            result.put("status", "UP");
            result.put("latencyMs", latency);
        } catch (Exception e) {
            long latency = System.currentTimeMillis() - start;
            if (ExceptionClassifier.isServerReachable(e)) {
                result.put("status", "UP");
                result.put("latencyMs", latency);
            } else {
                result.put("status", "DOWN");
                result.put("latencyMs", latency);
                String msg = e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName();
                result.put("error", msg);
                printResult(result);
                return ExitCodes.CONNECT_FAIL;
            }
        }

        printResult(result);
        return ExitCodes.SUCCESS;
    }

    private void printResult(Map<String, Object> result) {
        if (json) {
            JsonPrinter.print(result);
            return;
        }
        boolean up = "UP".equals(result.get("status"));
        String icon = up ? "✅" : "❌";
        String status = up ? "服务可达" : "服务不可达";
        System.out.println(icon + " " + status + ": " + server + " (" + result.get("address") + ")");
        System.out.println("  耗时: " + result.get("latencyMs") + "ms");
        if (result.containsKey("error")) {
            System.out.println("  错误: " + result.get("error"));
        }
    }
}
