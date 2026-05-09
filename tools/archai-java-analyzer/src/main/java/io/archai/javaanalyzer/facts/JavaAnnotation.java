package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

import java.util.ArrayList;
import java.util.List;

/**
 * An annotation attached to a class/field/method/parameter.
 *
 * <p>{@link #fqn} is the resolved fully-qualified name when the symbol
 * solver succeeds; otherwise it's the textual form as written. {@link #args}
 * are the raw textual values of annotation members (no type resolution) —
 * sufficient for downstream pattern-matching like {@code @Path("/users")}.
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({"fqn", "args"})
public final class JavaAnnotation {

    private String fqn = "";
    private List<String> args = new ArrayList<>();

    public String getFqn() { return fqn; }
    public void setFqn(String fqn) { this.fqn = fqn; }

    public List<String> getArgs() { return args; }
    public void setArgs(List<String> args) { this.args = args; }
}
