package io.archai.javaanalyzer.json;

import com.fasterxml.jackson.core.JsonGenerator;
import com.fasterxml.jackson.core.util.DefaultIndenter;
import com.fasterxml.jackson.core.util.DefaultPrettyPrinter;
import com.fasterxml.jackson.databind.MapperFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.ObjectWriter;
import com.fasterxml.jackson.databind.SerializationFeature;

import io.archai.javaanalyzer.facts.JavaFacts;

import java.io.IOException;
import java.io.OutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;

/**
 * Serializes {@link JavaFacts} to deterministic JSON.
 *
 * <p>Two modes:
 * <ul>
 *   <li><b>compact</b> (default): single-line minified output. One trailing
 *       newline so shell pipelines aren't surprised.</li>
 *   <li><b>pretty</b> ({@code --pretty}): two-space indent, system-default
 *       line separator coerced to {@code "\n"} so output is byte-for-byte
 *       reproducible across platforms — important for golden tests on CI.</li>
 * </ul>
 *
 * <p>Field ordering is fixed via {@code @JsonPropertyOrder} on the data
 * classes; the writer just respects that. Empty collections always emit
 * as {@code []} ({@code @JsonInclude.Include.ALWAYS}).
 */
public final class Writer {

    private final ObjectMapper mapper;

    public Writer() {
        this.mapper = new ObjectMapper();
        // Sort map entries alphabetically when we serialise maps. Our domain
        // model uses lists with explicit ordering, but this guards against
        // any future map fields slipping in.
        mapper.configure(MapperFeature.SORT_PROPERTIES_ALPHABETICALLY, false);
        mapper.configure(SerializationFeature.ORDER_MAP_ENTRIES_BY_KEYS, true);
        // No timestamps anywhere — nothing in JavaFacts is a date, but be
        // explicit so future additions don't accidentally introduce
        // non-determinism.
        mapper.disable(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS);
        // Don't auto-close the target stream when the generator closes —
        // we control stdout/test buffers ourselves and want trailing
        // bytes (newline) to land before close.
        mapper.disable(SerializationFeature.CLOSE_CLOSEABLE);
        mapper.getFactory().disable(JsonGenerator.Feature.AUTO_CLOSE_TARGET);
    }

    /**
     * Write {@code facts} to {@code out} in compact (minified) form.
     */
    public void writeCompact(JavaFacts facts, OutputStream out) throws IOException {
        ObjectWriter writer = mapper.writer();
        writer.writeValue(out, facts);
        out.write('\n');
        out.flush();
    }

    /**
     * Write {@code facts} to {@code out} in pretty form (2-space indent,
     * {@code \n} line separator).
     */
    public void writePretty(JavaFacts facts, OutputStream out) throws IOException {
        DefaultPrettyPrinter pp = new DefaultPrettyPrinter();
        DefaultIndenter indenter = new DefaultIndenter("  ", "\n");
        pp.indentArraysWith(indenter);
        pp.indentObjectsWith(indenter);

        try (JsonGenerator gen = mapper.getFactory().createGenerator(out)) {
            gen.setPrettyPrinter(pp);
            mapper.writeValue(gen, facts);
        }
        out.write('\n');
        out.flush();
    }

    /**
     * Convenience for tests: serialise to a UTF-8 string in pretty form.
     */
    public String toPrettyString(JavaFacts facts) throws IOException {
        java.io.ByteArrayOutputStream buf = new java.io.ByteArrayOutputStream();
        writePretty(facts, buf);
        return buf.toString(StandardCharsets.UTF_8);
    }

    /**
     * Convenience for tests: serialise to a UTF-8 string in compact form.
     */
    public String toCompactString(JavaFacts facts) throws IOException {
        java.io.ByteArrayOutputStream buf = new java.io.ByteArrayOutputStream();
        writeCompact(facts, buf);
        return buf.toString(StandardCharsets.UTF_8);
    }

    /** Wrap a {@link PrintStream} (e.g. {@code System.out}) for writers that
     * accept {@link OutputStream}. */
    public static OutputStream wrap(PrintStream stream) {
        return stream;
    }
}
