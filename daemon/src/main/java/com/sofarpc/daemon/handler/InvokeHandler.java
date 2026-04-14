package com.sofarpc.daemon.handler;

import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.engine.InvokeEngine;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;

/**
 * Thin adapter from Dispatcher to {@link InvokeEngine}.
 *
 * @author wuwh
 */
public final class InvokeHandler implements OpHandler {

    private final InvokeEngine engine;

    public InvokeHandler(InvokeEngine engine) {
        this.engine = engine;
    }

    @Override
    public ResponseEnvelope handle(RequestEnvelope req, DaemonContext ctx) {
        return engine.run(req);
    }
}
