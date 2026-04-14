package com.sofarpc.daemon.handler;

import com.fasterxml.jackson.databind.JsonNode;
import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Accepts a shutdown request, records the requested grace window, and signals the main
 * thread via {@link com.sofarpc.daemon.DaemonLifecycle}. Actual stop happens after the
 * response has been flushed back to the client.
 *
 * @author wuwh
 */
public final class ShutdownHandler implements OpHandler {

    private static final long DEFAULT_GRACE_MS = 0L;

    @Override
    public ResponseEnvelope handle(RequestEnvelope req, DaemonContext ctx) {
        long graceMs = DEFAULT_GRACE_MS;
        JsonNode payload = req.getPayload();
        if (payload != null && payload.isObject() && payload.hasNonNull("graceMs")) {
            long requested = payload.get("graceMs").asLong(DEFAULT_GRACE_MS);
            if (requested < 0L) {
                requested = 0L;
            }
            graceMs = requested;
        }
        Map<String, Object> data = new LinkedHashMap<>();
        data.put("accepted", true);
        data.put("graceMs", graceMs);
        ctx.getLifecycle().requestShutdown(graceMs);
        return ResponseEnvelope.success(req.getRequestId(), data);
    }
}
