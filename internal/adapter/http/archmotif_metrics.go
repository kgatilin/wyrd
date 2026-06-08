package http

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	nethttp "net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/serve"
)

const archMotifMetricsSchema = "archai.archmotif.metrics.v1"

type archMotifMetricsResponse struct {
	Schema        string                   `json:"schema"`
	Scope         string                   `json:"scope"`
	Nodes         int                      `json:"nodes"`
	Edges         int                      `json:"edges"`
	Components    int                      `json:"components"`
	LayeringScore float64                  `json:"layeringScore"`
	Acyclic       bool                     `json:"acyclic"`
	Cycles        []archMotifCycle         `json:"cycles"`
	TopCoupling   []archMotifPackageMetric `json:"topCoupling"`
	GodPackages   []archMotifPackageMetric `json:"godPackages"`
	Packages      []archMotifPackageMetric `json:"packages"`
	Embeddings    archMotifEmbeddingStatus `json:"embeddings"`
	Warnings      []string                 `json:"warnings,omitempty"`
}

type archMotifPackageMetric struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Group       string   `json:"group"`
	Layer       string   `json:"layer,omitempty"`
	Aggregate   string   `json:"aggregate,omitempty"`
	FanIn       int      `json:"fanIn"`
	FanOut      int      `json:"fanOut"`
	Degree      int      `json:"degree"`
	Instability float64  `json:"instability"`
	DependsOn   []string `json:"dependsOn"`
	UsedBy      []string `json:"usedBy"`
}

type archMotifCycle struct {
	Packages []string `json:"packages"`
	Edges    int      `json:"edges"`
}

