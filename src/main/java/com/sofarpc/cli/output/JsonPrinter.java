package com.sofarpc.cli.output;

import com.sofarpc.cli.core.JacksonHolder;

/**
 * Utility for printing objects as formatted JSON to stdout.
 *
 * @author wuweihua
 */
public class JsonPrinter {

    private JsonPrinter() {
    }

    public static void print(Object obj) {
        try {
            System.out.println(JacksonHolder.PRETTY_MAPPER.writeValueAsString(obj));
        } catch (Exception e) {
            System.err.println("JSON serialization error: " + e.getMessage());
        }
    }

    public static String toString(Object obj) {
        try {
            return JacksonHolder.PRETTY_MAPPER.writeValueAsString(obj);
        } catch (Exception e) {
            return obj != null ? obj.toString() : "null";
        }
    }
}
