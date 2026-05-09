package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

import java.util.ArrayList;
import java.util.List;

/**
 * Top-level JavaFacts document — the entire output of one analyzer run.
 *
 * <p>Fields are explicitly initialised to empty collections so JSON
 * serialisation always emits {@code []} rather than omitting the field —
 * this keeps the downstream Go parser simple and predictable.
 *
 * <p>Schema version is emitted in the {@code schema} field; bump it on any
 * breaking change to the JavaFacts contract.
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({
    "schema",
    "src_roots",
    "packages",
    "classes",
    "imports",
    "parse_warnings"
})
public final class JavaFacts {

    public static final String SCHEMA_VERSION = "javafacts/v1";

    private String schema = SCHEMA_VERSION;
    private List<String> srcRoots = new ArrayList<>();
    private List<String> packages = new ArrayList<>();
    private List<JavaClass> classes = new ArrayList<>();
    private List<JavaImport> imports = new ArrayList<>();
    private List<ParseWarning> parseWarnings = new ArrayList<>();

    public String getSchema() {
        return schema;
    }

    public void setSchema(String schema) {
        this.schema = schema;
    }

    @com.fasterxml.jackson.annotation.JsonProperty("src_roots")
    public List<String> getSrcRoots() {
        return srcRoots;
    }

    public void setSrcRoots(List<String> srcRoots) {
        this.srcRoots = srcRoots;
    }

    public List<String> getPackages() {
        return packages;
    }

    public void setPackages(List<String> packages) {
        this.packages = packages;
    }

    public List<JavaClass> getClasses() {
        return classes;
    }

    public void setClasses(List<JavaClass> classes) {
        this.classes = classes;
    }

    public List<JavaImport> getImports() {
        return imports;
    }

    public void setImports(List<JavaImport> imports) {
        this.imports = imports;
    }

    @com.fasterxml.jackson.annotation.JsonProperty("parse_warnings")
    public List<ParseWarning> getParseWarnings() {
        return parseWarnings;
    }

    public void setParseWarnings(List<ParseWarning> parseWarnings) {
        this.parseWarnings = parseWarnings;
    }
}
