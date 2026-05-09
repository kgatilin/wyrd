package io.archai.javaanalyzer;

import io.archai.javaanalyzer.facts.JavaFacts;
import io.archai.javaanalyzer.json.Writer;

import org.junit.jupiter.api.DynamicTest;
import org.junit.jupiter.api.TestFactory;

import java.io.IOException;
import java.net.URISyntaxException;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.stream.Stream;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;

/**
 * Golden-fixture tests. For every {@code <fixture>/src/} directory under
 * {@code src/test/resources/golden/}, the analyzer is run and the output
 * compared byte-for-byte to {@code <fixture>/expected.json}.
 *
 * <p>The {@code src_roots} field is normalised to the bare {@code "src"}
 * string so the golden file is portable across machines (the absolute path
 * is otherwise machine-specific).
 *
 * <p>Run with {@code -Darchai.update-golden=true} to regenerate
 * {@code expected.json} files in place after intentional schema changes.
 */
final class AnalyzerTest {

    @TestFactory
    Stream<DynamicTest> goldenFixtures() throws IOException, URISyntaxException {
        URL goldenUrl = getClass().getClassLoader().getResource("golden");
        assertNotNull(goldenUrl, "golden resources directory must exist on classpath");
        Path goldenDir = Paths.get(goldenUrl.toURI());

        // Resolve the source-tree golden dir so update-golden writes back to
        // the working tree, not the build output dir under target/.
        // Maven puts test resources under target/test-classes/; walk up to
        // src/test/resources/golden.
        Path sourceGoldenDir = resolveSourceGoldenDir(goldenDir);

        List<Path> fixtures;
        try (var stream = Files.list(goldenDir)) {
            fixtures = stream
                .filter(Files::isDirectory)
                .sorted(Comparator.comparing(Path::getFileName))
                .toList();
        }

        boolean updateGolden = Boolean.getBoolean("archai.update-golden");

        List<DynamicTest> tests = new ArrayList<>(fixtures.size());
        for (Path fixture : fixtures) {
            Path sourceFixture = sourceGoldenDir.resolve(fixture.getFileName().toString());
            tests.add(DynamicTest.dynamicTest(fixture.getFileName().toString(), () -> {
                runFixture(fixture, sourceFixture, updateGolden);
            }));
        }
        return tests.stream();
    }

    private void runFixture(Path fixture, Path sourceFixture, boolean updateGolden) throws IOException {
        Path src = fixture.resolve("src");
        // Read goldens from the source tree so they're not stale from a
        // previous test-classes copy; write updates back to the source tree.
        Path expected = sourceFixture.resolve("expected.json");

        Analyzer analyzer = new Analyzer();
        JavaFacts facts = analyzer.analyze(List.of(src));

        // Normalise src_roots to the bare basename so the golden file is
        // portable: tests pass an absolute path, but the expected JSON
        // records only "src".
        facts.setSrcRoots(List.of(src.getFileName().toString()));

        Writer writer = new Writer();
        String actual = writer.toPrettyString(facts);

        if (updateGolden || !Files.exists(expected)) {
            Files.writeString(expected, actual);
            // First-time generation is a soft pass — re-run to confirm
            // determinism.
            return;
        }

        String expectedContent = Files.readString(expected);
        assertEquals(expectedContent, actual,
            "golden mismatch for fixture " + fixture.getFileName()
            + " — re-run with -Darchai.update-golden=true if intentional");
    }

    /**
     * Walk up from {@code target/test-classes/golden} to the
     * {@code src/test/resources/golden} working-tree directory. Falls back
     * to the input path if the heuristic doesn't match — non-Maven layouts
     * just won't get update-golden support.
     */
    private static Path resolveSourceGoldenDir(Path classpathGolden) {
        Path candidate = classpathGolden;
        // Expected layout: <project>/target/test-classes/golden
        Path target = candidate.getParent();           // test-classes
        if (target == null) return candidate;
        Path projectBuild = target.getParent();        // target
        if (projectBuild == null) return candidate;
        Path projectRoot = projectBuild.getParent();   // <project>
        if (projectRoot == null) return candidate;
        Path source = projectRoot
            .resolve("src")
            .resolve("test")
            .resolve("resources")
            .resolve("golden");
        if (Files.isDirectory(source)) {
            return source;
        }
        return candidate;
    }
}
