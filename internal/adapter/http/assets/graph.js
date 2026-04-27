// graph.js — hydrates every .cy-graph <div> on the page. Reads either
// `data-graph` (inline JSON payload) or `data-api` (URL to fetch) and
// mounts a Cytoscape.js instance with the view's preset styling.
//
// Added for M7d (#24); extended for M8 (#46) to cover layer-map,
// package-overview and diff-overlay views. All display graphs are now
// rendered client-side; D2 remains only for export (server emits
// /view/.../d2 and /view/.../svg on demand).
//
// The script is tolerant: when Cytoscape or a layout plugin is missing,
// we fall back to a cose layout (cytoscape built-in) or display a short
// hint so the page is never blank.
//
// ============================================================================
// SHARED GRAPH STYLE — SINGLE SOURCE OF TRUTH (#90)
// ============================================================================
// All browser diagrams (type-detail, layer-map, layer-map-mini,
// package-overview, bc-map, bc-map-mini, diff-overlay) share one
// D2-like visual language defined by `defaultGraphDisplay()` below:
//
//   - palette       : neutral ink/border/panel colours and accents
//   - semantic      : named colours for kind/edge meaning (green=allowed,
//                     red=violation, etc.)
//   - node.d2       : the shared base node style (white panel,
//                     heavy ink border, wrapped left-justified label)
//   - node.layer    : compound layer container style (top-centered title)
//   - node.packageChip / kinds.packageContainer{,Soft} : compound
//                     package container variants
//   - edge.d2       : the shared base edge style (bezier arrow, ink
//                     label on white background)
//   - kinds         : per-kind border colour / shape adjustments (only
//                     semantic differences, never a separate preset)
//   - edges         : edge styles by kind (allowed / violation / ...)
//   - relationships : BC-map relationship qualifiers (#81)
//   - hover         : the single `.cy-hover` accent applied on mouseover
//
// To change the look of every browser graph, edit `defaultGraphDisplay()`.
// To add semantic meaning for a new kind / edge type, add an entry to
// `kinds` / `edges` / `relationships` and reference it from the relevant
// view's `registerView(...)` call below.
//
// IMPORTANT: views must NOT introduce their own dark/fill node/edge
// preset, override the shared base style with arbitrary colours, or
// duplicate any style block. The HTTP adapter tests guard against
// that — see `TestLayerGraphStyleUsesSharedD2LikePreset` and
// `TestDashboardDomainGraphStyleUsesCytoscapeD2LikePreset`.
// ============================================================================
(function () {
    'use strict';

    // registry[view] -> { style, layoutOpts, onTap? } used by render().
    var registry = {};

    /** Register a preset. fn(ctx) returns { style, layout, onTap }. */
    function registerView(name, fn) {
        registry[name] = fn;
    }

    // Ensure an extension is registered at most once per page-load.
    function registerOnce(flagName, plugin) {
        if (!plugin) {
            return false;
        }
        if (window[flagName]) {
            return true;
        }
        try {
            window.cytoscape.use(plugin);
            window[flagName] = true;
            return true;
        } catch (_err) {
            return false;
        }
    }

    function pickLayout(requested) {
        if (!window.cytoscape) {
            return 'cose';
        }
        if (requested === 'elk') {
            if (window.cytoscapeElk) {
                registerOnce('__archaiElkRegistered', window.cytoscapeElk);
                if (window.__archaiElkRegistered) {
                    return 'elk';
                }
            }
            // fall through to dagre if elk not available
            requested = 'dagre';
        }
        if (requested === 'dagre') {
            if (window.cytoscapeDagre) {
                registerOnce('__archaiDagreRegistered', window.cytoscapeDagre);
                if (window.__archaiDagreRegistered) {
                    return 'dagre';
                }
            }
            return 'cose';
        }
        return requested || 'cose';
    }

    function defaultGraphDisplay() {
        return {
            palette: {
                ink: '#111827',
                muted: '#475569',
                border: '#334155',
                panel: '#ffffff',
                packageFill: '#f8fafc',
                packageBorder: '#cbd5e1',
                accent: '#2563eb',
                accentWash: '#dbeafe'
            },
            semantic: {
                blue: '#2563eb',
                cyan: '#0ea5e9',
                purple: '#7c3aed',
                teal: '#0f766e',
                green: '#16a34a',
                greenDark: '#064e3b',
                red: '#dc2626',
                orange: '#d97706',
                slate: '#475569',
                slateDark: '#334155',
                gray: '#9ca3af',
                amber: '#b45309',
                sky: '#38bdf8',
                outbound: '#f97316'
            },
            layout: {
                layered: {
                    algorithm: 'layered',
                    'elk.edgeRouting': 'ORTHOGONAL',
                    'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
                    'elk.layered.cycleBreaking.strategy': 'GREEDY',
                    'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
                    'elk.layered.layering.strategy': 'NETWORK_SIMPLEX',
                    'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
                    'elk.spacing.nodeNode': '42',
                    'elk.layered.spacing.nodeNodeBetweenLayers': '96',
                    'elk.spacing.edgeNode': '32',
                    'elk.spacing.edgeEdge': '20'
                },
                free: {
                    algorithm: 'stress',
                    'elk.spacing.nodeNode': '48',
                    'elk.spacing.edgeNode': '28',
                    'elk.spacing.edgeEdge': '20',
                    'elk.hierarchyHandling': 'INCLUDE_CHILDREN'
                },
                dagre: {
                    nodeSep: 30,
                    rankSep: 60
                }
            },
            node: {
                d2: {
                    'background-color': 'palette.panel',
                    'border-color': 'palette.ink',
                    'border-width': 3,
                    'label': 'data(label)',
                    'color': 'palette.ink',
                    'text-valign': 'center',
                    'text-halign': 'center',
                    'text-outline-width': 0,
                    'font-size': 12,
                    'font-weight': 600,
                    'width': 'label',
                    'height': 'label',
                    'padding': 12,
                    'shape': 'round-rectangle',
                    'text-wrap': 'wrap',
                    'text-justification': 'left',
                    'text-max-width': 260,
                    'line-height': 1.25
                },
                layer: {
                    'background-color': 'palette.panel',
                    'background-opacity': 1,
                    'border-width': 3,
                    'border-color': 'palette.border',
                    'shape': 'round-rectangle',
                    'text-valign': 'top',
                    'text-halign': 'center',
                    'padding': 28,
                    'font-size': 14,
                    'font-weight': 700,
                    'color': 'palette.ink',
                    'text-justification': 'center'
                },
                packageChip: {
                    'background-color': 'palette.packageFill',
                    'border-color': 'palette.packageBorder',
                    'border-width': 1.5,
                    'font-size': 10,
                    'font-weight': 500,
                    'padding': 8,
                    'text-wrap': 'wrap',
                    'text-justification': 'center',
                    'text-max-width': 180,
                    'width': 190,
                    'height': 'label'
                },
                layerMini: {
                    'background-color': 'palette.panel',
                    'background-opacity': 1,
                    'border-color': 'palette.border',
                    'border-width': 3,
                    'shape': 'round-rectangle',
                    'text-valign': 'center',
                    'text-halign': 'center',
                    'padding': 10,
                    'font-size': 11,
                    'font-weight': 700,
                    'color': 'palette.ink',
                    'text-justification': 'center',
                    'width': 'label',
                    'height': 'label'
                }
            },
            edge: {
                d2: {
                    'curve-style': 'bezier',
                    'target-arrow-shape': 'triangle',
                    'line-color': 'palette.muted',
                    'target-arrow-color': 'palette.muted',
                    'label': 'data(label)',
                    'font-size': 10,
                    'font-weight': 500,
                    'color': 'palette.ink',
                    'text-background-color': 'palette.panel',
                    'text-background-opacity': 0.9,
                    'text-background-padding': 3,
                    'width': 3,
                    'z-index': 10
                }
            },
            kinds: {
                root: { 'border-color': 'semantic.blue' },
                interface: { 'border-color': 'semantic.cyan' },
                struct: { 'border-color': 'semantic.purple' },
                function: { 'border-color': 'semantic.teal' },
                package: { 'border-color': 'semantic.purple' },
                packageContainer: { 'background-color': 'palette.panel', 'border-color': 'palette.border', 'border-width': 3, 'shape': 'round-rectangle', 'padding': 14, 'background-opacity': 0.92 },
                packageContainerSoft: { 'background-color': 'palette.panel', 'border-color': 'palette.packageBorder', 'border-width': 2, 'shape': 'round-rectangle', 'padding': 14, 'background-opacity': 0.92 },
                packageIn: { 'border-color': 'semantic.sky' },
                packageOut: { 'border-color': 'semantic.outbound' },
                cycle: { 'border-color': 'semantic.amber' },
                depthLimit: { 'border-color': 'semantic.slate' },
                entryPoint: { 'border-color': 'semantic.green', 'border-width': 3, 'shape': 'round-rectangle', 'font-weight': 'bold' },
                bc: { 'border-color': 'palette.ink', 'shape': 'round-rectangle', 'padding': 12, 'font-size': 12 }
            },
            edges: {
                allowed: { 'line-color': 'semantic.green', 'target-arrow-color': 'semantic.green' },
                violation: { 'line-color': 'semantic.red', 'target-arrow-color': 'semantic.red', 'width': 4 },
                declared: { 'line-color': 'semantic.gray', 'target-arrow-color': 'semantic.gray', 'line-style': 'dashed', 'opacity': 0.6 },
                declaredPreview: { 'line-color': 'semantic.gray', 'target-arrow-color': 'semantic.gray', 'line-style': 'dashed', 'opacity': 0.75 },
                inbound: { 'line-color': 'semantic.sky', 'target-arrow-color': 'semantic.sky' },
                outbound: { 'line-color': 'semantic.outbound', 'target-arrow-color': 'semantic.outbound' },
                add: { 'line-color': 'semantic.green', 'target-arrow-color': 'semantic.green' },
                remove: { 'line-color': 'semantic.red', 'target-arrow-color': 'semantic.red', 'line-style': 'dashed' },
                change: { 'line-color': 'semantic.orange', 'target-arrow-color': 'semantic.orange' }
            },
            relationships: {
                'shared-kernel': { 'line-color': 'semantic.purple', 'target-arrow-color': 'semantic.purple' },
                'customer-supplier': { 'line-color': 'semantic.blue', 'target-arrow-color': 'semantic.blue' },
                'conformist': { 'line-color': 'semantic.gray', 'target-arrow-color': 'semantic.gray' },
                'acl': { 'line-color': 'semantic.red', 'target-arrow-color': 'semantic.red', 'line-style': 'dashed' },
                'open-host': { 'line-color': 'semantic.green', 'target-arrow-color': 'semantic.green' }
            },
            hover: {
                'border-color': 'palette.accent',
                'underlay-color': 'palette.accentWash',
                'underlay-opacity': 0.85,
                'underlay-padding': 8
            }
        };
    }

    function isPlainObject(value) {
        return value && typeof value === 'object' && !Array.isArray(value);
    }

    function deepMerge(base, override) {
        var out = {};
        var k;
        for (k in base) {
            if (Object.prototype.hasOwnProperty.call(base, k)) {
                out[k] = isPlainObject(base[k]) ? deepMerge(base[k], {}) : base[k];
            }
        }
        for (k in (override || {})) {
            if (Object.prototype.hasOwnProperty.call(override, k)) {
                if (isPlainObject(out[k]) && isPlainObject(override[k])) {
                    out[k] = deepMerge(out[k], override[k]);
                } else {
                    out[k] = override[k];
                }
            }
        }
        return out;
    }

    var graphDisplay = deepMerge(defaultGraphDisplay(), window.archaiGraphDisplay || {});
    window.archaiGraphDisplay = graphDisplay;

    function displayValue(value) {
        if (typeof value !== 'string') { return value; }
        var parts = value.split('.');
        if (parts.length !== 2) { return value; }
        var group = graphDisplay[parts[0]];
        if (!group) { return value; }
        return Object.prototype.hasOwnProperty.call(group, parts[1]) ? group[parts[1]] : value;
    }

    function resolveStyle(style) {
        var out = {};
        for (var k in style) {
            if (Object.prototype.hasOwnProperty.call(style, k)) {
                out[k] = displayValue(style[k]);
            }
        }
        return out;
    }

    function extendStyle() {
        var out = {};
        for (var i = 0; i < arguments.length; i++) {
            var style = arguments[i] || {};
            for (var k in style) {
                if (Object.prototype.hasOwnProperty.call(style, k)) {
                    out[k] = style[k];
                }
            }
        }
        return out;
    }

    function styleRule(selector, style) {
        return { selector: selector, style: resolveStyle(style) };
    }

    function nodeStyle(name, selector, extra) {
        return styleRule(selector || 'node', extendStyle(graphDisplay.node[name] || {}, extra));
    }

    function edgeStyle(name, selector, extra) {
        return styleRule(selector || 'edge', extendStyle(graphDisplay.edge[name] || {}, extra));
    }

    function kindNodeStyle(kind, styleName, extra) {
        return styleRule('node[kind = "' + kind + '"]', extendStyle(graphDisplay.kinds[styleName || kind] || {}, extra));
    }

    function opNodeStyle(op, colorName) {
        return styleRule('node[op = "' + op + '"]', { 'border-color': 'semantic.' + colorName });
    }

    function kindEdgeStyle(kind, styleName, extra) {
        return styleRule('edge[kind = "' + kind + '"]', extendStyle(graphDisplay.edges[styleName || kind] || {}, extra));
    }

    function relationshipEdgeStyle(relationship) {
        return styleRule('edge[relationship = "' + relationship + '"]', graphDisplay.relationships[relationship] || {});
    }

    function layeredLayout(direction, padding) {
        direction = normalizeLayoutDirection(direction);
        var elk = extendStyle(graphDisplay.layout.layered);
        if (direction) {
            elk['elk.direction'] = direction;
        }
        return {
            name: pickLayout('elk'),
            'elk': elk,
            padding: padding || 20
        };
    }

    function elkFreeLayout(algorithm, padding) {
        var elk = extendStyle(graphDisplay.layout.free, { algorithm: algorithm || graphDisplay.layout.free.algorithm || 'stress' });
        return {
            name: pickLayout('elk'),
            'elk': elk,
            padding: padding || 20
        };
    }

    function normalizeLayoutDirection(direction) {
        var d = String(direction || '').toUpperCase();
        if (d === 'RIGHT' || d === 'LR') { return 'RIGHT'; }
        if (d === 'DOWN' || d === 'TB') { return 'DOWN'; }
        return '';
    }

    function normalizeLayoutMode(mode) {
        var m = String(mode || '').toLowerCase();
        if (m === 'smart' || m === 'auto' || m === '') { return 'auto'; }
        if (m === 'vertical' || m === 'down' || m === 'tb') { return 'vertical'; }
        if (m === 'horizontal' || m === 'right' || m === 'lr') { return 'horizontal'; }
        if (m === 'relaxed' || m === 'stress') { return 'stress'; }
        return 'auto';
    }

    function graphLayoutKey(el, view) {
        var key = el.getAttribute('data-layout-key') || view || el.getAttribute('data-view') || 'default';
        return key;
    }

    function graphLayoutStorageKey(el, view) {
        return 'archai.graph.layoutMode.' + graphLayoutKey(el, view);
    }

    function getLayoutMode(el, view) {
        var fallback = normalizeLayoutMode(el.getAttribute('data-layout-mode'));
        try {
            var stored = window.localStorage && window.localStorage.getItem(graphLayoutStorageKey(el, view));
            return normalizeLayoutMode(stored || fallback);
        } catch (_err) {
            return fallback;
        }
    }

    function setLayoutMode(el, view, mode) {
        var normalized = normalizeLayoutMode(mode);
        el.setAttribute('data-layout-mode', normalized);
        try {
            if (window.localStorage) {
                window.localStorage.setItem(graphLayoutStorageKey(el, view), normalized);
            }
        } catch (_err) { /* storage can be unavailable in private contexts */ }
        return normalized;
    }

    function layerMapLayout(mode) {
        switch (normalizeLayoutMode(mode)) {
            case 'vertical':
                return layeredLayout('DOWN', 28);
            case 'horizontal':
                return layeredLayout('RIGHT', 28);
            case 'stress':
                return elkFreeLayout('stress', 28);
            default:
                return layeredLayout('', 28);
        }
    }

    function layerMapEdgeAxis(mode) {
        switch (normalizeLayoutMode(mode)) {
            case 'vertical':
                return 'vertical';
            case 'horizontal':
                return 'horizontal';
            default:
                return '';
        }
    }

    function d2LikeNodeStyle() {
        return nodeStyle('d2');
    }

    function d2LikeEdgeStyle(taxiDirection) {
        var useTaxi = taxiDirection === 'vertical' || taxiDirection === 'horizontal';
        var style = extendStyle(graphDisplay.edge.d2, { 'curve-style': useTaxi ? 'taxi' : 'bezier' });
        if (useTaxi) {
            style['taxi-direction'] = taxiDirection;
            style['taxi-turn'] = 24;
        }
        return edgeStyle('d2', 'edge', style);
    }

    // Type-detail view (M7d): kept as the default preset so existing
    // pages keep working with no template changes.
    registerView('type-detail', function () {
        return {
            layout: { name: pickLayout('dagre'), rankDir: 'LR', padding: 10 },
            style: [
                d2LikeNodeStyle(),
                styleRule('node[root]', graphDisplay.kinds.root),
                kindNodeStyle('interface'),
                kindNodeStyle('package'),
                kindNodeStyle('cycle'),
                kindNodeStyle('depth-limit', 'depthLimit'),
                d2LikeEdgeStyle('horizontal')
            ]
        };
    });

    // Layer map (full, /layers). Compound layer nodes; edges coloured
    // by kind ("allowed" / "violation" / "declared").
    registerView('layer-map', function (ctx) {
        var mode = normalizeLayoutMode(ctx && ctx.layoutMode);
        return {
            layout: layerMapLayout(mode),
            style: [
                d2LikeNodeStyle(),
                nodeStyle('layer', 'node[kind = "layer"]'),
                nodeStyle('packageChip', 'node[kind = "package"]'),
                d2LikeEdgeStyle(layerMapEdgeAxis(mode)),
                kindEdgeStyle('allowed'),
                kindEdgeStyle('violation'),
                kindEdgeStyle('declared')
            ]
        };
    });

    // Dashboard mini layer-map: layers only, no children, no
    // interactions (click-through navigates to /layers via the parent
    // <a>). Uses the shared `layerMini` node variant defined in
    // graphDisplay.node so the dashboard mini map stays visually in
    // line with the rest of the system (#90).
    registerView('layer-map-mini', function () {
        return {
            layout: layeredLayout('DOWN', 16),
            style: [
                d2LikeNodeStyle(),
                nodeStyle('layerMini', 'node[kind = "layer"]'),
                d2LikeEdgeStyle('vertical'),
                kindEdgeStyle('allowed'),
                kindEdgeStyle('violation'),
                kindEdgeStyle('declared', 'declaredPreview')
            ]
        };
    });

    // Package overview (M8): subject package at centre with exported
    // types as children, inbound/outbound packages around it.
    registerView('package-overview', function () {
        return {
            layout: extendStyle({ name: pickLayout('dagre'), rankDir: 'LR', padding: 10 }, graphDisplay.layout.dagre),
            style: [
                d2LikeNodeStyle(),
                styleRule('node[root]', graphDisplay.kinds.root),
                kindNodeStyle('interface'),
                kindNodeStyle('struct'),
                kindNodeStyle('function'),
                // M9 (#61): exported factories / constructors are the
                // "entry points" of a package's public surface; render
                // them with a distinct green fill and a bold dark border
                // so they stand out from regular functions.
                kindNodeStyle('entry-point', 'entryPoint'),
                kindNodeStyle('package', 'packageContainer'),
                kindNodeStyle('package-in', 'packageIn'),
                kindNodeStyle('package-out', 'packageOut'),
                d2LikeEdgeStyle(),
                kindEdgeStyle('inbound'),
                kindEdgeStyle('outbound')
            ]
        };
    });

    // Package dependency graph (#89): subject package at centre, project
    // packages it depends on (outbound) and packages that depend on it
    // (inbound) shown around it. Externals are surfaced outside the graph.
    registerView('package-deps', function () {
        return {
            layout: extendStyle({ name: pickLayout('dagre'), rankDir: 'LR', padding: 10 }, graphDisplay.layout.dagre),
            style: [
                d2LikeNodeStyle(),
                styleRule('node[root]', graphDisplay.kinds.root),
                kindNodeStyle('package', 'packageContainer'),
                kindNodeStyle('package-in', 'packageIn'),
                kindNodeStyle('package-out', 'packageOut'),
                d2LikeEdgeStyle(),
                kindEdgeStyle('inbound'),
                kindEdgeStyle('outbound')
            ]
        };
    });

    // Bounded-context map (#81). Each node is one BC; the relationship
    // qualifier (shared-kernel / customer-supplier / conformist / acl /
    // open-host) is exposed as data on the edge so it can be styled
    // and surfaced as a label. The optional description is exposed via
    // a native title tooltip (set in attachInteractions) so wrapping it
    // into the label can no longer cause clipping.
    registerView('bc-map', function () {
        return {
            layout: layeredLayout('RIGHT', 20),
            style: [
                d2LikeNodeStyle(),
                kindNodeStyle('bc'),
                d2LikeEdgeStyle('horizontal'),
                { selector: 'edge', style: { 'label': 'data(relationship)' } },
                relationshipEdgeStyle('shared-kernel'),
                relationshipEdgeStyle('customer-supplier'),
                relationshipEdgeStyle('conformist'),
                relationshipEdgeStyle('acl'),
                relationshipEdgeStyle('open-host')
            ]
        };
    });

    // Dashboard mini variant of bc-map: Cytoscape data and layout, but
    // with the same white-node / heavy-stroke visual language as D2.
    registerView('bc-map-mini', function () {
        return {
            layout: layeredLayout('RIGHT', 16),
            style: [
                d2LikeNodeStyle(),
                kindNodeStyle('bc'),
                d2LikeEdgeStyle('horizontal'),
                { selector: 'edge', style: { 'label': 'data(relationship)' } },
                relationshipEdgeStyle('shared-kernel'),
                relationshipEdgeStyle('customer-supplier'),
                relationshipEdgeStyle('conformist'),
                relationshipEdgeStyle('acl'),
                relationshipEdgeStyle('open-host')
            ]
        };
    });

    // Diff overlay (M8): per-change node, coloured by op, parented by
    // the pseudo-package node the change lives under.
    registerView('diff-overlay', function () {
        return {
            layout: { name: pickLayout('dagre'), rankDir: 'TB', padding: 10 },
            style: [
                d2LikeNodeStyle(),
                opNodeStyle('add', 'green'),
                opNodeStyle('remove', 'red'),
                opNodeStyle('change', 'orange'),
                kindNodeStyle('package', 'packageContainerSoft'),
                d2LikeEdgeStyle('vertical'),
                kindEdgeStyle('add'),
                kindEdgeStyle('remove'),
                kindEdgeStyle('change')
            ]
        };
    });

    // ---- runtime --------------------------------------------------

    function hydrateElements(payload) {
        var elements = [];
        (payload.nodes || []).forEach(function (n) {
            var data = {
                id: n.id,
                label: n.label || n.id,
                kind: n.kind || '',
                root: !!n.root
            };
            if (n.parent) { data.parent = n.parent; }
            if (n.op) { data.op = n.op; }
            // #81: description is a separate field on the node so the
            // front-end can show it as a tooltip without bloating the
            // label and triggering clipping.
            if (n.description) { data.description = n.description; }
            elements.push({ group: 'nodes', data: data });
        });
        (payload.edges || []).forEach(function (e, i) {
            var edgeData = {
                id: 'e' + i + ':' + e.source + '->' + e.target,
                source: e.source,
                target: e.target,
                label: e.label || '',
                kind: e.kind || ''
            };
            if (e.details) {
                edgeData.details = e.details;
            }
            // #81: BC-graph relationship qualifier surfaces on the edge.
            if (e.relationship) { edgeData.relationship = e.relationship; }
            elements.push({ group: 'edges', data: edgeData });
        });
        return elements;
    }

    function attachInteractions(cy, el, interactive) {
        if (interactive === false) {
            cy.userZoomingEnabled(false);
            cy.userPanningEnabled(false);
            cy.boxSelectionEnabled(false);
            return;
        }
        var isClickableNode = function (node) {
            var id = node.id();
            return id.indexOf('pkg:') === 0 || id.indexOf('type:') === 0;
        };
        // Click → navigate (package / type). The node's id encodes a
        // routing prefix ("pkg:" / "type:"). Compound layer containers
        // are visual grouping only, so hovering a large layer block does
        // not constantly toggle highlight while moving between packages.
        cy.on('tap', 'node', function (evt) {
            var id = evt.target.id();
            if (id.indexOf('pkg:') === 0) {
                window.location.href = '/packages/' + id.slice(4);
            } else if (id.indexOf('type:') === 0) {
                window.location.href = '/types/' + id.slice(5);
            }
        });
        // Hover highlight: only accent the clickable block under the
        // pointer. Do not fade the rest of the graph; on dense compound
        // graphs that creates a full-canvas flicker while moving across
        // labels and layer containers.
        cy.on('mouseover', 'node', function (evt) {
            var n = evt.target;
            if (isClickableNode(n)) {
                n.addClass('cy-hover');
                el.style.cursor = 'pointer';
            }
            // Surface the optional `description` field as a native
            // browser tooltip, so BC nodes (#81) can show it without
            // inflating the node label.
            var desc = n.data('description');
            if (desc) {
                el.setAttribute('title', desc);
            }
        });
        cy.on('mouseout', 'node', function (evt) {
            evt.target.removeClass('cy-hover');
            el.style.cursor = '';
            el.removeAttribute('title');
        });
    }

    function syncLayoutModeControls(toolbar, mode) {
        var normalized = normalizeLayoutMode(mode);
        var controls = toolbar.querySelectorAll('[data-cy-action="set-layout-mode"]');
        for (var i = 0; i < controls.length; i++) {
            if (controls[i].tagName === 'SELECT') {
                controls[i].value = normalized;
            }
        }
    }

    function clearZoomOverlay(el) {
        var parent = el.parentElement;
        if (!parent) { return; }
        var overlay = parent.querySelector('.cy-zoom-controls');
        if (overlay) {
            parent.removeChild(overlay);
        }
        delete el.dataset.cyZoomOverlay;
    }

    function attachToolbar(el, cy, view, rerender) {
        // The toolbar is the previous sibling of el when present.
        var toolbar = el.previousElementSibling;
        if (!toolbar || !toolbar.classList.contains('cy-toolbar')) {
            // Or a sibling at the same level inside the card; fall back
            // to a broader lookup within the parent.
            var parent = el.parentElement;
            if (parent) {
                toolbar = parent.querySelector('.cy-toolbar');
            }
        }
        if (!toolbar) { return; }
        syncLayoutModeControls(toolbar, getLayoutMode(el, view));
        var actions = toolbar.querySelectorAll('[data-cy-action]');
        for (var i = 0; i < actions.length; i++) {
            (function (btn) {
                var eventName = btn.tagName === 'SELECT' ? 'change' : 'click';
                if (btn.__archaiCyHandler) {
                    btn.removeEventListener(btn.__archaiCyEventName || 'click', btn.__archaiCyHandler);
                }
                btn.__archaiCyHandler = function (ev) {
                    ev.preventDefault();
                    switch (btn.getAttribute('data-cy-action')) {
                        case 'fit':
                            cy.fit(null, 20);
                            break;
                        case 'zoom-in':
                            cy.zoom({ level: cy.zoom() * 1.25, renderedPosition: { x: el.clientWidth / 2, y: el.clientHeight / 2 } });
                            break;
                        case 'zoom-out':
                            cy.zoom({ level: cy.zoom() / 1.25, renderedPosition: { x: el.clientWidth / 2, y: el.clientHeight / 2 } });
                            break;
                        case 'set-layout-mode':
                            var mode = setLayoutMode(el, view, btn.value || btn.getAttribute('data-layout-mode'));
                            syncLayoutModeControls(toolbar, mode);
                            if (rerender) {
                                rerender();
                            }
                            break;
                        case 'export-png':
                            try {
                                var uri = cy.png({ full: true, scale: 2, bg: graphDisplay.palette.panel });
                                var a = document.createElement('a');
                                a.href = uri;
                                a.download = 'graph.png';
                                document.body.appendChild(a);
                                a.click();
                                document.body.removeChild(a);
                            } catch (_err) { /* silent */ }
                            break;
                        case 'set-mode':
                            // M9 (#61): switch overview detail mode
                            // (public ↔ full). The server renders the
                            // page differently per ?mode= query, so the
                            // simplest deterministic path is to update
                            // the URL and reload. Public is the default
                            // so dropping the param is fine.
                            try {
                                var mode = btn.getAttribute('data-mode') || 'public';
                                var url = new URL(window.location.href);
                                if (mode === 'full') {
                                    url.searchParams.set('mode', 'full');
                                } else {
                                    url.searchParams.delete('mode');
                                }
                                window.location.assign(url.toString());
                            } catch (_err) { /* silent */ }
                            break;
                    }
                };
                btn.__archaiCyEventName = eventName;
                btn.addEventListener(eventName, btn.__archaiCyHandler);
            })(actions[i]);
        }
    }

    function applyHeight(el) {
        var h = el.getAttribute('data-height');
        if (h) { el.style.height = (parseInt(h, 10) || 300) + 'px'; }
        if (!el.style.height) { el.style.height = '300px'; }
    }

    // ensureZoomOverlay (#73): inject a small +/- stack into the
    // bottom-right corner of every cytoscape container so touch users
    // can zoom even when the toolbar above the graph is scrolled out
    // of view. Skipped when interactive=false or already added.
    function ensureZoomOverlay(el, cy) {
        if (el.dataset.cyZoomOverlay === 'on') { return; }
        el.dataset.cyZoomOverlay = 'on';
        var parent = el.parentElement;
        if (!parent) { return; }
        if (!parent.classList.contains('cy-graph-wrap')) {
            parent.classList.add('cy-graph-wrap');
        }
        var overlay = document.createElement('div');
        overlay.className = 'cy-zoom-controls';
        var mkBtn = function (label, ariaLabel, fn) {
            var b = document.createElement('button');
            b.type = 'button';
            b.className = 'cy-btn';
            b.textContent = label;
            b.setAttribute('aria-label', ariaLabel);
            b.addEventListener('click', function (ev) {
                ev.preventDefault();
                fn();
            });
            return b;
        };
        overlay.appendChild(mkBtn('+', 'Zoom in', function () {
            cy.zoom({ level: cy.zoom() * 1.25, renderedPosition: { x: el.clientWidth / 2, y: el.clientHeight / 2 } });
        }));
        overlay.appendChild(mkBtn('\u2212', 'Zoom out', function () {
            cy.zoom({ level: cy.zoom() / 1.25, renderedPosition: { x: el.clientWidth / 2, y: el.clientHeight / 2 } });
        }));
        overlay.appendChild(mkBtn('\u25A1', 'Fit', function () { cy.fit(null, 20); }));
        parent.appendChild(overlay);
    }

    function renderGraph(el) {
        applyHeight(el);
        var view = el.getAttribute('data-view') || 'type-detail';
        var interactive = el.getAttribute('data-interactive') !== 'false';
        var currentCy = null;
        var currentPayload = null;

        var makePreset = function () {
            var ctx = {
                el: el,
                view: view,
                layoutMode: getLayoutMode(el, view)
            };
            return (registry[view] || registry['type-detail'])(ctx);
        };

        var hydrate = function (payload) {
            currentPayload = payload;
            if (!window.cytoscape) {
                el.innerHTML = '<p class="muted small">(cytoscape.js not available)</p>';
                return;
            }
            if (currentCy) {
                currentCy.destroy();
                currentCy = null;
                clearZoomOverlay(el);
            }
            var elements = hydrateElements(payload);
            var preset = makePreset();
            // #73: tune touch interactions. wheelSensitivity reduces
            // jitter on trackpads + phones; touchTapThreshold avoids
            // accidental taps mid-pan. Pinch-to-zoom is enabled by
            // default in cytoscape.
            var cy = window.cytoscape({
                container: el,
                elements: elements,
                layout: preset.layout,
                wheelSensitivity: 0.2,
                touchTapThreshold: 8,
                style: preset.style.concat([
                    styleRule('.cy-hover', graphDisplay.hover)
                ])
            });
            currentCy = cy;
            attachInteractions(cy, el, interactive);
            attachToolbar(el, cy, view, function () {
                if (currentPayload) {
                    hydrate(currentPayload);
                }
            });
            if (interactive) {
                ensureZoomOverlay(el, cy);
            }
        };

        // Source 1: inline payload (M7d path).
        var raw = el.getAttribute('data-graph');
        if (raw) {
            try {
                hydrate(JSON.parse(raw));
            } catch (_err) {
                el.innerHTML = '<p class="muted small">(graph data not parseable)</p>';
            }
            return;
        }

        // Source 2: fetch from data-api.
        var api = el.getAttribute('data-api');
        if (!api) {
            el.innerHTML = '<p class="muted small">(no graph source)</p>';
            return;
        }
        fetch(api, { headers: { 'Accept': 'application/json' } })
            .then(function (resp) {
                if (!resp.ok) { throw new Error('HTTP ' + resp.status); }
                return resp.json();
            })
            .then(hydrate)
            .catch(function (err) {
                el.innerHTML = '<p class="muted small">(graph load failed: ' + (err && err.message ? err.message : 'error') + ')</p>';
            });
    }

    function init() {
        var nodes = document.querySelectorAll('.cy-graph');
        for (var i = 0; i < nodes.length; i++) {
            renderGraph(nodes[i]);
        }
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
