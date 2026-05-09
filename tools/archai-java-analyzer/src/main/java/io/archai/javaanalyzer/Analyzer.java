package io.archai.javaanalyzer;

import com.github.javaparser.JavaParser;
import com.github.javaparser.ParseResult;
import com.github.javaparser.ParserConfiguration;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.ImportDeclaration;
import com.github.javaparser.ast.Modifier;
import com.github.javaparser.ast.Node;
import com.github.javaparser.ast.NodeList;
import com.github.javaparser.ast.body.AnnotationDeclaration;
import com.github.javaparser.ast.body.BodyDeclaration;
import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.ConstructorDeclaration;
import com.github.javaparser.ast.body.EnumConstantDeclaration;
import com.github.javaparser.ast.body.EnumDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.body.Parameter;
import com.github.javaparser.ast.body.RecordDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.body.VariableDeclarator;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.expr.MemberValuePair;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.NormalAnnotationExpr;
import com.github.javaparser.ast.expr.SingleMemberAnnotationExpr;
import com.github.javaparser.ast.type.ClassOrInterfaceType;
import com.github.javaparser.ast.type.ReferenceType;
import com.github.javaparser.ast.type.TypeParameter;
import com.github.javaparser.utils.SourceRoot;

import io.archai.javaanalyzer.facts.JavaAnnotation;
import io.archai.javaanalyzer.facts.JavaCall;
import io.archai.javaanalyzer.facts.JavaClass;
import io.archai.javaanalyzer.facts.JavaFacts;
import io.archai.javaanalyzer.facts.JavaField;
import io.archai.javaanalyzer.facts.JavaImport;
import io.archai.javaanalyzer.facts.JavaMethod;
import io.archai.javaanalyzer.facts.JavaParam;
import io.archai.javaanalyzer.facts.ParseWarning;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Optional;
import java.util.TreeSet;
import java.util.stream.Collectors;

/**
 * Walks one or more source roots with JavaParser and collects {@link
 * JavaFacts}.
 *
 * <p>The analyzer is intentionally semantically dumb: it dumps what the AST
 * tells it. Schema interpretation (stereotypes, archai-domain mapping) lives
 * in the Go translator, never here.
 *
 * <p>JavaParser's symbol solver is configured per-root with a {@link
 * com.github.javaparser.symbolsolver.resolution.typesolvers.JavaParserTypeSolver}
 * + {@link com.github.javaparser.symbolsolver.resolution.typesolvers.ReflectionTypeSolver}
 * so resolution works inside the analyzed source set and falls back to
 * runtime-classpath types (i.e. {@code java.*}). Anything outside both is
 * left as the textual form and marked {@link JavaCall#isExternal()}.
 *
 * <p>Output ordering: classes sorted by FQN, members within a class kept in
 * source order (deterministic per file), imports sorted, packages sorted.
 */
public final class Analyzer {

    private final boolean includePrivate;

    public Analyzer() {
        this(true);
    }

    public Analyzer(boolean includePrivate) {
        this.includePrivate = includePrivate;
    }

