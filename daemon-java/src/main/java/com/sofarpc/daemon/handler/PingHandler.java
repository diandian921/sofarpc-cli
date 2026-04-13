package com.sofarpc.daemon.handler;

import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.engine.PingEngine;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;

/**
 * Thin adapter from Dispatcher to {@link PingEngine}.
 *
 * @author wuwh
 */
public final class PingHandler implements OpHandler {

    private final PingEngine engine;

    public PingHandler(PingEngine engine) {
        this.engine = engine;
    }

    @Override
    public ResponseEnvelope handle(RequestEnvelope req, DaemonContext ctx) {
        return engine.run(req);
    }
}
