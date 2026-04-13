package com.sofarpc.daemon.server;

import java.io.DataInputStream;
import java.io.EOFException;
import java.io.IOException;
import java.io.OutputStream;

/**
 * 4-byte big-endian length-prefixed frame reader/writer.
 *
 * @author wuwh
 */
public final class Framing {

    /**
     * Hard upper bound on a single frame. Protects against OOM from a malformed length prefix.
     */
    public static final int MAX_FRAME_BYTES = 32 * 1024 * 1024;

    private Framing() {
    }

    public static byte[] readFrame(DataInputStream in) throws IOException {
        int length;
        try {
            length = in.readInt();
        } catch (EOFException e) {
            return null;
        }
        if (length < 0 || length > MAX_FRAME_BYTES) {
            throw new IOException("invalid frame length: " + length);
        }
        byte[] buf = new byte[length];
        in.readFully(buf);
        return buf;
    }

    public static void writeFrame(OutputStream out, byte[] payload) throws IOException {
        int length = payload.length;
        byte[] header = new byte[]{
                (byte) ((length >>> 24) & 0xFF),
                (byte) ((length >>> 16) & 0xFF),
                (byte) ((length >>> 8) & 0xFF),
                (byte) (length & 0xFF)
        };
        out.write(header);
        out.write(payload);
        out.flush();
    }
}
