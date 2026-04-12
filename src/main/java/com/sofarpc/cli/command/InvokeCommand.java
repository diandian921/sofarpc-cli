package com.sofarpc.cli.command;

import com.alipay.sofa.rpc.api.GenericService;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.jayway.jsonpath.JsonPath;
import com.sofarpc.cli.core.GlobalConfig;
import com.sofarpc.cli.core.RpcClientFactory;
import com.sofarpc.cli.core.ServerStore;
import com.sofarpc.cli.output.JsonPrinter;
import picocli.CommandLine.Command;
import picocli.CommandLine.Option;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Invoke command: call a single RPC method via generic invocation.
 * Supports single object parameter, multi-parameter array, and no-arg invocation.
 *
 * @author wuweihua
 */
@Command(name = "invoke", description = "Invoke a single RPC method.")
public class InvokeCommand implements Runnable {

    private static final int EXIT_INVOKE_FAIL = 1;
    private static final int EXIT_CONNECT_FAIL = 2;
    private static final int EXIT_BAD_ARGS = 3;
    private static final int EXIT_ALIAS_NOT_FOUND = 4;

    private static final ObjectMapper OBJECT_MAPPER = new ObjectMapper();

    @Option(names = "--server", required = true, description = "Server alias.")
    private String server;

    @Option(names = "--service", required = true, description = "Full qualified interface name.")
    private String service;

    @Option(names = "--method", required = true, description = "Method name.")
    private String method;

    @Option(names = "--arg-types", description = "Parameter types, comma separated. Required when --args is specified.")
    private String argTypes;

    @Option(names = "--args", description = "JSON parameters. Single object for one param, array for multiple params.")
    private String args;

    @Option(names = "--assert", description = "JSONPath assertion expression, e.g. '$.status == \"ACTIVE\"'.")
    private String assertExpr;

    @Option(names = "--timeout", description = "Timeout in milliseconds.")
    private Integer timeout;

    @Option(names = "--json", description = "Output in JSON format.")
    private boolean json;

    @Override
    public void run() {
        int effectiveTimeout = timeout != null ? timeout : GlobalConfig.getInstance().getTimeout();

        // Resolve server alias
        ServerStore store = new ServerStore();
        String address = store.resolveAddress(server);
        if (address == null) {
            printError("服务别名不存在: " + server);
            System.exit(EXIT_ALIAS_NOT_FOUND);
            return;
        }

        // Parse arg-types and args
        String[] typeArray;
        Object[] argArray;
        try {
            ParsedArgs parsed = parseArgs();
            typeArray = parsed.types;
            argArray = parsed.values;
        } catch (Exception e) {
            printError("参数解析失败: " + e.getMessage());
            System.exit(EXIT_BAD_ARGS);
            return;
        }

        // Invoke
        Map<String, Object> output = new LinkedHashMap<>();
        long start = System.currentTimeMillis();
        try {
            GenericService genericService = RpcClientFactory.getOrCreate(server, address, service, effectiveTimeout);
            Object rawResult = genericService.$genericInvoke(method, typeArray, argArray);
            Object result = RpcClientFactory.flattenResult(rawResult);
            long latency = System.currentTimeMillis() - start;

            output.put("success", true);
            output.put("latencyMs", latency);
            output.put("result", result);

            // Assertion
            if (assertExpr != null && !assertExpr.isEmpty()) {
                boolean assertionPassed = evaluateAssertion(result, assertExpr);
                output.put("assertionPassed", assertionPassed);
                if (!assertionPassed) {
                    output.put("error", "Assertion failed: " + assertExpr);
                } else {
                    output.put("error", null);
                }
                printOutput(output);
                if (!assertionPassed) {
                    System.exit(EXIT_INVOKE_FAIL);
                }
            } else {
                output.put("error", null);
                printOutput(output);
            }
        } catch (Exception e) {
            long latency = System.currentTimeMillis() - start;
            output.put("success", false);
            output.put("latencyMs", latency);
            output.put("result", null);
            output.put("error", e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName());
            printOutput(output);
            System.exit(EXIT_INVOKE_FAIL);
        }
    }