type archMotifEmbeddingStatus struct {
	TextGraphPath   string `json:"textGraphPath,omitempty"`
	VectorGraphPath string `json:"vectorGraphPath,omitempty"`
	HasTextGraph    bool   `json:"hasTextGraph"`
	HasVectors      bool   `json:"hasVectors"`
	VectorCount     int    `json:"vectorCount"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
	Binary          string `json:"binary,omitempty"`
}

type archMotifEmbedRequest struct {
	Project  string `json:"project,omitempty"`
	Model    string `json:"model,omitempty"`
	Location string `json:"location,omitempty"`
}

type archMotifEmbedResponse struct {
	Status          string                   `json:"status"`
	Message         string                   `json:"message"`
	Embeddings      archMotifEmbeddingStatus `json:"embeddings"`
	TextGraphPath   string                   `json:"textGraphPath"`
	VectorGraphPath string                   `json:"vectorGraphPath"`
	Stdout          string                   `json:"stdout,omitempty"`
	Stderr          string                   `json:"stderr,omitempty"`
}

type archMotifPackageGraph struct {
	Nodes map[string]archMotifPackageNode
	Edges []archMotifPackageEdge
	Out   map[string]map[string]int
	In    map[string]map[string]int
}

type archMotifPackageNode struct {
	ID        string
	Name      string
	Group     string
	Layer     string
	Aggregate string
	Doc       string
}

type archMotifPackageEdge struct {
	From   string
	To     string
	Weight int
}

func (s *Server) handleArchMotifMetricsJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "state unavailable", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()
	resp := buildArchMotifMetrics(snap)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleArchMotifEmbed(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "state unavailable", nethttp.StatusServiceUnavailable)
		return
	}

	var req archMotifEmbedRequest
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			nethttp.Error(w, "decode embed request: "+err.Error(), nethttp.StatusBadRequest)
			return
		}
	}

	snap := state.Snapshot()
	graph := buildArchMotifPackageGraph(snap.Packages)
	artifactDir := archMotifArtifactDir(snap.Root)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		nethttp.Error(w, "create artifact dir: "+err.Error(), nethttp.StatusInternalServerError)
		return
	}

	textGraph := filepath.Join(artifactDir, "packages-text.graphml")
	vectorGraph := filepath.Join(artifactDir, "packages-vec.graphml")
	if err := writeArchMotifPackageGraphMLFile(textGraph, graph); err != nil {
		nethttp.Error(w, "write package GraphML: "+err.Error(), nethttp.StatusInternalServerError)
		return
	}

	bin, err := resolveArchMotifBin()
	if err != nil {
		resp := archMotifEmbedResponse{
			Status:          "unavailable",
			Message:         err.Error(),
			Embeddings:      archMotifEmbeddingInfo(snap.Root, ""),
			TextGraphPath:   textGraph,
			VectorGraphPath: vectorGraph,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	args := []string{
		"embed",
		textGraph,
		"--text-key", "doc",
		"--out", vectorGraph,
		"--cache-dir", filepath.Join(artifactDir, "embed-cache"),
	}
	if req.Project == "" {
		req.Project = firstNonEmpty(os.Getenv("ARCHMOTIF_EMBED_PROJECT"), os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GCP_PROJECT"))
	}
	if req.Project != "" {
		args = append(args, "--project", req.Project)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Location != "" {
		args = append(args, "--location", req.Location)
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	status := "ready"
	message := "embeddings generated"
	if ctx.Err() != nil {
		status = "failed"
		message = "archmotif embed timed out"
	} else if err != nil {
		status = "failed"
		message = err.Error()
	}

	resp := archMotifEmbedResponse{
		Status:          status,
		Message:         message,
		Embeddings:      archMotifEmbeddingInfo(snap.Root, bin),
		TextGraphPath:   textGraph,
		VectorGraphPath: vectorGraph,
		Stdout:          strings.TrimSpace(stdout.String()),
		Stderr:          strings.TrimSpace(stderr.String()),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func buildArchMotifMetrics(snap serve.Snapshot) archMotifMetricsResponse {
	graph := buildArchMotifPackageGraph(snap.Packages)
	components := weakComponentCount(graph)
	cycles := stronglyConnectedCycles(graph)
	if cycles == nil {
		cycles = []archMotifCycle{}
	}
	packageMetrics := archMotifPackageMetrics(graph)
	topCoupling := append([]archMotifPackageMetric(nil), packageMetrics...)
	sort.SliceStable(topCoupling, func(i, j int) bool {
		if topCoupling[i].Degree != topCoupling[j].Degree {
			return topCoupling[i].Degree > topCoupling[j].Degree
		}
		return topCoupling[i].ID < topCoupling[j].ID
	})
	if len(topCoupling) > 8 {
		topCoupling = topCoupling[:8]
	}
	if topCoupling == nil {
		topCoupling = []archMotifPackageMetric{}
	}

	cycleEdges := 0
	for _, cycle := range cycles {
		cycleEdges += cycle.Edges
	}
	layeringScore := 1.0
	if len(graph.Edges) > 0 {
		layeringScore = math.Max(0, 1-float64(cycleEdges)/float64(len(graph.Edges)))
	}

	godPackages := godPackageCandidates(packageMetrics)
	if godPackages == nil {
		godPackages = []archMotifPackageMetric{}
	}

	return archMotifMetricsResponse{
		Schema:        archMotifMetricsSchema,
		Scope:         "packages",
		Nodes:         len(graph.Nodes),
		Edges:         len(graph.Edges),
		Components:    components,
		LayeringScore: roundFloat(layeringScore, 3),
		Acyclic:       len(cycles) == 0,
		Cycles:        cycles,
		TopCoupling:   topCoupling,
		GodPackages:   godPackages,
		Packages:      packageMetrics,
		Embeddings:    archMotifEmbeddingInfo(snap.Root, ""),
	}
}

func buildArchMotifPackageGraph(packages []domain.PackageModel) archMotifPackageGraph {
	graph := archMotifPackageGraph{
		Nodes: make(map[string]archMotifPackageNode, len(packages)),
		Out:   make(map[string]map[string]int),
		In:    make(map[string]map[string]int),
	}
	for _, pkg := range packages {
		if pkg.Path == "" {
			continue
		}
		graph.Nodes[pkg.Path] = archMotifPackageNode{
			ID:        pkg.Path,
			Name:      packageDisplayName(pkg),
			Group:     packageGroup(pkg.Path),
			Layer:     pkg.Layer,
			Aggregate: pkg.Aggregate,
			Doc:       packageSemanticText(pkg),
		}
	}

	for _, pkg := range packages {
		from := pkg.Path
		if _, ok := graph.Nodes[from]; !ok {
			continue
		}
		for _, dep := range pkg.Dependencies {
			if dep.To.External || dep.To.Package == "" {
				continue
			}
			depFrom := dep.From.Package
			if depFrom == "" {
				depFrom = from
			}
			if _, ok := graph.Nodes[depFrom]; !ok {
				depFrom = from
			}
			to := dep.To.Package
			if _, ok := graph.Nodes[to]; !ok || depFrom == to {
				continue
			}
			if graph.Out[depFrom] == nil {
				graph.Out[depFrom] = make(map[string]int)
			}
			if graph.In[to] == nil {
				graph.In[to] = make(map[string]int)
			}
			graph.Out[depFrom][to]++
			graph.In[to][depFrom]++
		}
	}

	for from, tos := range graph.Out {
		for to, weight := range tos {
			graph.Edges = append(graph.Edges, archMotifPackageEdge{From: from, To: to, Weight: weight})
		}
	}
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].From != graph.Edges[j].From {
			return graph.Edges[i].From < graph.Edges[j].From
		}
		return graph.Edges[i].To < graph.Edges[j].To
	})
	return graph
}

func archMotifPackageMetrics(graph archMotifPackageGraph) []archMotifPackageMetric {
	ids := sortedNodeIDs(graph.Nodes)
	out := make([]archMotifPackageMetric, 0, len(ids))
	for _, id := range ids {
		node := graph.Nodes[id]
		dependsOn := sortedMapKeys(graph.Out[id])
		usedBy := sortedMapKeys(graph.In[id])
		fanIn := len(usedBy)
		fanOut := len(dependsOn)
		instability := 0.0
		if fanIn+fanOut > 0 {
			instability = float64(fanOut) / float64(fanIn+fanOut)
		}
		out = append(out, archMotifPackageMetric{
			ID:          id,
			Name:        node.Name,
			Group:       node.Group,
			Layer:       node.Layer,
			Aggregate:   node.Aggregate,
			FanIn:       fanIn,
			FanOut:      fanOut,
			Degree:      fanIn + fanOut,
			Instability: roundFloat(instability, 3),
			DependsOn:   dependsOn,
			UsedBy:      usedBy,
		})
	}
	return out
}

func weakComponentCount(graph archMotifPackageGraph) int {
	ids := sortedNodeIDs(graph.Nodes)
	seen := make(map[string]bool, len(ids))
	count := 0
	for _, id := range ids {
		if seen[id] {
			continue
		}
		count++
		queue := []string{id}
		seen[id] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, next := range sortedMapKeys(graph.Out[cur]) {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
			for _, next := range sortedMapKeys(graph.In[cur]) {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
	}
	return count
}

func stronglyConnectedCycles(graph archMotifPackageGraph) []archMotifCycle {
	var index int
	stack := []string{}
	onStack := map[string]bool{}
	indices := map[string]int{}
	lowlink := map[string]int{}
	var cycles []archMotifCycle

	var visit func(string)
	visit = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range sortedMapKeys(graph.Out[v]) {
			if _, ok := indices[w]; !ok {
				visit(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] && indices[w] < lowlink[v] {
				lowlink[v] = indices[w]
			}
		}

		if lowlink[v] != indices[v] {
			return
		}
		component := []string{}
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			component = append(component, w)
			if w == v {
				break
			}
		}
		if len(component) <= 1 {
			return
		}
		sort.Strings(component)
		cycles = append(cycles, archMotifCycle{
			Packages: component,
			Edges:    countInternalEdges(graph, component),
		})
	}

	for _, id := range sortedNodeIDs(graph.Nodes) {
		if _, ok := indices[id]; !ok {
			visit(id)
		}
	}
	sort.Slice(cycles, func(i, j int) bool {
		if len(cycles[i].Packages) != len(cycles[j].Packages) {
			return len(cycles[i].Packages) > len(cycles[j].Packages)
		}
		return strings.Join(cycles[i].Packages, "\x00") < strings.Join(cycles[j].Packages, "\x00")
	})
	return cycles
}

func countInternalEdges(graph archMotifPackageGraph, ids []string) int {
	inSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		inSet[id] = true
	}
	count := 0
	for _, edge := range graph.Edges {
		if inSet[edge.From] && inSet[edge.To] {
			count++
		}
	}
	return count
}

func godPackageCandidates(metrics []archMotifPackageMetric) []archMotifPackageMetric {
	if len(metrics) < 4 {
		return nil
	}
	var sum float64
	for _, metric := range metrics {
		sum += float64(metric.Degree)
	}
	mean := sum / float64(len(metrics))
	var variance float64
	for _, metric := range metrics {
		delta := float64(metric.Degree) - mean
		variance += delta * delta
	}
	stddev := math.Sqrt(variance / float64(len(metrics)))
	threshold := mean + stddev*1.5
	out := []archMotifPackageMetric{}
	for _, metric := range metrics {
		if metric.Degree >= 4 && float64(metric.Degree) >= threshold {
			out = append(out, metric)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Degree != out[j].Degree {
			return out[i].Degree > out[j].Degree
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func packageSemanticText(pkg domain.PackageModel) string {
	var parts []string
	parts = append(parts, "package "+packageDisplayName(pkg), "path "+pkg.Path)
	if pkg.Layer != "" {
		parts = append(parts, "layer "+pkg.Layer)
	}
	if pkg.Aggregate != "" {
		parts = append(parts, "aggregate "+pkg.Aggregate)
	}

	addNamed := func(label string, names []string) {
		if len(names) == 0 {
			return
		}
		sort.Strings(names)
		parts = append(parts, label+": "+strings.Join(names, ", "))
	}

	ifaces := make([]string, 0, len(pkg.Interfaces))
	for _, iface := range pkg.Interfaces {
		methods := make([]string, 0, len(iface.Methods))
		for _, method := range iface.Methods {
			methods = append(methods, method.Signature())
		}
		ifaces = append(ifaces, domain.NameWithTypeParams(iface.Name, iface.TypeParams)+" { "+strings.Join(methods, "; ")+" }")
	}
	addNamed("interfaces", ifaces)

	structs := make([]string, 0, len(pkg.Structs))
	for _, st := range pkg.Structs {
		fields := make([]string, 0, len(st.Fields))
		for _, field := range st.Fields {
			fields = append(fields, field.String())
		}
		methods := make([]string, 0, len(st.Methods))
		for _, method := range st.Methods {
			methods = append(methods, method.Signature())
		}
		structs = append(structs, domain.NameWithTypeParams(st.Name, st.TypeParams)+" fields "+strings.Join(fields, ", ")+" methods "+strings.Join(methods, "; "))
	}
	addNamed("structs", structs)

	funcs := make([]string, 0, len(pkg.Functions))
	for _, fn := range pkg.Functions {
		funcs = append(funcs, fn.Signature())
	}
	addNamed("functions", funcs)

	types := make([]string, 0, len(pkg.TypeDefs))
	for _, typ := range pkg.TypeDefs {
		types = append(types, domain.NameWithTypeParams(typ.Name, typ.TypeParams)+" "+typ.UnderlyingType.String())
	}
	addNamed("types", types)

	deps := map[string]bool{}
	for _, dep := range pkg.Dependencies {
		if dep.To.External || dep.To.Package == "" || dep.To.Package == pkg.Path {
			continue
		}
		deps[dep.To.Package] = true
	}
	addNamed("depends on", sortedBoolKeys(deps))

	return strings.Join(parts, ". ")
}

func writeArchMotifPackageGraphMLFile(path string, graph archMotifPackageGraph) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeArchMotifPackageGraphML(f, graph)
}

func writeArchMotifPackageGraphML(w io.Writer, graph archMotifPackageGraph) error {
	if _, err := fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, `<graphml xmlns="http://graphml.graphdrawing.org/xmlns">`); err != nil {
		return err
	}
	for _, line := range []string{
		`  <key id="label" for="node" attr.name="label" attr.type="string"/>`,
		`  <key id="group" for="node" attr.name="group" attr.type="string"/>`,
		`  <key id="kind" for="node" attr.name="kind" attr.type="string"/>`,
		`  <key id="doc" for="node" attr.name="doc" attr.type="string"/>`,
		`  <key id="weight" for="edge" attr.name="weight" attr.type="double"/>`,
		`  <key id="e_kind" for="edge" attr.name="kind" attr.type="string"/>`,
		`  <graph edgedefault="directed">`,
	} {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	for _, id := range sortedNodeIDs(graph.Nodes) {
		node := graph.Nodes[id]
		if _, err := fmt.Fprintf(w, "    <node id=\"%s\">", xmlEscapeAttr(id)); err != nil {
			return err
		}
		for _, data := range []struct {
			key   string
			value string
		}{
			{"label", node.Name},
			{"group", node.Group},
			{"kind", "package"},
			{"doc", node.Doc},
		} {
			if err := writeGraphMLData(w, data.key, data.value); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, "</node>"); err != nil {
			return err
		}
	}
	for i, edge := range graph.Edges {
		if _, err := fmt.Fprintf(w, "    <edge id=\"e%d\" source=\"%s\" target=\"%s\">", i, xmlEscapeAttr(edge.From), xmlEscapeAttr(edge.To)); err != nil {
			return err
		}
		if err := writeGraphMLData(w, "e_kind", "depends_on"); err != nil {
			return err
		}
		if err := writeGraphMLData(w, "weight", fmt.Sprintf("%d", edge.Weight)); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "</edge>"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "  </graph>"); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, "</graphml>")
	return err
}

func writeGraphMLData(w io.Writer, key, value string) error {
	if _, err := fmt.Fprintf(w, "<data key=%q>", key); err != nil {
		return err
	}
	if err := xml.EscapeText(w, []byte(value)); err != nil {
		return err
	}
	_, err := fmt.Fprint(w, "</data>")
	return err
}

func archMotifEmbeddingInfo(root, bin string) archMotifEmbeddingStatus {
	artifactDir := archMotifArtifactDir(root)
	textGraph := filepath.Join(artifactDir, "packages-text.graphml")
	vectorGraph := filepath.Join(artifactDir, "packages-vec.graphml")
	info := archMotifEmbeddingStatus{
		TextGraphPath:   textGraph,
		VectorGraphPath: vectorGraph,
		HasTextGraph:    fileExists(textGraph),
		HasVectors:      fileExists(vectorGraph),
		Binary:          bin,
	}
	if stat, err := os.Stat(vectorGraph); err == nil {
		info.UpdatedAt = stat.ModTime().Format(time.RFC3339)
	}
	if info.HasVectors {
		info.VectorCount = countGraphMLVectors(vectorGraph)
	}
	return info
}

func archMotifArtifactDir(root string) string {
	if root == "" {
		return filepath.Join(".arch", "archmotif")
	}
	return filepath.Join(root, ".arch", "archmotif")
}

func resolveArchMotifBin() (string, error) {
	if env := os.Getenv("ARCHMOTIF_BIN"); env != "" {
		if isExecutableFile(env) {
			return env, nil
		}
		return "", fmt.Errorf("ARCHMOTIF_BIN is not executable: %s", env)
	}
	candidates := []string{}
	if home := os.Getenv("HOME"); home != "" {
		candidates = append(candidates,
			filepath.Join(home, "dev", "tools", "archmotif", "bin", "archmotif"),
			filepath.Join(home, "bin", "archmotif"),
		)
	}
	for _, candidate := range candidates {
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath("archmotif"); err == nil {
		return path, nil
	}
	return "", errors.New("archmotif binary not found; set ARCHMOTIF_BIN or install archmotif on PATH")
}

func countGraphMLVectors(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), `<data key="vec">`)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func isExecutableFile(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir() && stat.Mode()&0o111 != 0
}

func packageDisplayName(pkg domain.PackageModel) string {
	if pkg.Name != "" {
		return pkg.Name
	}
	if pkg.Path == "." || pkg.Path == "" {
		return "(root)"
	}
	return pathBase(pkg.Path)
}

func packageGroup(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" || path == "." {
		return "(root)"
	}
	if len(parts) >= 2 && parts[0] == "internal" {
		if parts[1] == "plugins" {
			return "plugins"
		}
		return "internal/" + parts[1]
	}
	return parts[0]
}

func sortedNodeIDs(nodes map[string]archMotifPackageNode) []string {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedMapKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func roundFloat(v float64, digits int) float64 {
	scale := math.Pow10(digits)
	return math.Round(v*scale) / scale
}

func xmlEscapeAttr(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	escaped := buf.String()
	escaped = strings.ReplaceAll(escaped, `"`, "&quot;")
	escaped = strings.ReplaceAll(escaped, `'`, "&apos;")
	return escaped
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
