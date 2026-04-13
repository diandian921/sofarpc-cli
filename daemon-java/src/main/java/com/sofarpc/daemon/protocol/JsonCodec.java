package com.sofarpc.daemon.protocol;

import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;

/**
 * Shared Jackson ObjectMapper for all envelope encoding/decoding.
 *
 * @author wuwh
 */
public final class JsonCodec {

    private static final ObjectMapper MAPPER = new ObjectMapper()
            .configure(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, true)
            .configure(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS, true);

    private JsonCodec() {
    }

    public static ObjectMapper mapper() {
        return MAPPER;
    }
}
