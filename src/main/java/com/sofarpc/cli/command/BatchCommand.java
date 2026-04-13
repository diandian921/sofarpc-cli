package com.sofarpc.cli.command;

import com.sofarpc.cli.core.ExitCodes;
import com.sofarpc.cli.core.GlobalConfig;
import com.sofarpc.cli.core.JacksonHolder;
import com.sofarpc.cli.core.RpcClientFactory;
import com.sofarpc.cli.core.ServerStore;
import com.sofarpc.cli.model.BatchResult;
import com.sofarpc.cli.model.CaseResult;
import com.sofarpc.cli.service.AssertionEvaluator;
import com.sofarpc.cli.service.OutputFormatter;
import com.sofarpc.cli.service.RpcInvokeService;
import picocli.CommandLine.Command;
import picocli.CommandLine.Option;

import java.io.File;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.text.SimpleDateFormat;
import java.util.ArrayList;
import java.util.Date;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.stream.Collectors;
import java.util.stream.Stream;
import java.util.concurrent.Callable;
import java.util.concurrent.Future;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;

/**
 * Batch command: execute test cases from a directory with parallel support.
 *
 * @author wuweihua
 */
@Command(name = "batch", mixinStandardHelpOptions = true,
    description = "Run test cases from a directory in batch.")
