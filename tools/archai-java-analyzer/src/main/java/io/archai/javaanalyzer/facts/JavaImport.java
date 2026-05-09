package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/**
 * One {@code import} statement, attributed to its source compilation unit's
 * primary class FQN.
 *
 * <p>Kind is one of {@code class}, {@code static}, {@code wildcard},
 * {@code static_wildcard}.
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({"from", "to_class", "kind"})
public final class JavaImport {

    private String from = "";
    private String toClass = "";
    private String kind = "class";

    public String getFrom() { return from; }
    public void setFrom(String from) { this.from = from; }

    @JsonProperty("to_class")
    public String getToClass() { return toClass; }
    public void setToClass(String toClass) { this.toClass = toClass; }

    public String getKind() { return kind; }
    public void setKind(String kind) { this.kind = kind; }
}
