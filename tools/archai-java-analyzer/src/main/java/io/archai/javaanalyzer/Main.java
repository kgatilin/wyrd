package io.archai.javaanalyzer;

import io.archai.javaanalyzer.facts.JavaFacts;
import io.archai.javaanalyzer.json.Writer;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;

/**
 * CLI entry — {@code java -jar archai-java-analyzer.jar [flags] <src-root>...}
 *
 * <p>Stdout: JavaFacts JSON. Stderr: progress / errors. Exit 0 on success,
 * 1 on hard failure. Single unparseable files surface as
 * {@code parse_warnings} entries inside the JSON, not as fatal errors.
 *
 * <p>Flags:
 * <ul>
 *   <li>{@code --pretty} — pretty-print JSON output (default: minified)</li>
 *   <li>{@code --include-private} / {@code --no-include-private} — include
 *       private members (default: include)</li>
 *   <li>{@code --help} / {@code -h} — usage</li>
 *   <li>{@code --version} — schema and analyzer version</li>
 * </ul>
 */
public final class Main {

    private Main() {}

    public static void main(String[] args) {
        int exit = run(args);
        System.exit(exit);
    }

    /**
     * Run the CLI and return the intended exit code. Split out from {@code
     * main} so tests can drive it without {@code System.exit} unwinding the
     * JVM.
     */
    public static int run(String[] args) {
        boolean pretty = false;
        boolean includePrivate = true;
        List<Path> roots = new ArrayList<>();

        for (int i = 0; i < args.length; i++) {
            String a = args[i];
            switch (a) {
                case "--pretty" -> pretty = true;
                case "--include-private" -> includePrivate = true;
                case "--no-include-private" -> includePrivate = false;
                case "-h", "--help" -> {
                    printUsage(System.out);
                    return 0;
                }
                case "--version" -> {
                    System.out.println("archai-java-analyzer schema=" + JavaFacts.SCHEMA_VERSION);
                    return 0;
                }
                default -> {
                    if (a.startsWith("-")) {
                        System.err.println("unknown flag: " + a);
                        printUsage(System.err);
                        return 1;
                    }
                    roots.add(Paths.get(a));
                }
            }
        }

        if (roots.isEmpty()) {
            System.err.println("error: at least one src-root required");
            printUsage(System.err);
            return 1;
        }

        // Hard-fail check: every root must exist. A missing root is a CLI
        // misuse, not a parse warning.
        for (Path r : roots) {
            if (!Files.exists(r)) {
                System.err.println("error: src-root does not exist: " + r);
                return 1;
            }
        }

        Analyzer analyzer = new Analyzer(includePrivate);
        JavaFacts facts;
        try {
            facts = analyzer.analyze(roots);
        } catch (IOException e) {
            System.err.println("error: " + e.getMessage());
            return 1;
        }

        // Hard-fail: not a single file parsed and we have no class output.
        // Distinguishes "empty source tree" from "tree of unparseable files":
        // if every file produced a parse_warning AND no class was extracted,
        // exit 1 — the consumer would otherwise see an empty document and
        // assume success.
        if (facts.getClasses().isEmpty() && !facts.getParseWarnings().isEmpty()) {
            System.err.println("error: no parsable Java source found across "
                + roots.size() + " root(s); " + facts.getParseWarnings().size()
                + " warning(s)");
            for (var w : facts.getParseWarnings()) {
                System.err.println("  " + w.getFile() + ": " + w.getMessage());
            }
            return 1;
        }

        Writer writer = new Writer();
        try {
            if (pretty) {
                writer.writePretty(facts, System.out);
            } else {
                writer.writeCompact(facts, System.out);
            }
        } catch (IOException e) {
            System.err.println("error: writing JSON: " + e.getMessage());
            return 1;
        }
        return 0;
    }

    private static void printUsage(java.io.PrintStream out) {
        out.println("usage: java -jar archai-java-analyzer.jar [flags] <src-root>...");
        out.println();
        out.println("flags:");
        out.println("  --pretty                pretty-print JSON (default: minified)");
        out.println("  --include-private       include private members (default)");
        out.println("  --no-include-private    skip private members");
        out.println("  --version               print schema version");
        out.println("  -h, --help              show this help");
        out.println();
        out.println("Output: JavaFacts JSON on stdout. Errors on stderr. Exit 0 on success, 1 on failure.");
    }
}
