package com.sofarpc.daemon.server;

import org.junit.Test;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.fail;

/**
 * @author wuwh
 */
public class FramingTest {

    @Test
    public void writeThenReadRoundTrip() throws IOException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        byte[] payload = "{\"op\":\"ping\"}".getBytes(StandardCharsets.UTF_8);
        Framing.writeFrame(out, payload);

        DataInputStream in = new DataInputStream(new ByteArrayInputStream(out.toByteArray()));
        byte[] decoded = Framing.readFrame(in);

        assertArrayEquals(payload, decoded);
    }

    @Test
    public void readReturnsNullOnEof() throws IOException {
        DataInputStream in = new DataInputStream(new ByteArrayInputStream(new byte[0]));
        assertNull(Framing.readFrame(in));
    }

    @Test
    public void readRejectsOversizedFrame() {
        byte[] header = new byte[]{0x7F, (byte) 0xFF, (byte) 0xFF, (byte) 0xFF};
        DataInputStream in = new DataInputStream(new ByteArrayInputStream(header));
        try {
            Framing.readFrame(in);
            fail("should have thrown");
        } catch (IOException e) {
            assertEquals(true, e.getMessage().contains("invalid frame length"));
        }
    }
}