    private ParsedArgs parseArgs() throws Exception {
        // No args
        if (args == null || args.isEmpty()) {
            return new ParsedArgs(new String[0], new Object[0]);
        }

        // arg-types is required when args is specified
        if (argTypes == null || argTypes.isEmpty()) {
            throw new IllegalArgumentException("--arg-types is required when --args is specified");
        }

        String[] types = argTypes.split(",");
        for (int i = 0; i < types.length; i++) {
            types[i] = types[i].trim();
        }

        String trimmed = args.trim();
        if (trimmed.startsWith("[")) {
            // Multi-parameter: JSON array
            List<Object> list = OBJECT_MAPPER.readValue(trimmed,
                OBJECT_MAPPER.getTypeFactory().constructCollectionType(List.class, Object.class));
            if (list.size() != types.length) {
                throw new IllegalArgumentException(
                    "args array size (" + list.size() + ") does not match arg-types count (" + types.length + ")");
            }
            return new ParsedArgs(types, list.toArray());
        } else {
            // Single parameter: JSON object or primitive
            Object value = OBJECT_MAPPER.readValue(trimmed, Object.class);
            if (types.length != 1) {
                throw new IllegalArgumentException(
                    "Single object arg provided but arg-types has " + types.length + " types");
            }
            return new ParsedArgs(types, new Object[]{value});
        }
    }

    @SuppressWarnings("unchecked")
    private boolean evaluateAssertion(Object result, String expression) {
        try {
            // Convert result to JSON string, then parse with JsonPath
            String jsonStr = OBJECT_MAPPER.writeValueAsString(result);

            // Parse expression: "$.field == value"
            String[] parts = expression.split("==", 2);
            if (parts.length != 2) {
                System.err.println("Warning: assertion expression must use '==' operator: " + expression);
                return false;
            }
            String path = parts[0].trim();
            String expected = parts[1].trim();
            // Remove surrounding quotes if present
            if (expected.startsWith("\"") && expected.endsWith("\"")) {
                expected = expected.substring(1, expected.length() - 1);
            }

            Object actual = JsonPath.read(jsonStr, path);
            return expected.equals(String.valueOf(actual));
        } catch (Exception e) {
            System.err.println("Assertion evaluation error: " + e.getMessage());
            return false;
        }
    }

    @SuppressWarnings("unchecked")
    private void printOutput(Map<String, Object> output) {
        if (json) {
            JsonPrinter.print(output);
        } else {
            printHumanReadable(output);
        }
    }

    private void printHumanReadable(Map<String, Object> output) {
        boolean success = (boolean) output.get("success");
        System.out.println(success ? "✅ 调用成功" : "❌ 调用失败");
        System.out.println("  耗时: " + output.get("latencyMs") + "ms");
        if (output.get("result") != null) {
            try {
                String resultJson = OBJECT_MAPPER.writerWithDefaultPrettyPrinter()
                    .writeValueAsString(output.get("result"));
                System.out.println("  结果: " + resultJson);
            } catch (Exception e) {
                System.out.println("  结果: " + output.get("result"));
            }
        }
        if (output.containsKey("assertionPassed")) {
            boolean passed = (boolean) output.get("assertionPassed");
            System.out.println("  断言: " + (passed ? "✅ 通过" : "❌ 不通过"));
        }
        if (output.get("error") != null) {
            System.out.println("  错误: " + output.get("error"));
        }
    }

    private void printError(String message) {
        System.err.println("❌ " + message);
    }

    private static class ParsedArgs {
        final String[] types;
        final Object[] values;

        ParsedArgs(String[] types, Object[] values) {
            this.types = types;
            this.values = values;
        }
    }
}
