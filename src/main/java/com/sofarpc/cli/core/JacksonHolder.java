package com.sofarpc.cli.core;

import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;

/**
 * Shared ObjectMapper instances for the entire CLI.
 *
 * @author wuwh
 */
public final class JacksonHolder {

    // Standard mapper: BigDecimal for floats, no pretty print
    public static final ObjectMapper MAPPER = new ObjectMapper()
        .configure(DeserializationFeature.USE_BIG_DECIMAL_FOR_FLOATS, true);

    // Pretty-printing mapper for human-readable JSON output
    public static final ObjectMapper PRETTY_MAPPER = new ObjectMapper()
        .configure(DeserializationFeature.USE_BIG_DECIMAL_FOR_FLOATS, true)
        .configure(SerializationFeature.INDENT_OUTPUT, true);

    private JacksonHolder() {
    }
}
