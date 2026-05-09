package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/**
 * Structured fallback information for a {@link JavaCall} whose receiver type
 * could not be resolved to a class in the analyzed source set.
 *
 * <p>Both fields default to the empty string when the parent {@link JavaCall}
 * is resolved (i.e. {@link JavaCall#getTargetFqn()} non-empty); they only
 * carry useful values when {@link JavaCall#isExternal()} is {@code true}.
 *
 * <p>{@link #receiverText} is the raw textual form of the call's receiver
 * scope as written in source — e.g. {@code "voice()"}, {@code "System.out"},
 * {@code "new Foo()"}, or {@code ""} for unqualified calls. {@link #methodName}
 * mirrors {@link JavaCall#getToMethod()} for convenience so downstream
 * consumers can match on a single object.
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({"receiver_text", "method_name"})
public final class JavaCallUnresolved {

    private String receiverText = "";
    private String methodName = "";

    public JavaCallUnresolved() {}

    public JavaCallUnresolved(String receiverText, String methodName) {
        this.receiverText = receiverText;
        this.methodName = methodName;
    }

    @JsonProperty("receiver_text")
    public String getReceiverText() { return receiverText; }
    public void setReceiverText(String receiverText) { this.receiverText = receiverText; }

    @JsonProperty("method_name")
    public String getMethodName() { return methodName; }
    public void setMethodName(String methodName) { this.methodName = methodName; }
}
