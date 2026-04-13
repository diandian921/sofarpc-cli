package com.sofarpc.cli.service;

import com.jayway.jsonpath.JsonPath;
import com.sofarpc.cli.core.JacksonHolder;

import java.util.Map;

/**
 * Evaluate assertions against RPC invocation results.
 * Supports two flavors: expression-style (invoke/call) and map-style (batch).
 *
 * @author wuwh
 */
public final class AssertionEvaluator {

    private AssertionEvaluator() {
    }

    /**
     * Evaluate an expression-style assertion (e.g. "$.status == \"ACTIVE\"").
     * Used by invoke and call commands via --assert flag.
     *
     * @param result     the RPC invocation result (already flattened)
     * @param expression assertion expression with == operator
     * @return evaluation result
     */
    public static AssertionResult evaluateExpression(Object result, String expression) {
        try {
            String jsonStr = JacksonHolder.MAPPER.writeValueAsString(result);
            String[] parts = expression.split("==", 2);
            if (parts.length != 2) {
                return new AssertionResult(false,
                    "Assertion expression must use '==' operator: " + expression);
            }
            String path = parts[0].trim();
            String expected = stripQuotes(parts[1].trim());
            Object actual = JsonPath.read(jsonStr, path);
            if (expected.equals(String.valueOf(actual))) {
                return AssertionResult.PASSED;
            }
            return new AssertionResult(false,
                "Assertion failed: " + expression + " (actual: " + actual + ")");
        } catch (Exception e) {
            return new AssertionResult(false, "Assertion evaluation error: " + e.getMessage());
        }
    }

    /**
     * Evaluate a map-style assertion (JSONPath → expected value).
     * Used by batch command via the "expect" field in test case JSON.
     *
     * @param result the RPC invocation result (already flattened)
     * @param expect map of JSONPath → expected string value
     * @return evaluation result; fails on first mismatch
     */
    public static AssertionResult evaluateExpectMap(Object result, Map<String, Object> expect) {
        if (expect == null || expect.isEmpty()) {
            return AssertionResult.PASSED;
        }
        try {
            String jsonStr = JacksonHolder.MAPPER.writeValueAsString(result);
            for (Map.Entry<String, Object> entry : expect.entrySet()) {
                String path = entry.getKey();
                String expectedStr = entry.getValue() != null
                    ? String.valueOf(entry.getValue()) : "null";
                Object actual = JsonPath.read(jsonStr, path);
                if (!expectedStr.equals(String.valueOf(actual))) {
                    return new AssertionResult(false,
                        "Assertion failed: " + path
                            + " expected " + expectedStr + " but got " + actual);
                }
            }
            return AssertionResult.PASSED;
        } catch (Exception e) {
            return new AssertionResult(false, "Assertion evaluation error: " + e.getMessage());
        }
    }

    private static String stripQuotes(String s) {
        if (s.startsWith("\"") && s.endsWith("\"") && s.length() >= 2) {
            return s.substring(1, s.length() - 1);
        }
        return s;
    }

    /**
     * Result of an assertion evaluation.
     */
    public static class AssertionResult {
        public static final AssertionResult PASSED = new AssertionResult(true, null);

        private final boolean passed;
        private final String failureMessage;

        public AssertionResult(boolean passed, String failureMessage) {
            this.passed = passed;
            this.failureMessage = failureMessage;
        }

        public boolean isPassed() {
            return passed;
        }

        public String getFailureMessage() {
            return failureMessage;
        }
    }
}