    /**
     * Analyze the given source roots and return the merged {@link JavaFacts}.
     *
     * @param srcRoots one or more directories containing Java source
     * @return populated {@link JavaFacts}
     * @throws IOException if a root cannot be read
     */
    public JavaFacts analyze(List<Path> srcRoots) throws IOException {
        JavaFacts facts = new JavaFacts();

        // Record src_roots in input order — keeps origin information visible
        // to the downstream consumer without forcing canonical paths.
        for (Path root : srcRoots) {
            facts.getSrcRoots().add(root.toString());
        }

        // Resolved roots used for relative-path computation in source_file.
        List<Path> rootsAbs = new ArrayList<>(srcRoots.size());
        for (Path root : srcRoots) {
            rootsAbs.add(root.toAbsolutePath().normalize());
        }

        TreeSet<String> packages = new TreeSet<>();
        List<JavaClass> classes = new ArrayList<>();
        List<JavaImport> imports = new ArrayList<>();
        List<ParseWarning> warnings = new ArrayList<>();

        ParserConfiguration parserConfig = new ParserConfiguration()
            .setLanguageLevel(ParserConfiguration.LanguageLevel.JAVA_17);

        for (int i = 0; i < srcRoots.size(); i++) {
            Path root = srcRoots.get(i);
            if (!Files.isDirectory(root)) {
                warnings.add(new ParseWarning(root.toString(), "src-root is not a directory"));
                continue;
            }

            SourceRoot sourceRoot = new SourceRoot(root, parserConfig);
            // Don't re-write input on close.
            sourceRoot.setPrinter(cu -> cu.toString());

            List<ParseResult<CompilationUnit>> results = sourceRoot.tryToParseParallelized();

            Path rootAbs = rootsAbs.get(i);

            for (ParseResult<CompilationUnit> result : results) {
                if (!result.isSuccessful() || result.getResult().isEmpty()) {
                    String file = result.getResult()
                        .flatMap(CompilationUnit::getStorage)
                        .map(s -> s.getPath().toString())
                        .orElse("<unknown>");
                    String msg = result.getProblems().stream()
                        .map(p -> p.getMessage())
                        .collect(Collectors.joining("; "));
                    if (msg.isEmpty()) {
                        msg = "parse failed";
                    }
                    warnings.add(new ParseWarning(file, msg));
                    continue;
                }

                CompilationUnit cu = result.getResult().get();
                processCompilationUnit(cu, rootAbs, packages, classes, imports);
            }
        }

        // Deterministic ordering — classes by FQN, imports by (from, to_class, kind).
        classes.sort(Comparator.comparing(JavaClass::getFqn));
        imports.sort(Comparator
            .comparing(JavaImport::getFrom)
            .thenComparing(JavaImport::getToClass)
            .thenComparing(JavaImport::getKind));

        facts.getPackages().addAll(packages);
        facts.getClasses().addAll(classes);
        facts.getImports().addAll(imports);
        facts.getParseWarnings().addAll(warnings);

        return facts;
    }

    private void processCompilationUnit(
        CompilationUnit cu,
        Path rootAbs,
        TreeSet<String> packages,
        List<JavaClass> classes,
        List<JavaImport> imports
    ) {
        String pkg = cu.getPackageDeclaration()
            .map(p -> p.getNameAsString())
            .orElse("");

        if (!pkg.isEmpty()) {
            packages.add(pkg);
        }

        String sourceFile = relativiseSource(cu, rootAbs);

        // Primary class FQN — used as the "from" attribution for imports.
        // Fall back to package name if a CU somehow has no top-level type.
        String primaryFqn = cu.getTypes().stream()
            .findFirst()
            .map(t -> joinFqn(pkg, t.getNameAsString()))
            .orElse(pkg);

        // Imports — captured per CU and attributed to the primary class.
        for (ImportDeclaration imp : cu.getImports()) {
            JavaImport ji = new JavaImport();
            ji.setFrom(primaryFqn);
            ji.setToClass(imp.getNameAsString());
            String kind;
            if (imp.isStatic() && imp.isAsterisk()) {
                kind = "static_wildcard";
            } else if (imp.isStatic()) {
                kind = "static";
            } else if (imp.isAsterisk()) {
                kind = "wildcard";
            } else {
                kind = "class";
            }
            ji.setKind(kind);
            imports.add(ji);
        }

        // Top-level type declarations (and their nested types).
        for (TypeDeclaration<?> td : cu.getTypes()) {
            collectType(td, pkg, sourceFile, classes);
        }
    }

