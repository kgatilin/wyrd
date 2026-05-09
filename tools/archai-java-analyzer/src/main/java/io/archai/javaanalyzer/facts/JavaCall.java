package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/**
 * A static call edge — one method invocation captured inside another method's
 * body.
 *
 * <p>Resolution is best-effort and same-source-only (v1): JavaParser's symbol
 * solver attempts to bind the receiver to a class in the analyzed source set.
 * On success, {@link #targetFqn} carries the resolved owner FQN and
 * {@link #external} is {@code false}; the {@link #unresolved} block is empty.
 *
 * <p>On failure (call into stdlib, third-party, or unresolved {@code this}),
 * {@link #targetFqn} stays empty, {@link #external} is set to {@code true}, and
 * {@link #unresolved} captures the textual receiver scope plus the called
 * method name so downstream consumers can pattern-match. The legacy
 * {@link #toClass}/{@link #toMethod}/{@link #isStatic()} fields are preserved
 * for backward compatibility — they always reflect the textual form.
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({"to_class", "to_method", "static", "external", "target_fqn", "unresolved"})
public final class JavaCall {

    private String toClass = "";
    private String toMethod = "";
    private boolean isStatic;
    private boolean external;
    private String targetFqn = "";
    private JavaCallUnresolved unresolved = new JavaCallUnresolved();

    @JsonProperty("to_class")
    public String getToClass() { return toClass; }
    public void setToClass(String toClass) { this.toClass = toClass; }

    @JsonProperty("to_method")
    public String getToMethod() { return toMethod; }
    public void setToMethod(String toMethod) { this.toMethod = toMethod; }

    @JsonProperty("static")
    public boolean isStatic() { return isStatic; }
    public void setStatic(boolean aStatic) { isStatic = aStatic; }

    public boolean isExternal() { return external; }
    public void setExternal(boolean external) { this.external = external; }

    @JsonProperty("target_fqn")
    public String getTargetFqn() { return targetFqn; }
    public void setTargetFqn(String targetFqn) { this.targetFqn = targetFqn; }

    public JavaCallUnresolved getUnresolved() { return unresolved; }
    public void setUnresolved(JavaCallUnresolved unresolved) { this.unresolved = unresolved; }
}
