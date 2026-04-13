package com.sofarpc.cli.service;

import com.sofarpc.cli.core.JacksonHolder;
import com.sofarpc.cli.model.BatchResult;
import com.sofarpc.cli.model.CaseResult;
import com.sofarpc.cli.output.JsonPrinter;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Unified output formatting for invoke and batch results.
 * Supports both JSON and human-readable output modes.
 *
 * @author wuwh
 */
public final class OutputFormatter {

    private OutputFormatter() {
    }

    /**
     * Print a pre-invocation error (argument validation, config read failure, etc.)
     * using the same output contract as success paths:
     * - JSON mode → stable JSON schema on stdout: {success:false, exitCode, error}
     * - human mode → "❌ message" on stderr
     *
     * @param message  human readable error message (no leading icon)
     * @param exitCode the exit code about to be returned
     * @param jsonMode whether --json was specified
     */
    public static void printError(String message, int exitCode, boolean jsonMode) {
        if (jsonMode) {
            Map<String, Object> output = new LinkedHashMap<>();
            output.put("success", false);
            output.put("exitCode", exitCode);
            output.put("error", message);
            JsonPrinter.print(output);
        } else {
            System.err.println("❌ " + message);
        }
    }

    /**
     * Print invoke/call result.
     * JSON output structure: {success, latencyMs, result, assertionPassed?, error}
     *
     * @param result    the invocation result
     * @param assertion assertion result, or null if no assertion
     * @param jsonMode  true for JSON output, false for human-readable
     */
    public static void printInvokeResult(RpcInvokeService.InvokeResult result,
                                         AssertionEvaluator.AssertionResult assertion,
                                         boolean jsonMode) {
        Map<String, Object> output = buildInvokeOutput(result, assertion);
        if (jsonMode) {
            JsonPrinter.print(output);
        } else {
            printInvokeHumanReadable(output);
        }
    }

    /**
     * Print batch result.
     *
     * @param output   the typed batch result
     * @param jsonMode true for JSON output, false for human-readable
     */
    public static void printBatchResult(BatchResult output, boolean jsonMode) {
        if (jsonMode) {
            JsonPrinter.print(output);
        } else {
            printBatchHumanReadable(output);
        }
    }

    private static Map<String, Object> buildInvokeOutput(
        RpcInvokeService.InvokeResult result,
        AssertionEvaluator.AssertionResult assertion) {
        Map<String, Object> output = new LinkedHashMap<>();
        output.put("success", result.isSuccess());
        output.put("latencyMs", result.getLatencyMs());
        output.put("result", result.getResult());

        if (assertion != null) {
            output.put("assertionPassed", assertion.isPassed());
            output.put("error", assertion.isPassed() ? null : assertion.getFailureMessage());
        } else {
            output.put("error", result.getError());
        }
        return output;
    }

    private static void printInvokeHumanReadable(Map<String, Object> output) {
        boolean success = Boolean.TRUE.equals(output.get("success"));
        System.out.println(success ? "✅ 调用成功" : "❌ 调用失败");
        System.out.println("  耗时: " + output.get("latencyMs") + "ms");

        if (output.get("result") != null) {
            try {
                String resultJson = JacksonHolder.PRETTY_MAPPER
                    .writeValueAsString(output.get("result"));
                System.out.println("  结果: " + resultJson);
            } catch (Exception e) {
                System.out.println("  结果: " + output.get("result"));
            }
        }

        if (output.containsKey("assertionPassed")) {
            boolean passed = Boolean.TRUE.equals(output.get("assertionPassed"));
            System.out.println("  断言: " + (passed ? "✅ 通过" : "❌ 不通过"));
        }

        if (output.get("error") != null) {
            System.out.println("  错误: " + output.get("error"));
        }
    }

    private static void printBatchHumanReadable(BatchResult output) {
        System.out.println("========== Batch Test Report ==========");
        System.out.println("开始时间: " + output.getStartTime());
        System.out.println("总计: " + output.getTotal() + "  通过: " + output.getPassed()
            + "  失败: " + output.getFailed() + "  耗时: " + output.getDuration());
        System.out.println();

        for (CaseResult r : output.getResults()) {
            String icon = r.isPassed() ? "✅" : "❌";
            String line = icon + " " + r.getCaseName();
            if (r.getLatencyMs() != null) {
                line += " (" + r.getLatencyMs() + "ms)";
            }
            System.out.println(line);
            if (!r.isPassed() && r.getError() != null) {
                System.out.println("   " + r.getError());
            }
        }
    }
}
