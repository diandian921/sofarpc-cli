package com.sofarpc.daemon.engine;

import com.jayway.jsonpath.Configuration;
import com.jayway.jsonpath.DocumentContext;
import com.jayway.jsonpath.JsonPath;
import com.jayway.jsonpath.Option;
import com.jayway.jsonpath.PathNotFoundException;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;

/**
 * Evaluates assertion descriptors against a flattened RPC result.
 *
 * @author wuwh
 */
public final class AssertionEvaluator {

    private static final Configuration LENIENT_CONFIG = Configuration.defaultConfiguration()
            .addOptions(Option.SUPPRESS_EXCEPTIONS, Option.DEFAULT_PATH_LEAF_TO_NULL);

    private AssertionEvaluator() {
    }

    public static Result evaluate(Object flattened, List<Map<String, Object>> assertions) {
        List<Map<String, Object>> outcomes = new ArrayList<>();
        int failed = 0;
        if (assertions == null || assertions.isEmpty()) {
            return new Result(outcomes, 0);
        }
        DocumentContext ctx = JsonPath.using(LENIENT_CONFIG).parse(flattened);
        for (Map<String, Object> assertion : assertions) {
            Map<String, Object> outcome = evaluateOne(ctx, assertion);
            outcomes.add(outcome);
            if (Boolean.FALSE.equals(outcome.get("passed"))) {
                failed++;
            }
        }
        return new Result(outcomes, failed);
    }

    private static Map<String, Object> evaluateOne(DocumentContext ctx, Map<String, Object> assertion) {
        String path = (String) assertion.get("path");
        Map<String, Object> outcome = new LinkedHashMap<>();
        outcome.put("path", path);
        if (path == null || path.isEmpty()) {
            outcome.put("passed", false);
            outcome.put("message", "assertion path is required");
            return outcome;
        }
        Object actual;
        try {
            actual = ctx.read(path);
        } catch (PathNotFoundException e) {
            actual = null;
        }
        if (assertion.containsKey("equals")) {
            Object expected = assertion.get("equals");
            boolean passed = Objects.equals(expected, actual);
            outcome.put("passed", passed);
            outcome.put("expected", expected);
            outcome.put("actual", actual);
            if (!passed) {
                outcome.put("message", "expected " + expected + " but got " + actual);
            }
            return outcome;
        }
        if (assertion.containsKey("exists")) {
            boolean want = Boolean.TRUE.equals(assertion.get("exists"));
            boolean actualExists = actual != null;
            boolean passed = want == actualExists;
            outcome.put("passed", passed);
            if (!passed) {
                outcome.put("message", "exists=" + want + " but value " + (actualExists ? "present" : "absent"));
            }
            return outcome;
        }
        outcome.put("passed", false);
        outcome.put("message", "assertion must specify equals or exists");
        return outcome;
    }

    public static final class Result {
        private final List<Map<String, Object>> outcomes;
        private final int failedCount;

        Result(List<Map<String, Object>> outcomes, int failedCount) {
            this.outcomes = outcomes;
            this.failedCount = failedCount;
        }

        public List<Map<String, Object>> getOutcomes() {
            return outcomes;
        }

        public int getFailedCount() {
            return failedCount;
        }

        public int getTotal() {
            return outcomes.size();
        }
    }
}
