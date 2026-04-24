// graph.js — hydrates every .cy-graph <div> on the page by reading its
// data-graph attribute (JSON with `nodes` + `edges`) and mounting a
// Cytoscape.js instance. Added for M7d (#24) per the M8 (#46) decision
// to render graphs client-side instead of server-rendering D2→SVG.
//
// The script is tolerant: if Cytoscape or the dagre plugin fail to
// load, we fall back to displaying the raw payload so the page is
// never blank.
(function () {
    'use strict';

    function renderGraph(el) {
        var raw = el.getAttribute('data-graph');
        if (!raw) {
            el.innerHTML = '<p class="muted small">(no graph data)</p>';
            return;
        }
        var payload;
        try {
            payload = JSON.parse(raw);
        } catch (err) {
            el.innerHTML = '<p class="muted small">(graph data not parseable)</p>';
            return;
        }
        if (!window.cytoscape) {
            el.innerHTML = '<p class="muted small">(cytoscape.js not available)</p>';
            return;
        }

        var elements = [];
        (payload.nodes || []).forEach(function (n) {
            elements.push({
                group: 'nodes',
                data: { id: n.id, label: n.label || n.id, kind: n.kind || '', root: !!n.root }
            });
        });
        (payload.edges || []).forEach(function (e, i) {
            elements.push({
                group: 'edges',
                data: {
                    id: 'e' + i + ':' + e.source + '->' + e.target,
                    source: e.source,
                    target: e.target,
                    label: e.label || '',
                    kind: e.kind || ''
                }
            });
        });

        // Register dagre layout once per page load. If the plugin is
        // missing we silently fall back to cose.
        var layoutName = 'cose';
        if (window.cytoscapeDagre && !window.__archaiDagreRegistered) {
            window.cytoscape.use(window.cytoscapeDagre);
            window.__archaiDagreRegistered = true;
        }
        if (window.cytoscapeDagre || window.__archaiDagreRegistered) {
            layoutName = 'dagre';
        }

        window.cytoscape({
            container: el,
            elements: elements,
            layout: { name: layoutName, rankDir: 'LR', padding: 10 },
            style: [
                {
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
                },
                {
                    selector: 'node[root]',
                    style: { 'background-color': '#2563eb' }
                },
                {
                    selector: 'node[kind = "interface"]',
                    style: { 'background-color': '#0ea5e9' }
                },
                {
                    selector: 'node[kind = "package"]',
                    style: { 'background-color': '#7c3aed' }
                },
                {
                    selector: 'node[kind = "cycle"]',
                    style: { 'background-color': '#b45309' }
                },
                {
                    selector: 'node[kind = "depth-limit"]',
                    style: { 'background-color': '#475569' }
                },
                {
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
                }
            ]
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
