package com.sofarpc.daemon.handler;

import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Returns daemon runtime info. Used by the launcher as the ready probe after spawn.
 *
 * @author wuwh
 */
public final class HealthHandler implements OpHandler {

    @Override
    public ResponseEnvelope handle(RequestEnvelope req, DaemonContext ctx) {
        Map<String, Object> data = new LinkedHashMap<>();
        data.put("pid", ctx.getPid());
        data.put("buildVersion", ctx.getBuildVersion());
        data.put("startedAtMs", ctx.getStartedAtMs());
        data.put("port", ctx.getPort());
        data.put("connections", ctx.getLiveConnections());
        data.put("cachedRpcTargets", ctx.getRpcGateway().getConnectionManager().size());
        return ResponseEnvelope.success(req.getRequestId(), data);
    }
}
