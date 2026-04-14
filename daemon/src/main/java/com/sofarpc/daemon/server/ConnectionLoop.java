package com.sofarpc.daemon.server;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.sofarpc.daemon.DaemonContext;
import com.sofarpc.daemon.handler.Dispatcher;
import com.sofarpc.daemon.protocol.ErrorCode;
import com.sofarpc.daemon.protocol.JsonCodec;
import com.sofarpc.daemon.protocol.RequestEnvelope;
import com.sofarpc.daemon.protocol.ResponseEnvelope;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.DataInputStream;
import java.io.IOException;
import java.io.OutputStream;
import java.net.Socket;
import java.nio.charset.StandardCharsets;

/**
 * Reads length-prefixed JSON requests from one TCP connection and writes envelope responses back.
 *
 * @author wuwh
 */
public final class ConnectionLoop implements Runnable {

    private static final Logger LOG = LoggerFactory.getLogger(ConnectionLoop.class);

    private final Socket socket;
    private final Dispatcher dispatcher;
    private final DaemonContext ctx;
    private final ObjectMapper mapper;

    public ConnectionLoop(Socket socket, Dispatcher dispatcher, DaemonContext ctx) {
        this.socket = socket;
        this.dispatcher = dispatcher;
        this.ctx = ctx;
        this.mapper = JsonCodec.mapper();
    }

    @Override
    public void run() {
        ctx.connectionOpened();
        try (Socket s = socket;
             DataInputStream in = new DataInputStream(new BufferedInputStream(s.getInputStream()));
             OutputStream out = new BufferedOutputStream(s.getOutputStream())) {
            while (!Thread.currentThread().isInterrupted()) {
                byte[] frame = Framing.readFrame(in);
                if (frame == null) {
                    return;
                }
                ctx.markActivity();
                ResponseEnvelope resp = handleOne(frame);
                writeResponse(out, resp);
            }
        } catch (IOException e) {
            LOG.debug("connection closed: {}", e.getMessage());
        } finally {
            ctx.connectionClosed();
        }
    }

    private ResponseEnvelope handleOne(byte[] frame) {
        RequestEnvelope req;
        try {
            req = mapper.readValue(frame, RequestEnvelope.class);
        } catch (IOException parseEx) {
            LOG.warn("malformed request frame", parseEx);
            return ResponseEnvelope.failure(null, ErrorCode.BAD_REQUEST, "malformed JSON: " + parseEx.getMessage());
        }
        try {
            return dispatcher.dispatch(req, ctx);
        } catch (RuntimeException dispatchEx) {
            LOG.error("dispatch failed for {}", req, dispatchEx);
            return ResponseEnvelope.failure(req.getRequestId(), ErrorCode.INTERNAL_ERROR,
                    dispatchEx.getClass().getSimpleName() + ": " + dispatchEx.getMessage());
        }
    }

    private void writeResponse(OutputStream out, ResponseEnvelope resp) throws IOException {
        byte[] body = mapper.writeValueAsString(resp).getBytes(StandardCharsets.UTF_8);
        Framing.writeFrame(out, body);
    }
}
