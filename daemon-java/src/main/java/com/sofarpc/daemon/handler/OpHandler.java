package com.sofarpc.daemon.handler;

import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;

/**
 * Handles one op. Implementations must not throw; wrap failures into ResponseEnvelope.
 *
 * @author wuwh
 */
public interface OpHandler {

    ResponseEnvelope handle(RequestEnvelope req, DaemonContext ctx);
}
