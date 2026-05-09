package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

import java.util.ArrayList;
import java.util.List;

/**
 * A formal parameter on a method or constructor.
 *
 * <p>Varargs parameters are flagged via {@link #varargs}; their {@link #type}
 * is the element type (e.g. {@code String} for {@code String... args}).
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({"name", "type", "varargs", "modifiers", "annotations"})
public final class JavaParam {

    private String name = "";
    private String type = "";
    private boolean varargs;
    private List<String> modifiers = new ArrayList<>();
    private List<JavaAnnotation> annotations = new ArrayList<>();

    public String getName() { return name; }
    public void setName(String name) { this.name = name; }

    public String getType() { return type; }
    public void setType(String type) { this.type = type; }

    public boolean isVarargs() { return varargs; }
    public void setVarargs(boolean varargs) { this.varargs = varargs; }

    public List<String> getModifiers() { return modifiers; }
    public void setModifiers(List<String> modifiers) { this.modifiers = modifiers; }

    public List<JavaAnnotation> getAnnotations() { return annotations; }
    public void setAnnotations(List<JavaAnnotation> annotations) { this.annotations = annotations; }
}