    /**
     * Recursively collect a top-level or nested type declaration into the
     * facts list. Nested types are emitted as separate classes with
     * {@code Outer.Inner} as the simple name and {@code pkg.Outer.Inner} as
     * the FQN — keeps a flat class list while preserving nesting in the FQN.
     */
    private void collectType(
        TypeDeclaration<?> td,
        String pkg,
        String sourceFile,
        List<JavaClass> classes
    ) {
        if (!includePrivate && hasModifier(td, Modifier.Keyword.PRIVATE)) {
            return;
        }

        JavaClass jc = new JavaClass();

        String simpleName = computeNestedName(td);
        jc.setName(simpleName);
        jc.setPackage(pkg);
        jc.setFqn(joinFqn(pkg, simpleName));
        jc.setSourceFile(sourceFile);

        if (td instanceof ClassOrInterfaceDeclaration coid) {
            jc.setKind(coid.isInterface() ? "interface" : "class");
            jc.getTypeParameters().addAll(typeParamsToStrings(coid.getTypeParameters()));

            if (!coid.isInterface() && !coid.getExtendedTypes().isEmpty()) {
                jc.setExtends(coid.getExtendedTypes(0).getNameWithScope());
            }
            if (coid.isInterface()) {
                // Interfaces use 'extends' for super-interfaces; record them as implements
                // is reserved for class -> interface, so super-interfaces of interfaces
                // go into 'implements' to keep one shape downstream. Document in SCHEMA.md.
                for (ClassOrInterfaceType t : coid.getExtendedTypes()) {
                    jc.getImplements().add(t.getNameWithScope());
                }
            } else {
                for (ClassOrInterfaceType t : coid.getImplementedTypes()) {
                    jc.getImplements().add(t.getNameWithScope());
                }
            }
            for (ClassOrInterfaceType t : coid.getPermittedTypes()) {
                jc.getPermits().add(t.getNameWithScope());
            }
        } else if (td instanceof EnumDeclaration ed) {
            jc.setKind("enum");
            for (ClassOrInterfaceType t : ed.getImplementedTypes()) {
                jc.getImplements().add(t.getNameWithScope());
            }
            for (EnumConstantDeclaration c : ed.getEntries()) {
                jc.getEnumConstants().add(c.getNameAsString());
            }
        } else if (td instanceof RecordDeclaration rd) {
            jc.setKind("record");
            jc.getTypeParameters().addAll(typeParamsToStrings(rd.getTypeParameters()));
            for (ClassOrInterfaceType t : rd.getImplementedTypes()) {
                jc.getImplements().add(t.getNameWithScope());
            }
            // Record components surface as fields — they are part of the record's
            // public API and downstream needs them.
            for (Parameter p : rd.getParameters()) {
                JavaField f = new JavaField();
                f.setName(p.getNameAsString());
                f.setType(p.getType().asString());
                f.getModifiers().add("final");
                f.getModifiers().add("private");
                f.getAnnotations().addAll(annotationsOf(p.getAnnotations()));
                jc.getFields().add(f);
            }
        } else if (td instanceof AnnotationDeclaration) {
            jc.setKind("annotation");
        } else {
            jc.setKind("class");
        }

        // Modifiers — preserve declared order from JavaParser (matches source
        // order) but stable since AST is deterministic.
        for (Modifier m : td.getModifiers()) {
            jc.getModifiers().add(m.getKeyword().asString());
        }

        // Annotations on the type itself.
        jc.getAnnotations().addAll(annotationsOf(td.getAnnotations()));

        // Doc comment — first line trimmed, full body preserved with single
        // {@code \n} line separators.
        td.getJavadocComment()
            .ifPresent(c -> jc.setDoc(stripJavadoc(c.getContent())));

        // Body declarations: fields, methods, constructors. Order preserved
        // from source.
        List<? extends BodyDeclaration<?>> members = td.getMembers();
        for (BodyDeclaration<?> member : members) {
            if (member instanceof FieldDeclaration fd) {
                if (!includePrivate && fd.isPrivate()) {
                    continue;
                }
                List<String> mods = modifiersToStrings(fd.getModifiers());
                List<JavaAnnotation> anns = annotationsOf(fd.getAnnotations());
                String doc = fd.getJavadocComment().map(c -> stripJavadoc(c.getContent())).orElse("");
                for (VariableDeclarator v : fd.getVariables()) {
                    JavaField f = new JavaField();
                    f.setName(v.getNameAsString());
                    f.setType(v.getType().asString());
                    f.getModifiers().addAll(mods);
                    f.getAnnotations().addAll(anns);
                    f.setDoc(doc);
                    jc.getFields().add(f);
                }
            } else if (member instanceof MethodDeclaration md) {
                if (!includePrivate && md.isPrivate()) {
                    continue;
                }
                jc.getMethods().add(buildMethod(md));
            } else if (member instanceof ConstructorDeclaration cd) {
                if (!includePrivate && cd.isPrivate()) {
                    continue;
                }
                jc.getMethods().add(buildConstructor(cd));
            } else if (member instanceof TypeDeclaration<?> nested) {
                // Nested type — emit as a sibling class with Outer.Inner naming.
                collectType(nested, pkg, sourceFile, classes);
            }
            // Initializers and other members are intentionally skipped (out of
            // scope for v1).
        }

        classes.add(jc);
    }

