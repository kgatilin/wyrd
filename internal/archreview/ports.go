package archreview

import (
	"sort"
	"strings"

	"github.com/kgatilin/wyrd/internal/overlay"
)

// portRules decides whether an exported symbol is reached from outside the
// dependency graph. Three rules, none of them a guess:
//
//  1. Language visibility. Only packages with an "internal" path element are
//     checked at all; anything else is importable by the world and is a port
//     by Go's own rule. Go's other entry points — package main's main, and
//     every init — are ports for the same reason: the runtime calls them.
//  2. Overlay declaration. wyrd.yaml's ports.external names the symbols
//     reached by plugin hooks, reflection, registration or generated code.
//  3. Graph evidence. A method of a type that implements an interface
//     declaring that method is used through the interface, and the implements
//     edge is in the graph.
type portRules struct {
	external []overlay.ExternalPort
}

func newPortRules(cfg *overlay.Config) portRules {
	if cfg == nil {
		return portRules{}
	}
	rules := portRules{}
	for _, raw := range cfg.Ports.External {
		// A selector that does not parse was already reported by overlay
		// validation; here it simply covers nothing, which fails towards
		// reporting a symbol rather than towards hiding one.
		if port, err := overlay.ParseExternalPort(raw); err == nil {
			rules.external = append(rules.external, port)
		}
	}
	return rules
}

// isPort reports whether the symbol is reachable from outside the graph, so
// the absence of an incoming edge says nothing about it.
func (p portRules) isPort(sym symbol, s *side) bool {
	if sym.exported && worldVisible(sym.pkg) {
		return true
	}
	if isGoEntryPoint(sym, s) {
		return true
	}
	if s.viaIface[sym.node] {
		return true
	}
	for _, port := range p.external {
		if sym.isMember() {
			if port.Matches(sym.pkg, sym.recv, sym.name) {
				return true
			}
			continue
		}
		if port.Matches(sym.pkg, sym.name, "") {
			return true
		}
	}
	return false
}

// worldVisible reports whether Go's visibility rule lets code outside the
// module import the package's exports. A path element named "internal" is what
// takes that away.
func worldVisible(pkg string) bool {
	for _, segment := range strings.Split(pkg, "/") {
		if segment == "internal" {
			return false
		}
	}
	return true
}

// isGoEntryPoint reports whether the runtime, rather than any caller in the
// graph, invokes the symbol: every init, and main in package main.
func isGoEntryPoint(sym symbol, s *side) bool {
	if sym.kind != "func" {
		return false
	}
	if sym.name == "init" {
		return true
	}
	return sym.name == "main" && s.byPath[sym.pkg].Name == "main"
}

// exportFinding is one exported symbol nothing in the graph reaches. Dead
// means no incoming edge at all, not merely none from another package — a
// different state with a different action, so the two never share a row.
type exportFinding struct {
	sym  symbol
	dead bool
}

// unusedExports scans a side for exported symbols with no fan-in from another
// package. include narrows the scan (review mode passes it the symbols the
// branch added); a nil include scans everything.
func unusedExports(s *side, ports portRules, include func(symbol) bool) []exportFinding {
	var out []exportFinding
	for _, node := range s.order {
		sym := s.symbols[node]
		if !sym.exported || worldVisible(sym.pkg) {
			continue
		}
		if include != nil && !include(sym) {
			continue
		}
		if s.crossIn(node) > 0 || ports.isPort(sym, s) {
			continue
		}
		out = append(out, exportFinding{sym: sym, dead: s.inbound[node] == 0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sym.label() < out[j].sym.label() })
	return out
}

// orphans scans a side for symbols with no incoming edge at all, under the
// same port rules — an added symbol nothing references is either dead on
// arrival or not wired up yet.
func orphans(s *side, ports portRules, include func(symbol) bool) []symbol {
	var out []symbol
	for _, node := range s.order {
		sym := s.symbols[node]
		if include != nil && !include(sym) {
			continue
		}
		if s.inbound[node] > 0 || ports.isPort(sym, s) {
			continue
		}
		out = append(out, sym)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].label() < out[j].label() })
	return out
}
