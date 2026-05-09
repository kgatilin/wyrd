package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

import java.util.ArrayList;
import java.util.List;

/**
 * A method (or constructor) declaration on a {@link JavaClass}.
 *
 * <p>Constructors set {@link #kind} to {@code "constructor"}; instance methods
 * to {@code "method"}; static initialisers are not emitted (out of scope for
 * v1 — tracked as future work).
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({
    "name",
    "kind",
    "modifiers",
    "type_parameters",
    "params",
    "returns",
    "throws",
    "annotations",
    "doc",
    "calls"
})
public final class JavaMethod {

    private String name = "";
    private String kind = "method";
    private List<String> modifiers = new ArrayList<>();
    private List<String> typeParameters = new ArrayList<>();
    private List<JavaParam> params = new ArrayList<>();
    private String returns = "void";
    private List<String> throws_ = new ArrayList<>();
    private List<JavaAnnotation> annotations = new ArrayList<>();
    private String doc = "";
    private List<JavaCall> calls = new ArrayList<>();

    public String getName() { return name; }
    public void setName(String name) { this.name = name; }

    public String getKind() { return kind; }
    public void setKind(String kind) { this.kind = kind; }

    public List<String> getModifiers() { return modifiers; }
    public void setModifiers(List<String> modifiers) { this.modifiers = modifiers; }

    @JsonProperty("type_parameters")
    public List<String> getTypeParameters() { return typeParameters; }
    public void setTypeParameters(List<String> typeParameters) { this.typeParameters = typeParameters; }

    public List<JavaParam> getParams() { return params; }
    public void setParams(List<JavaParam> params) { this.params = params; }

    public String getReturns() { return returns; }
    public void setReturns(String returns) { this.returns = returns; }

    @JsonProperty("throws")
    public List<String> getThrows() { return throws_; }
    public void setThrows(List<String> throws_) { this.throws_ = throws_; }

    public List<JavaAnnotation> getAnnotations() { return annotations; }
    public void setAnnotations(List<JavaAnnotation> annotations) { this.annotations = annotations; }

    public String getDoc() { return doc; }
    public void setDoc(String doc) { this.doc = doc; }

    public List<JavaCall> getCalls() { return calls; }
    public void setCalls(List<JavaCall> calls) { this.calls = calls; }
}
