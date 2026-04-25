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

    function baseNodeStyle() {
        return {
            selector: 'node',
            style: {
                'background-color': '#64748b',
                'label': 'data(label)',
                'color': '#e2e8f0',
                'text-valign': 'center',
                'text-halign': 'center',
                'text-outline-color': '#1e293b',
                'text-outline-width': 2,
                'font-size': 11,
                'width': 'label',
                'height': 24,
                'padding': 6,
                'shape': 'round-rectangle'
            }
        };
    }

    function baseEdgeStyle() {
        return {
            selector: 'edge',
            style: {
                'curve-style': 'bezier',
                'target-arrow-shape': 'triangle',
                'line-color': '#94a3b8',
                'target-arrow-color': '#94a3b8',
                'label': 'data(label)',
                'font-size': 9,
                'color': '#64748b',
                'text-background-color': '#0f172a',
                'text-background-opacity': 0.7,
                'text-background-padding': 2,
                'width': 1.5
            }
        };
    }

    // Type-detail view (M7d): kept as the default preset so existing
    // pages keep working with no template changes.
    registerView('type-detail', function () {
        return {
            layout: { name: pickLayout('dagre'), rankDir: 'LR', padding: 10 },
            style: [
                baseNodeStyle(),
                { selector: 'node[root]', style: { 'background-color': '#2563eb' } },
                { selector: 'node[kind = "interface"]', style: { 'background-color': '#0ea5e9' } },
                { selector: 'node[kind = "package"]', style: { 'background-color': '#7c3aed' } },
                { selector: 'node[kind = "cycle"]', style: { 'background-color': '#b45309' } },
                { selector: 'node[kind = "depth-limit"]', style: { 'background-color': '#475569' } },
                baseEdgeStyle()
            ]
        };
    });

    // Layer map (full, /layers). Compound layer nodes; edges coloured
    // by kind ("allowed" / "violation" / "declared").
    registerView('layer-map', function () {
        return {
            layout: { name: pickLayout('elk'), 'elk': { algorithm: 'layered', 'elk.direction': 'RIGHT' }, padding: 20 },
            style: [
                baseNodeStyle(),
                {
                    selector: 'node[kind = "layer"]',
                    style: {
                        'background-color': '#1e293b',
                        'background-opacity': 0.35,
                        'border-width': 1,
                        'border-color': '#475569',
                        'shape': 'round-rectangle',
                        'text-valign': 'top',
                        'text-halign': 'center',
                        'padding': 16,
                        'font-size': 13,
                        'color': '#f1f5f9'
                    }
                },
                { selector: 'node[kind = "package"]', style: { 'background-color': '#7c3aed' } },
                baseEdgeStyle(),
                { selector: 'edge[kind = "allowed"]', style: { 'line-color': '#16a34a', 'target-arrow-color': '#16a34a' } },
                { selector: 'edge[kind = "violation"]', style: { 'line-color': '#dc2626', 'target-arrow-color': '#dc2626', 'width': 2 } },
                {
                    selector: 'edge[kind = "declared"]',
                    style: {
                        'line-color': '#9ca3af',
                        'target-arrow-color': '#9ca3af',
                        'line-style': 'dashed',
                        'opacity': 0.6
                    }
                }
            ]
        };
    });

    // Dashboard mini layer-map: layers only, no children, no
    // interactions (click-through navigates to /layers via the parent
    // <a>).
    registerView('layer-map-mini', function () {
        var full = registry['layer-map']();
        full.layout = { name: pickLayout('elk'), 'elk': { algorithm: 'layered', 'elk.direction': 'RIGHT' }, padding: 8 };
        return full;
    });

    // Package overview (M8): subject package at centre with exported
    // types as children, inbound/outbound packages around it.
    registerView('package-overview', function () {
        return {
            layout: { name: pickLayout('dagre'), rankDir: 'LR', padding: 10, nodeSep: 30, rankSep: 60 },
            style: [
                baseNodeStyle(),
                { selector: 'node[root]', style: { 'background-color': '#2563eb' } },
                { selector: 'node[kind = "interface"]', style: { 'background-color': '#0ea5e9' } },
                { selector: 'node[kind = "struct"]', style: { 'background-color': '#7c3aed' } },
                { selector: 'node[kind = "function"]', style: { 'background-color': '#0d9488' } },
                // M9 (#61): exported factories / constructors are the
                // "entry points" of a package's public surface; render
                // them with a distinct green fill and a bold dark border
                // so they stand out from regular functions.
                { selector: 'node[kind = "entry-point"]', style: {
                    'background-color': '#16a34a',
                    'border-color': '#064e3b',
                    'border-width': 3,
                    'shape': 'round-rectangle',
                    'font-weight': 'bold'
                } },
                { selector: 'node[kind = "package"]', style: { 'background-color': '#1e293b', 'shape': 'round-rectangle', 'padding': 12, 'background-opacity': 0.4 } },
                { selector: 'node[kind = "package-in"]', style: { 'background-color': '#475569' } },
                { selector: 'node[kind = "package-out"]', style: { 'background-color': '#334155' } },
                baseEdgeStyle(),
                { selector: 'edge[kind = "inbound"]', style: { 'line-color': '#38bdf8', 'target-arrow-color': '#38bdf8' } },
                { selector: 'edge[kind = "outbound"]', style: { 'line-color': '#f97316', 'target-arrow-color': '#f97316' } }
            ]
        };
    });

    // Diff overlay (M8): per-change node, coloured by op, parented by
    // the pseudo-package node the change lives under.
    registerView('diff-overlay', function () {
        return {
            layout: { name: pickLayout('dagre'), rankDir: 'TB', padding: 10 },
            style: [
                baseNodeStyle(),
                { selector: 'node[op = "add"]', style: { 'background-color': '#16a34a' } },
                { selector: 'node[op = "remove"]', style: { 'background-color': '#dc2626' } },
                { selector: 'node[op = "change"]', style: { 'background-color': '#d97706' } },
                { selector: 'node[kind = "package"]', style: { 'background-color': '#1e293b', 'shape': 'round-rectangle', 'padding': 12, 'background-opacity': 0.35 } },
                baseEdgeStyle(),
                { selector: 'edge[kind = "add"]', style: { 'line-color': '#16a34a', 'target-arrow-color': '#16a34a' } },
                { selector: 'edge[kind = "remove"]', style: { 'line-color': '#dc2626', 'target-arrow-color': '#dc2626', 'line-style': 'dashed' } },
                { selector: 'edge[kind = "change"]', style: { 'line-color': '#d97706', 'target-arrow-color': '#d97706' } }
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
        // Click → navigate (package / type). The node's id encodes a
        // routing prefix ("pkg:" / "type:" / "layer:"); anything else
        // is left alone.
        cy.on('tap', 'node', function (evt) {
            var id = evt.target.id();
            if (id.indexOf('pkg:') === 0) {
                window.location.href = '/packages/' + id.slice(4);
            } else if (id.indexOf('type:') === 0) {
                window.location.href = '/types/' + id.slice(5);
            } else if (id.indexOf('layer:') === 0) {
                // Navigate to the Layers list — layers don't have a
                // dedicated detail page.
                window.location.href = '/layers#' + id.slice(6);
            }
        });
        // Hover highlight: fade everything else, accent the neighbourhood.
        cy.on('mouseover', 'node', function (evt) {
            var n = evt.target;
            cy.elements().addClass('cy-faded');
            n.removeClass('cy-faded').addClass('cy-hi');
            n.connectedEdges().removeClass('cy-faded').addClass('cy-hi');
            n.connectedEdges().connectedNodes().removeClass('cy-faded').addClass('cy-hi');
        });
        cy.on('mouseout', 'node', function () {
            cy.elements().removeClass('cy-faded cy-hi');
        });
    }

    function attachToolbar(el, cy) {
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
        var actions = toolbar.querySelectorAll('[data-cy-action]');
        for (var i = 0; i < actions.length; i++) {
            (function (btn) {
                btn.addEventListener('click', function (ev) {
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
                        case 'export-png':
                            try {
                                var uri = cy.png({ full: true, scale: 2, bg: '#0f172a' });
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
                });
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
        var preset = (registry[view] || registry['type-detail'])();

        var hydrate = function (payload) {
            if (!window.cytoscape) {
                el.innerHTML = '<p class="muted small">(cytoscape.js not available)</p>';
                return;
            }
            var elements = hydrateElements(payload);
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
                    { selector: '.cy-faded', style: { 'opacity': 0.2 } },
                    { selector: '.cy-hi', style: { 'opacity': 1 } }
                ])
            });
            attachInteractions(cy, el, interactive);
            attachToolbar(el, cy);
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
