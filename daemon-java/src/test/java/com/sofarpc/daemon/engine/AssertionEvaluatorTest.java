package com.sofarpc.daemon.engine;

import org.junit.Test;

import java.util.Arrays;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * @author wuwh
 */
public class AssertionEvaluatorTest {

    @Test
    public void equalsAssertionPasses() {
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("status", "ACTIVE");

        List<Map<String, Object>> assertions = Arrays.asList(singleton("path", "$.status", "equals", "ACTIVE"));

        AssertionEvaluator.Result r = AssertionEvaluator.evaluate(result, assertions);

        assertEquals(0, r.getFailedCount());
        assertTrue(Boolean.TRUE.equals(r.getOutcomes().get(0).get("passed")));
    }

    @Test
    public void equalsAssertionFails() {
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("status", "INACTIVE");

        List<Map<String, Object>> assertions = Arrays.asList(singleton("path", "$.status", "equals", "ACTIVE"));

        AssertionEvaluator.Result r = AssertionEvaluator.evaluate(result, assertions);

        assertEquals(1, r.getFailedCount());
        assertFalse((Boolean) r.getOutcomes().get(0).get("passed"));
    }

    @Test
    public void existsAssertionAgainstMissingFieldFails() {
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("status", "ACTIVE");

        Map<String, Object> assertion = new HashMap<>();
        assertion.put("path", "$.name");
        assertion.put("exists", true);

        AssertionEvaluator.Result r = AssertionEvaluator.evaluate(result, Arrays.asList(assertion));

        assertEquals(1, r.getFailedCount());
    }

    private static Map<String, Object> singleton(String k1, Object v1, String k2, Object v2) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put(k1, v1);
        m.put(k2, v2);
        return m;
    }
}
