package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

import java.util.ArrayList;
import java.util.List;

/**
 * A single Java field declaration. One {@code int x, y;} expands into two
 * {@link JavaField} entries — JavaFacts always names one field per entry.
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({"name", "type", "modifiers", "annotations", "doc"})
public final class JavaField {

    private String name = "";
    private String type = "";
    private List<String> modifiers = new ArrayList<>();
    private List<JavaAnnotation> annotations = new ArrayList<>();
    private String doc = "";

    public String getName() { return name; }
    public void setName(String name) { this.name = name; }

    public String getType() { return type; }
    public void setType(String type) { this.type = type; }

    public List<String> getModifiers() { return modifiers; }
    public void setModifiers(List<String> modifiers) { this.modifiers = modifiers; }

    public List<JavaAnnotation> getAnnotations() { return annotations; }
    public void setAnnotations(List<JavaAnnotation> annotations) { this.annotations = annotations; }

    public String getDoc() { return doc; }
    public void setDoc(String doc) { this.doc = doc; }
}
