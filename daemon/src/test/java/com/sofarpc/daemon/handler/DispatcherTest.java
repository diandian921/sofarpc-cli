package com.sofarpc.daemon.handler;

import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.protocol.ErrorCode;
import com.sofarpc.daemon.protocol.Op;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;
import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

/**
 * @author wuwh
 */
public class DispatcherTest {

    @Test
    public void routesKnownOpToRegisteredHandler() {
        Dispatcher dispatcher = new Dispatcher()
                .register(Op.HEALTH, (req, ctx) -> ResponseEnvelope.success(req.getRequestId(), "pong"));

        RequestEnvelope req = new RequestEnvelope();
        req.setRequestId("r1");
        req.setOp("health");

        ResponseEnvelope resp = dispatcher.dispatch(req, (DaemonContext) null);

        assertTrue(resp.isOk());
        assertEquals("SUCCESS", resp.getCode());
        assertEquals("pong", resp.getData());
    }

    @Test
    public void unknownOpReturnsBadRequest() {
        Dispatcher dispatcher = new Dispatcher();

        RequestEnvelope req = new RequestEnvelope();
        req.setRequestId("r1");
        req.setOp("frobnicate");

        ResponseEnvelope resp = dispatcher.dispatch(req, (DaemonContext) null);

        assertFalse(resp.isOk());
        assertEquals(ErrorCode.BAD_REQUEST.name(), resp.getCode());
    }

    @Test
    public void missingRequestIdReturnsBadRequest() {
        Dispatcher dispatcher = new Dispatcher();

        RequestEnvelope req = new RequestEnvelope();
        req.setOp("health");

        ResponseEnvelope resp = dispatcher.dispatch(req, (DaemonContext) null);

        assertFalse(resp.isOk());
        assertEquals(ErrorCode.BAD_REQUEST.name(), resp.getCode());
    }

    @Test
    public void unknownOpErrorListsAllowedValuesAndFlagsCaseSensitivity() {
        Dispatcher dispatcher = new Dispatcher();

        RequestEnvelope req = new RequestEnvelope();
        req.setRequestId("r1");
        req.setOp("HEALTH");

        ResponseEnvelope resp = dispatcher.dispatch(req, (DaemonContext) null);

        assertFalse(resp.isOk());
        assertEquals(ErrorCode.BAD_REQUEST.name(), resp.getCode());
        assertNotNull(resp.getError());
        String msg = resp.getError().getMessage();
        assertTrue("message should echo the offending op, got: " + msg, msg.contains("HEALTH"));
        assertTrue("message should list invoke, got: " + msg, msg.contains("invoke"));
        assertTrue("message should list ping, got: " + msg, msg.contains("ping"));
        assertTrue("message should list health, got: " + msg, msg.contains("health"));
        assertTrue("message should list shutdown, got: " + msg, msg.contains("shutdown"));
        assertTrue("message should call out case sensitivity, got: " + msg, msg.contains("case-sensitive"));
    }

    @Test
    public void unregisteredKnownOpReturnsBadRequest() {
        Dispatcher dispatcher = new Dispatcher();

        RequestEnvelope req = new RequestEnvelope();
        req.setRequestId("r1");
        req.setOp("invoke");

        ResponseEnvelope resp = dispatcher.dispatch(req, (DaemonContext) null);

        assertFalse(resp.isOk());
        assertEquals(ErrorCode.BAD_REQUEST.name(), resp.getCode());
    }
}
