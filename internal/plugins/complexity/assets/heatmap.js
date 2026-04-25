// heatmap.js — built-in complexity plugin custom element.
//
// Defines <plugin-complexity-heatmap> as a vanilla Custom Element.
// On connect it fetches the JSON payload at the URL given by the
// `data-model-url` attribute (the host page sets it to the
// /api/plugins/complexity/scores route) and renders a small heatmap
// table whose row background scales with the relative score.
//
// No external dependencies. Designed to stay under 200 lines so it
// compiles into the archai binary via embed without bloating the
// distribution.

(function () {
    if (customElements.get("plugin-complexity-heatmap")) {
        return;
    }

    class ComplexityHeatmap extends HTMLElement {
        connectedCallback() {
            const url = this.getAttribute("data-model-url") || "/api/plugins/complexity/scores";
            this.renderLoading();
            fetch(url, { headers: { Accept: "application/json" } })
                .then((r) => {
                    if (!r.ok) {
                        throw new Error("HTTP " + r.status);
                    }
                    return r.json();
                })
                .then((rows) => this.renderRows(rows || []))
                .catch((err) => this.renderError(err));
        }

        renderLoading() {
            this.innerHTML = '<p class="plugin-complexity-loading">Loading complexity…</p>';
        }

        renderError(err) {
            const msg = (err && err.message) || String(err);
            this.innerHTML = '<p class="plugin-complexity-error">Error loading complexity: ' +
                escapeHtml(msg) + "</p>";
        }

        renderRows(rows) {
            if (!rows.length) {
                this.innerHTML = '<p class="plugin-complexity-empty">No packages.</p>';
                return;
            }
            let max = 0;
            for (const r of rows) {
                if (typeof r.score === "number" && r.score > max) {
                    max = r.score;
                }
            }
            if (max <= 0) {
                max = 1;
            }
            const lines = [];
            lines.push('<table class="plugin-complexity-heatmap">');
            lines.push("<thead><tr><th>Package</th><th>Layer</th><th>Score</th></tr></thead>");
            lines.push("<tbody>");
            for (const row of rows) {
                const score = typeof row.score === "number" ? row.score : 0;
                const ratio = score / max;
                const bg = colorFor(ratio);
                lines.push(
                    '<tr style="background:' + bg + '">' +
                        "<td>" + escapeHtml(row.package || "") + "</td>" +
                        "<td>" + escapeHtml(row.layer || "") + "</td>" +
                        "<td>" + score + "</td>" +
                        "</tr>",
                );
            }
            lines.push("</tbody></table>");
            this.innerHTML = lines.join("");
        }
    }

    function colorFor(ratio) {
        // Cool (low) -> warm (high) gradient.
        const r = Math.round(255 * Math.min(1, Math.max(0, ratio)));
        const g = Math.round(255 * (1 - Math.min(1, Math.max(0, ratio))));
        const b = 80;
        return "rgba(" + r + "," + g + "," + b + ",0.25)";
    }

    function escapeHtml(s) {
        return String(s)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;")
            .replace(/'/g, "&#39;");
    }

    customElements.define("plugin-complexity-heatmap", ComplexityHeatmap);
})();