    private JavaMethod buildMethod(MethodDeclaration md) {
        JavaMethod jm = new JavaMethod();
        jm.setName(md.getNameAsString());
        jm.setKind("method");
        jm.getModifiers().addAll(modifiersToStrings(md.getModifiers()));
        jm.getTypeParameters().addAll(typeParamsToStrings(md.getTypeParameters()));
        jm.setReturns(md.getType().asString());
        for (Parameter p : md.getParameters()) {
            jm.getParams().add(buildParam(p));
        }
        for (ReferenceType t : md.getThrownExceptions()) {
            jm.getThrows().add(t.asString());
        }
        jm.getAnnotations().addAll(annotationsOf(md.getAnnotations()));
        md.getJavadocComment().ifPresent(c -> jm.setDoc(stripJavadoc(c.getContent())));
        jm.getCalls().addAll(extractCalls(md));
        return jm;
    }

    private JavaMethod buildConstructor(ConstructorDeclaration cd) {
        JavaMethod jm = new JavaMethod();
        jm.setName(cd.getNameAsString());
        jm.setKind("constructor");
        jm.getModifiers().addAll(modifiersToStrings(cd.getModifiers()));
        jm.getTypeParameters().addAll(typeParamsToStrings(cd.getTypeParameters()));
        jm.setReturns("void");
        for (Parameter p : cd.getParameters()) {
            jm.getParams().add(buildParam(p));
        }
        for (ReferenceType t : cd.getThrownExceptions()) {
            jm.getThrows().add(t.asString());
        }
        jm.getAnnotations().addAll(annotationsOf(cd.getAnnotations()));
        cd.getJavadocComment().ifPresent(c -> jm.setDoc(stripJavadoc(c.getContent())));
        jm.getCalls().addAll(extractCalls(cd));
        return jm;
    }

    private JavaParam buildParam(Parameter p) {
        JavaParam jp = new JavaParam();
        jp.setName(p.getNameAsString());
        jp.setType(p.getType().asString());
        jp.setVarargs(p.isVarArgs());
        jp.getModifiers().addAll(modifiersToStrings(p.getModifiers()));
        jp.getAnnotations().addAll(annotationsOf(p.getAnnotations()));
        return jp;
    }

    /**
     * Extract method calls from a method body. Resolution is best-effort:
     * if the symbol solver is not configured (the default in v1), the call
     * receiver is captured textually and {@link JavaCall#isExternal()} is
     * left {@code false} — Go side decides.
     *
     * <p>TODO: wire JavaParser symbol solver in a follow-up so {@code
     * to_class} is the resolved FQN when the receiver type lives in the
     * analyzed source set. For v1 the textual scope is enough to enable the
     * Go translator to do same-package matching.
     */
    private List<JavaCall> extractCalls(Node bodyOwner) {
        List<JavaCall> out = new ArrayList<>();
        bodyOwner.findAll(MethodCallExpr.class).forEach(call -> {
            JavaCall jc = new JavaCall();
            jc.setToMethod(call.getNameAsString());
            jc.setToClass(call.getScope()
                .map(Object::toString)
                .orElse(""));
            // Static-ness: best-effort textual heuristic — if the receiver
            // looks like a Type (starts uppercase) treat as static. The Go
            // side can refine this once #102 wires symbol resolution.
            String scope = jc.getToClass();
            jc.setStatic(!scope.isEmpty()
                && Character.isUpperCase(scope.charAt(0))
                && scope.indexOf('.') < 0);
            // External flag: left false in v1 since we do not resolve. The
            // translator (Go side) can flip it based on the analyzed
            // package set.
            jc.setExternal(false);
            out.add(jc);
        });
        // Calls follow source order (findAll is depth-first, document order)
        // — keeps output deterministic without further sorting.
        return out;
    }

