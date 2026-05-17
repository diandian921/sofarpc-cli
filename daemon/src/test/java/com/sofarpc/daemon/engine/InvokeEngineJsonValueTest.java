package com.sofarpc.daemon.engine;

import com.fasterxml.jackson.databind.JsonNode;
import com.sofarpc.daemon.protocol.JsonCodec;
import org.junit.Test;

import java.util.Map;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

/**
 * Pins JSON numeric conversion before arguments enter SOFA Hessian.
 *
 * @author wuwh
 */
public class InvokeEngineJsonValueTest {

    @Test
    public void preservesLargeLongAsLong() throws Exception {
        JsonNode node = JsonCodec.mapper().readTree("{\"mpCode\":433905635109773312}");

        Object value = InvokeEngine.jsonValue(node);

        assertTrue(value instanceof Map);
        Object mpCode = ((Map<?, ?>) value).get("mpCode");
        assertTrue("mpCode type = " + mpCode.getClass(), mpCode instanceof Long);
        assertEquals(433905635109773312L, mpCode);
    }
}
