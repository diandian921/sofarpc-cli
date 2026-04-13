package com.sofarpc.daemon.handler;

import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.protocol.ErrorCode;
import com.sofarpc.daemon.protocol.Op;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;

import java.util.EnumMap;
import java.util.Map;

/**
 * Routes a request to the handler registered for its op. No business logic lives here.
 *
 * @author wuwh
 */
public final class Dispatcher {

    private final Map<Op, OpHandler> handlers = new EnumMap<>(Op.class);

    public Dispatcher register(Op op, OpHandler handler) {
        if (op == null || handler == null) {
            throw new IllegalArgumentException("op and handler must not be null");
        }
        handlers.put(op, handler);
        return this;
    }

    public ResponseEnvelope dispatch(RequestEnvelope req, DaemonContext ctx) {
        if (req == null || req.getRequestId() == null || req.getRequestId().isEmpty()) {
            return ResponseEnvelope.failure(
                    req == null ? null : req.getRequestId(),
                    ErrorCode.BAD_REQUEST,
                    "requestId is required");
        }
        Op op = Op.fromWire(req.getOp());
        if (op == null) {
            return ResponseEnvelope.failure(
                    req.getRequestId(),
                    ErrorCode.BAD_REQUEST,
                    "unknown op: " + req.getOp()
                            + " (expected one of: " + Op.allowedWireValues() + "; op is case-sensitive)");
        }
        OpHandler handler = handlers.get(op);
        if (handler == null) {
            return ResponseEnvelope.failure(
                    req.getRequestId(),
                    ErrorCode.BAD_REQUEST,
                    "no handler registered for op: " + op.wire());
        }
        return handler.handle(req, ctx);
    }
}
