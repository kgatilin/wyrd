package io.archai.javaanalyzer;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.net.URISyntaxException;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * Smoke tests for the {@link Main} CLI entry — exit codes, stdout/stderr
 * routing, error paths.
 */
final class MainTest {

    private PrintStream origOut;
    private PrintStream origErr;
    private ByteArrayOutputStream out;
    private ByteArrayOutputStream err;

    @BeforeEach
    void capture() {
        origOut = System.out;
        origErr = System.err;
        out = new ByteArrayOutputStream();
        err = new ByteArrayOutputStream();
        System.setOut(new PrintStream(out, true, StandardCharsets.UTF_8));
        System.setErr(new PrintStream(err, true, StandardCharsets.UTF_8));
    }

    @AfterEach
    void restore() {
        System.setOut(origOut);
        System.setErr(origErr);
    }

    @Test
    void missingRoot_exitsOne() {
        int code = Main.run(new String[]{});
        assertEquals(1, code);
        assertTrue(err.toString(StandardCharsets.UTF_8).contains("at least one src-root"));
    }

    @Test
    void nonExistentRoot_exitsOne() {
        int code = Main.run(new String[]{"/path/that/does/not/exist/anywhere/__archai__"});
        assertEquals(1, code);
        assertTrue(err.toString(StandardCharsets.UTF_8).contains("does not exist"));
    }

    @Test
    void unknownFlag_exitsOne() {
        int code = Main.run(new String[]{"--bogus"});
        assertEquals(1, code);
        assertTrue(err.toString(StandardCharsets.UTF_8).contains("unknown flag"));
    }

    @Test
    void help_exitsZero() {
        int code = Main.run(new String[]{"--help"});
        assertEquals(0, code);
        assertTrue(out.toString(StandardCharsets.UTF_8).contains("usage:"));
    }

    @Test
    void version_exitsZero() {
        int code = Main.run(new String[]{"--version"});
        assertEquals(0, code);
        assertTrue(out.toString(StandardCharsets.UTF_8).contains("javafacts/v1"));
    }

    @Test
    void simpleClassFixture_emitsValidJson() throws URISyntaxException, IOException {
        Path src = goldenSrc("simple-class");
        int code = Main.run(new String[]{"--pretty", src.toString()});
        assertEquals(0, code);

        String json = out.toString(StandardCharsets.UTF_8);
        assertTrue(json.contains("\"schema\" : \"javafacts/v1\""));
        assertTrue(json.contains("\"fqn\" : \"com.example.Greeter\""));
        // Stdout must end with a single trailing newline so shell pipelines
        // (e.g. `> facts.json`) work cleanly.
        assertTrue(json.endsWith("\n"));
    }

    @Test
    void compactOutput_isSingleLine() throws URISyntaxException, IOException {
        Path src = goldenSrc("simple-class");
        int code = Main.run(new String[]{src.toString()});
        assertEquals(0, code);

        String json = out.toString(StandardCharsets.UTF_8).strip();
        assertEquals(-1, json.indexOf('\n'),
            "compact output must be single-line, got:\n" + json);
    }

    private Path goldenSrc(String fixture) throws URISyntaxException {
        URL url = getClass().getClassLoader().getResource("golden/" + fixture + "/src");
        assertNotNull(url, "fixture " + fixture + " missing");
        return Paths.get(url.toURI());
    }
}
