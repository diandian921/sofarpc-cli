package com.sofarpc.daemon.protocol;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.Test;

import java.io.File;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.stream.Stream;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

/**
 * Pins the wire contract across every shared fixture under protocol/fixtures. Requests
 * deserialize strictly into RequestEnvelope and responses into ResponseEnvelope — unknown
 * top-level fields fail the test, matching the Go side's DisallowUnknownFields.
 *
 * @author wuwh
 */
public class EnvelopeFixturesTest {

    private static final Path FIXTURES_DIR = locateFixturesDir();
    private final ObjectMapper mapper = JsonCodec.mapper();

    @Test
    public void everyRequestFixtureDeserializesIntoEnvelope() throws Exception {
        for (File f : collectFixtures("request")) {
            RequestEnvelope env = mapper.readValue(f, RequestEnvelope.class);
            assertNotNull("requestId null in " + f.getName(), env.getRequestId());
            assertNotNull("op null in " + f.getName(), env.getOp());
            assertNotNull("payload null in " + f.getName(), env.getPayload());
        }
    }

    @Test
    public void everyResponseFixtureMatchesEnvelopeShape() throws Exception {
        for (File f : collectFixtures("response")) {
            ResponseEnvelope env = mapper.readValue(f, ResponseEnvelope.class);
            assertNotNull("requestId null in " + f.getName(), env.getRequestId());
            assertNotNull("code null in " + f.getName(), env.getCode());
            ErrorCode.valueOf(env.getCode());
            if (env.isOk()) {
                assertEquals(ErrorCode.SUCCESS.name(), env.getCode());
            }
        }
    }

    @Test
    public void everyKnownCodeHasFixture() throws Exception {
        Set<String> seen = new HashSet<>();
        for (File f : collectFixtures("response")) {
            JsonNode node = mapper.readTree(f);
            seen.add(node.get("code").asText());
        }
        for (ErrorCode code : ErrorCode.values()) {
            assertTrue("no fixture covers code " + code, seen.contains(code.name()));
        }
    }

    private static List<File> collectFixtures(String prefix) throws IOException {
        List<File> out = new ArrayList<>();
        try (Stream<Path> stream = Files.walk(FIXTURES_DIR)) {
            stream.filter(Files::isRegularFile)
                    .filter(p -> p.getFileName().toString().startsWith(prefix + "."))
                    .filter(p -> p.getFileName().toString().endsWith(".json"))
                    .forEach(p -> out.add(p.toFile()));
        }
        assertFalse("no " + prefix + " fixtures under " + FIXTURES_DIR, out.isEmpty());
        return out;
    }

    private static Path locateFixturesDir() {
        Path cwd = Paths.get("").toAbsolutePath();
        Path candidate = cwd.resolve("../protocol/fixtures").normalize();
        if (candidate.toFile().isDirectory()) {
            return candidate;
        }
        candidate = cwd.resolve("protocol/fixtures").normalize();
        return candidate;
    }
}