public class BatchCommand implements Callable<Integer> {

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
    @SuppressWarnings("unchecked")
    public Integer call() {
        GlobalConfig config = GlobalConfig.getInstance();
        int effectiveTimeout = timeout != null ? timeout : config.getTimeout();
        int effectiveParallel = parallel != null ? parallel : config.getParallel();

        if (effectiveParallel <= 0) {
            OutputFormatter.printError(
                "parallel 必须为正整数，当前值: " + effectiveParallel, ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }
        if (effectiveTimeout <= 0) {
            OutputFormatter.printError(
                "timeout 必须为正整数，当前值: " + effectiveTimeout, ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }

        // Resolve server
        ServerStore store = new ServerStore();
        String address;
        try {
            address = store.resolveAddress(server);
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

        // Find case files
        if (!casesDir.isDirectory()) {
            OutputFormatter.printError(
                "用例目录不存在: " + casesDir.getPath(), ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }
        List<File> caseFileList;
        try {
            caseFileList = findJsonFiles(casesDir);
        } catch (IOException e) {
            OutputFormatter.printError(
                "扫描用例目录失败: " + e.getMessage(), ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }
        if (caseFileList.isEmpty()) {
            OutputFormatter.printError(
                "用例目录下没有 .json 文件", ExitCodes.BAD_ARGS, json);
            return ExitCodes.BAD_ARGS;
        }
        File[] caseFiles = caseFileList.toArray(new File[0]);

        // Execute cases
        String startTime = new SimpleDateFormat("yyyy-MM-dd HH:mm:ss").format(new Date());
        long batchStart = System.currentTimeMillis();

        ThreadPoolExecutor executor = new ThreadPoolExecutor(
            effectiveParallel, effectiveParallel,
            0L, TimeUnit.MILLISECONDS,
            new LinkedBlockingQueue<Runnable>(),
            new ThreadPoolExecutor.CallerRunsPolicy());

        RpcInvokeService invokeService = new RpcInvokeService();
        List<Future<CaseResult>> futures = new ArrayList<>();

        for (File caseFile : caseFiles) {
            futures.add(executor.submit(
                () -> runCase(caseFile, invokeService, address, effectiveTimeout)));
        }

        executor.shutdown();
        try {
            executor.awaitTermination(10, TimeUnit.MINUTES);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            System.err.println("Batch execution interrupted.");
        }

        // Collect results
        List<CaseResult> results = new ArrayList<>();
        int passed = 0;
        int failed = 0;
        boolean hasConnectFail = false;
        for (Future<CaseResult> future : futures) {
            try {
                CaseResult result = future.get();
                results.add(result);
                if (result.isPassed()) {
                    passed++;
                } else {
                    failed++;
                    if (result.getExitCode() == ExitCodes.CONNECT_FAIL) {
                        hasConnectFail = true;
                    }
                }
            } catch (Exception e) {
                CaseResult errorResult = new CaseResult("unknown");
                errorResult.setPassed(false);
                errorResult.setError(e.getMessage());
                errorResult.setExitCode(ExitCodes.INVOKE_FAIL);
                results.add(errorResult);
                failed++;
            }
        }

        long duration = System.currentTimeMillis() - batchStart;

        // Build output
        BatchResult output = new BatchResult();
        output.setTotal(caseFiles.length);
        output.setPassed(passed);
        output.setFailed(failed);
        output.setStartTime(startTime);
        output.setDuration(formatDuration(duration));
        output.setResults(results);

        OutputFormatter.printBatchResult(output, json);

        // Release connections used by this batch
        RpcClientFactory.destroyByAddress(address);

        if (failed > 0) {
            // Connection failures take priority over invocation failures
            return hasConnectFail ? ExitCodes.CONNECT_FAIL : ExitCodes.INVOKE_FAIL;
        }
        return ExitCodes.SUCCESS;
    }

    private List<File> findJsonFiles(File dir) throws IOException {
        try (Stream<Path> stream = Files.walk(dir.toPath())) {
            return stream
                .filter(Files::isRegularFile)
                .filter(p -> p.getFileName().toString().endsWith(".json"))
                .sorted()
                .map(Path::toFile)
                .collect(Collectors.toList());
        } catch (UncheckedIOException e) {
            throw e.getCause();
        }
    }

    @SuppressWarnings("unchecked")
    private CaseResult runCase(File caseFile, RpcInvokeService invokeService,
                               String address, int caseTimeout) {
        CaseResult caseResult = new CaseResult(caseFile.getName());

        try {
            Map<String, Object> testCase = JacksonHolder.MAPPER.readValue(
                caseFile, LinkedHashMap.class);
            String serviceId = (String) testCase.get("service");
            String methodName = (String) testCase.get("method");
            List<String> argTypesList = (List<String>) testCase.get("argTypes");
            Object argsObj = testCase.get("args");
            Map<String, Object> expect = (Map<String, Object>) testCase.get("expect");

            // Build type and arg arrays
            String[] types = argTypesList != null
                ? argTypesList.toArray(new String[0])
                : new String[0];
            Object[] args = buildArgs(argsObj, types.length);

            // Invoke
            RpcInvokeService.InvokeResult invokeResult = invokeService.invokeWithAddress(
                server, address, serviceId, methodName, types, args, caseTimeout);

            caseResult.setLatencyMs(invokeResult.getLatencyMs());

            if (!invokeResult.isSuccess()) {
                caseResult.setPassed(false);
                caseResult.setError(invokeResult.getError());
                caseResult.setExitCode(invokeResult.getExitCode());
                return caseResult;
            }

            // Check expectations
            if (expect != null && !expect.isEmpty()) {
                AssertionEvaluator.AssertionResult assertion =
                    AssertionEvaluator.evaluateExpectMap(invokeResult.getResult(), expect);
                if (!assertion.isPassed()) {
                    caseResult.setPassed(false);
                    caseResult.setError(assertion.getFailureMessage());
                    return caseResult;
                }
            }

            caseResult.setPassed(true);
        } catch (Exception e) {
            caseResult.setPassed(false);
            caseResult.setError(
                e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName());
        }
        return caseResult;
    }

    private Object[] buildArgs(Object argsObj, int typeCount) {
        if (argsObj == null) {
            if (typeCount != 0) {
                throw new IllegalArgumentException(
                    "args is empty but argTypes has " + typeCount + " types");
            }
            return new Object[0];
        }
        if (argsObj instanceof List) {
            List<?> list = (List<?>) argsObj;
            if (list.size() != typeCount) {
                throw new IllegalArgumentException(
                    "args array size (" + list.size()
                        + ") does not match argTypes count (" + typeCount + ")");
            }
            return list.toArray();
        }
        if (typeCount != 1) {
            throw new IllegalArgumentException(
                "Single object arg provided but argTypes has " + typeCount + " types");
        }
        return new Object[]{argsObj};
    }

    private String formatDuration(long ms) {
        if (ms < 1000) {
            return ms + "ms";
        }
        return String.format("%.1fs", ms / 1000.0);
    }
}