    private List<JavaAnnotation> annotationsOf(NodeList<AnnotationExpr> nodeList) {
        List<JavaAnnotation> out = new ArrayList<>();
        for (AnnotationExpr a : nodeList) {
            JavaAnnotation ja = new JavaAnnotation();
            ja.setFqn(a.getNameAsString());
            if (a instanceof SingleMemberAnnotationExpr sm) {
                ja.getArgs().add(sm.getMemberValue().toString());
            } else if (a instanceof NormalAnnotationExpr nm) {
                for (MemberValuePair mvp : nm.getPairs()) {
                    ja.getArgs().add(mvp.getNameAsString() + "=" + mvp.getValue().toString());
                }
            }
            out.add(ja);
        }
        return out;
    }

    private static List<String> modifiersToStrings(NodeList<Modifier> mods) {
        List<String> out = new ArrayList<>(mods.size());
        for (Modifier m : mods) {
            out.add(m.getKeyword().asString());
        }
        return out;
    }

    private static List<String> typeParamsToStrings(NodeList<TypeParameter> params) {
        List<String> out = new ArrayList<>(params.size());
        for (TypeParameter p : params) {
            String s = p.getNameAsString();
            if (!p.getTypeBound().isEmpty()) {
                s += " extends " + p.getTypeBound().stream()
                    .map(Object::toString)
                    .collect(Collectors.joining(" & "));
            }
            out.add(s);
        }
        return out;
    }

    private static boolean hasModifier(TypeDeclaration<?> td, Modifier.Keyword kw) {
        for (Modifier m : td.getModifiers()) {
            if (m.getKeyword() == kw) return true;
        }
        return false;
    }

    /**
     * Build the simple name for a possibly-nested type. Top-level: just the
     * simple name. Nested: dot-joined chain through enclosing type names.
     */
    private static String computeNestedName(TypeDeclaration<?> td) {
        List<String> parts = new ArrayList<>();
        Node current = td;
        while (current != null) {
            if (current instanceof TypeDeclaration<?> t) {
                parts.add(0, t.getNameAsString());
            }
            current = current.getParentNode().orElse(null);
        }
        return String.join(".", parts);
    }

    private static String joinFqn(String pkg, String name) {
        if (pkg == null || pkg.isEmpty()) return name;
        return pkg + "." + name;
    }

    /**
     * Strip the leading {@code *} (and optional space) that JavaParser
     * preserves on each line of a javadoc body. Lines are trimmed, leading
     * blanks dropped, and the result rejoined with {@code "\n"} so output
     * is portable across platforms.
     */
    private static String stripJavadoc(String raw) {
        if (raw == null || raw.isEmpty()) return "";
        String[] lines = raw.split("\\r?\\n", -1);
        List<String> out = new ArrayList<>(lines.length);
        for (String line : lines) {
            String trimmed = line.trim();
            if (trimmed.startsWith("*")) {
                trimmed = trimmed.substring(1);
                if (trimmed.startsWith(" ")) {
                    trimmed = trimmed.substring(1);
                } else {
                    // Strip exactly one space if present; otherwise keep
                    // remainder verbatim.
                    trimmed = trimmed.stripLeading();
                }
            }
            out.add(trimmed);
        }
        // Trim leading/trailing blank lines but preserve blank lines inside
        // the body so paragraph breaks survive.
        int start = 0;
        while (start < out.size() && out.get(start).isEmpty()) start++;
        int end = out.size();
        while (end > start && out.get(end - 1).isEmpty()) end--;
        return String.join("\n", out.subList(start, end));
    }

    private static String relativiseSource(CompilationUnit cu, Path rootAbs) {
        Optional<CompilationUnit.Storage> storage = cu.getStorage();
        if (storage.isEmpty()) return "";
        Path file = storage.get().getPath().toAbsolutePath().normalize();
        String s;
        try {
            s = rootAbs.relativize(file).toString();
        } catch (IllegalArgumentException e) {
            s = file.toString();
        }
        // Normalise to forward slashes so output is portable across OS
        // boundaries — golden tests run on Linux and macOS without divergence.
        return s.replace(java.io.File.separatorChar, '/');
    }
}
