package com.sofarpc.daemon.state;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.sofarpc.daemon.protocol.JsonCodec;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Writes and deletes {@code state.json}. The daemon only writes once both port bind and
 * initial warmup succeed, so a readable state file implies the daemon is accepting
 * connections. Writes are atomic via tmp+rename.
 *
 * @author wuwh
 */
public final class StateFile {

    private static final Logger LOG = LoggerFactory.getLogger(StateFile.class);

    public static final String STATUS_READY = "ready";

    private final Path path;
    private final ObjectMapper mapper = JsonCodec.mapper();

    public StateFile(Path path) {
        this.path = path;
    }

    public void writeReady(long pid, int port, String buildVersion, long startedAtMs) throws IOException {
        Map<String, Object> data = new LinkedHashMap<>();
        data.put("pid", pid);
        data.put("port", port);
        data.put("buildVersion", buildVersion);
        data.put("startedAtMs", startedAtMs);
        data.put("status", STATUS_READY);
        Files.createDirectories(path.getParent());
        Path tmp = path.resolveSibling(path.getFileName().toString() + ".tmp");
        Files.write(tmp, mapper.writeValueAsBytes(data));
        Files.move(tmp, path, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE);
        LOG.info("wrote state.json at {}", path);
    }

    public void deleteQuietly() {
        try {
            Files.deleteIfExists(path);
        } catch (IOException e) {
            LOG.debug("could not delete {}: {}", path, e.getMessage());
        }
    }

    public Path getPath() {
        return path;
    }
}
