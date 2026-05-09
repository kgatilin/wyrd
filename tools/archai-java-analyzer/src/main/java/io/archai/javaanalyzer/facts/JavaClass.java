package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

import java.util.ArrayList;
import java.util.List;

/**
 * One Java type declaration — class, interface, enum, record, or annotation.
 *
 * <p>Fields stay close to JavaParser's AST output. Resolution to FQN happens
 * via JavaParser's symbol solver when possible; otherwise the simple name as
 * written in source is preserved (and {@link #external} marks it as such).
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({
    "fqn",
    "package",
    "name",
    "kind",
    "modifiers",
    "type_parameters",
    "extends",
    "implements",
    "permits",
    "source_file",
    "doc",
    "annotations",
    "fields",
    "methods",
    "enum_constants"
})
public final class JavaClass {

    private String fqn = "";
    private String pkg = "";
    private String name = "";
    private String kind = "class";
    private List<String> modifiers = new ArrayList<>();
    private List<String> typeParameters = new ArrayList<>();
    private String extends_ = "";
    private List<String> implements_ = new ArrayList<>();
    private List<String> permits = new ArrayList<>();
    private String sourceFile = "";
    private String doc = "";
    private List<JavaAnnotation> annotations = new ArrayList<>();
    private List<JavaField> fields = new ArrayList<>();
    private List<JavaMethod> methods = new ArrayList<>();
    private List<String> enumConstants = new ArrayList<>();

    public String getFqn() { return fqn; }
    public void setFqn(String fqn) { this.fqn = fqn; }

    @JsonProperty("package")
    public String getPackage() { return pkg; }
    public void setPackage(String pkg) { this.pkg = pkg; }

    public String getName() { return name; }
    public void setName(String name) { this.name = name; }

    public String getKind() { return kind; }
    public void setKind(String kind) { this.kind = kind; }

    public List<String> getModifiers() { return modifiers; }
    public void setModifiers(List<String> modifiers) { this.modifiers = modifiers; }

    @JsonProperty("type_parameters")
    public List<String> getTypeParameters() { return typeParameters; }
    public void setTypeParameters(List<String> typeParameters) { this.typeParameters = typeParameters; }

    @JsonProperty("extends")
    public String getExtends() { return extends_; }
    public void setExtends(String extends_) { this.extends_ = extends_; }

    @JsonProperty("implements")
    public List<String> getImplements() { return implements_; }
    public void setImplements(List<String> implements_) { this.implements_ = implements_; }

    public List<String> getPermits() { return permits; }
    public void setPermits(List<String> permits) { this.permits = permits; }

    @JsonProperty("source_file")
    public String getSourceFile() { return sourceFile; }
    public void setSourceFile(String sourceFile) { this.sourceFile = sourceFile; }

    public String getDoc() { return doc; }
    public void setDoc(String doc) { this.doc = doc; }

    public List<JavaAnnotation> getAnnotations() { return annotations; }
    public void setAnnotations(List<JavaAnnotation> annotations) { this.annotations = annotations; }

    public List<JavaField> getFields() { return fields; }
    public void setFields(List<JavaField> fields) { this.fields = fields; }

    public List<JavaMethod> getMethods() { return methods; }
    public void setMethods(List<JavaMethod> methods) { this.methods = methods; }

    @JsonProperty("enum_constants")
    public List<String> getEnumConstants() { return enumConstants; }
    public void setEnumConstants(List<String> enumConstants) { this.enumConstants = enumConstants; }
}
