package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/**
 * A static call edge — one method invocation captured inside another method's
 * body.
 *
 * <p>Resolution is best-effort: when JavaParser's symbol solver cannot resolve
 * the receiver type, {@link #toClass} falls back to the textual form
 * ("Receiver" or empty for unqualified calls) and {@link #external} is set.
 * Downstream consumers (Go translator) decide what to do with unresolved
 * edges.
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({"to_class", "to_method", "static", "external"})
public final class JavaCall {

    private String toClass = "";
    private String toMethod = "";
    private boolean isStatic;
    private boolean external;

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
}
