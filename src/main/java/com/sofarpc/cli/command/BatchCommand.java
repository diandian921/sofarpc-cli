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

import java.io.File;
import java.text.SimpleDateFormat;
import java.util.ArrayList;
import java.util.Date;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;

/**
 * Batch command: execute test cases from a directory with parallel support.
 *
 * @author wuweihua
 */
@Command(name = "batch", description = "Run test cases from a directory in batch.")
public class BatchCommand implements Runnable {

    private static final int EXIT_HAS_FAILURE = 1;
    private static final int EXIT_BAD_ARGS = 3;
    private static final int EXIT_ALIAS_NOT_FOUND = 4;

    private static final ObjectMapper OBJECT_MAPPER = new ObjectMapper();

    @Option(names = "--server", required = true, description = "Server alias.")
    private String server;

    @Option(names = "--cases", required = true, description = "Test cases directory path.")
    private File casesDir;

    @Option(names = "--parallel", description = "Number of parallel threads.")
    private Integer parallel;

    @Option(names = "--timeout", description = "Timeout in milliseconds.")
    private Integer timeout;

    @Option(names = "--json", description = "Output in JSON format.")
    private boolean json;

    @Override
    public void run() {
        GlobalConfig config = GlobalConfig.getInstance();
        int effectiveTimeout = timeout != null ? timeout : config.getTimeout();
        int effectiveParallel = parallel != null ? parallel : config.getParallel();

        // Resolve server
        ServerStore store = new ServerStore();
        String address = store.resolveAddress(server);
        if (address == null) {
            System.err.println("❌ 服务别名不存在: " + server);
            System.exit(EXIT_ALIAS_NOT_FOUND);
            return;
        }

        // Find case files
        if (!casesDir.isDirectory()) {
            System.err.println("❌ 用例目录不存在: " + casesDir.getPath());
            System.exit(EXIT_BAD_ARGS);
            return;
        }
        File[] caseFiles = casesDir.listFiles((dir, name) -> name.endsWith(".json"));
        if (caseFiles == null || caseFiles.length == 0) {
            System.err.println("❌ 用例目录下没有 .json 文件");
            System.exit(EXIT_BAD_ARGS);
            return;
        }

        // Execute cases
        String startTime = new SimpleDateFormat("yyyy-MM-dd HH:mm:ss").format(new Date());
        long batchStart = System.currentTimeMillis();

        ExecutorService executor = Executors.newFixedThreadPool(effectiveParallel);
        List<Future<Map<String, Object>>> futures = new ArrayList<>();

        for (File caseFile : caseFiles) {
            futures.add(executor.submit(new CaseRunner(caseFile, address, effectiveTimeout)));
        }

        executor.shutdown();
        try {
            executor.awaitTermination(10, TimeUnit.MINUTES);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            System.err.println("Batch execution interrupted.");
        }

        // Collect results
        List<Map<String, Object>> results = new ArrayList<>();
        int passed = 0;
        int failed = 0;
        for (Future<Map<String, Object>> future : futures) {
            try {
                Map<String, Object> result = future.get();
                results.add(result);
                if (Boolean.TRUE.equals(result.get("passed"))) {
                    passed++;
                } else {
                    failed++;
                }
            } catch (Exception e) {
                Map<String, Object> errorResult = new LinkedHashMap<>();
                errorResult.put("case", "unknown");
                errorResult.put("passed", false);
                errorResult.put("error", e.getMessage());
                results.add(errorResult);
                failed++;
            }
        }

        long duration = System.currentTimeMillis() - batchStart;

        // Build output
        Map<String, Object> output = new LinkedHashMap<>();
        output.put("total", caseFiles.length);
        output.put("passed", passed);
        output.put("failed", failed);
        output.put("startTime", startTime);
        output.put("duration", formatDuration(duration));
        output.put("results", results);

        if (json) {
            JsonPrinter.print(output);
        } else {
            printHumanReadable(output);
        }

        if (failed > 0) {
            System.exit(EXIT_HAS_FAILURE);
        }
    }

    private class CaseRunner implements Callable<Map<String, Object>> {

        private final File caseFile;
        private final String address;
        private final int timeout;

        CaseRunner(File caseFile, String address, int timeout) {
            this.caseFile = caseFile;
            this.address = address;
            this.timeout = timeout;
        }

        @Override
        @SuppressWarnings("unchecked")
        public Map<String, Object> call() {
            Map<String, Object> result = new LinkedHashMap<>();
            result.put("case", caseFile.getName());

            try {
                Map<String, Object> testCase = OBJECT_MAPPER.readValue(caseFile, LinkedHashMap.class);
                String serviceId = (String) testCase.get("service");
                String methodName = (String) testCase.get("method");
                List<String> argTypesList = (List<String>) testCase.get("argTypes");
                Object argsObj = testCase.get("args");
                Map<String, String> expect = (Map<String, String>) testCase.get("expect");

                // Build type and arg arrays
                String[] types = argTypesList != null
                    ? argTypesList.toArray(new String[0])
                    : new String[0];
                Object[] args = buildArgs(argsObj, types.length);

                // Invoke
                GenericService genericService = RpcClientFactory.getOrCreate(server, address, serviceId, timeout);
                long start = System.currentTimeMillis();
                Object rawInvokeResult = genericService.$genericInvoke(methodName, types, args);
                Object invokeResult = RpcClientFactory.flattenResult(rawInvokeResult);
                long latency = System.currentTimeMillis() - start;

                result.put("latencyMs", latency);

                // Check expectations
                if (expect != null && !expect.isEmpty()) {
                    String resultJson = OBJECT_MAPPER.writeValueAsString(invokeResult);
                    for (Map.Entry<String, String> entry : expect.entrySet()) {
                        Object actual = JsonPath.read(resultJson, entry.getKey());
                        String expectedStr = entry.getValue() != null ? entry.getValue() : "null";
                        if (!expectedStr.equals(String.valueOf(actual))) {
                            result.put("passed", false);
                            result.put("error", "Assertion failed: " + entry.getKey()
                                + " expected " + expectedStr + " but got " + actual);
                            return result;
                        }
                    }
                }

                result.put("passed", true);
            } catch (Exception e) {
                result.put("passed", false);
                result.put("error", e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName());
            }
            return result;
        }

        private Object[] buildArgs(Object argsObj, int typeCount) {
            if (argsObj == null) {
                return new Object[0];
            }
            if (argsObj instanceof List) {
                return ((List<?>) argsObj).toArray();
            }
            return new Object[]{argsObj};
        }
    }

    @SuppressWarnings("unchecked")
    private void printHumanReadable(Map<String, Object> output) {
        System.out.println("========== Batch Test Report ==========");
        System.out.println("开始时间: " + output.get("startTime"));
        System.out.println("总计: " + output.get("total") + "  通过: " + output.get("passed")
            + "  失败: " + output.get("failed") + "  耗时: " + output.get("duration"));
        System.out.println();

        List<Map<String, Object>> results = (List<Map<String, Object>>) output.get("results");
        for (Map<String, Object> r : results) {
            boolean passed = Boolean.TRUE.equals(r.get("passed"));
            String icon = passed ? "✅" : "❌";
            String line = icon + " " + r.get("case");
            if (r.containsKey("latencyMs")) {
                line += " (" + r.get("latencyMs") + "ms)";
            }
            System.out.println(line);
            if (!passed && r.containsKey("error")) {
                System.out.println("   " + r.get("error"));
            }
        }
    }

    private String formatDuration(long ms) {
        if (ms < 1000) {
            return ms + "ms";
        }
        return String.format("%.1fs", ms / 1000.0);
    }
}
