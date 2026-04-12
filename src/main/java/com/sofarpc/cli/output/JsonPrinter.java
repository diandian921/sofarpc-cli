package com.sofarpc.cli.output;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;

/**
 * Utility for printing objects as formatted JSON to stdout.
 *
 * @author wuweihua
 */
public class JsonPrinter {

    private static final ObjectMapper MAPPER = new ObjectMapper()
        .enable(SerializationFeature.INDENT_OUTPUT);

    private JsonPrinter() {
    }

    public static void print(Object obj) {
        try {
            System.out.println(MAPPER.writeValueAsString(obj));
        } catch (Exception e) {
            System.err.println("JSON serialization error: " + e.getMessage());
        }
    }

    public static String toString(Object obj) {
        try {
            return MAPPER.writeValueAsString(obj);
        } catch (Exception e) {
            return obj != null ? obj.toString() : "null";
        }
    }
}
