import { resolveDaemonBase, worktreePrefix } from "@/lib/server/daemon";

// Server-side proxy to the archai daemon's graph API.
//
// Why a proxy and not a direct browser fetch: the daemon binds a random
// loopback port and sets no CORS headers, so the browser can't reach it. This
// route runs in Node, discovers the daemon for the current repo from the global
// registry (~/.arch/daemons/*.json), and forwards the request server-to-server.
//
// Two response shapes, by params:
//   - ?query=… (or ?nodes=…): a queried SUBGRAPH from /api/search_graph or
//     /api/expand → { nodes, edges } (symbol-level).
//   - otherwise: the whole-project UIGraph from /api/uigraph.
export const dynamic = "force-dynamic";

function err(message: string, status: number) {
  return Response.json({ error: message }, { status });
}

export async function GET(request: Request) {
  const base = await resolveDaemonBase();
  if (!base) {
    return err("no archai daemon found for this repo (start one with `archai daemon start`)", 503);
  }

  const params = new URL(request.url).searchParams;
  const query = params.get("query")?.trim();
  const nodesParam = params.get("nodes")?.trim();
  const hops = clampInt(params.get("hops"), 1, 0, 4);
  const edges = (params.get("edges") ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

  const prefix = await worktreePrefix(base);

  try {
    // Queried subgraph: semantic query → search_graph.
    if (query) {
      const res = await fetch(`${base}${prefix}/api/search_graph`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({ query, k: 14, hops }),
      });
      if (!res.ok) return err(`archai search_graph returned ${res.status}`, 502);
      return Response.json(await res.json());
    }

    // Explicit seeds → expand (with optional edge-kind filter).
    if (nodesParam) {
      const node_ids = nodesParam.split(",").map((s) => s.trim()).filter(Boolean);
      const res = await fetch(`${base}${prefix}/api/expand`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({ node_ids, hops, edges }),
      });
      if (!res.ok) return err(`archai expand returned ${res.status}`, 502);
      return Response.json(await res.json());
    }

    // Whole-project UIGraph.
    const res = await fetch(`${base}${prefix}/api/uigraph`, {
      headers: { Accept: "application/json" },
    });
    if (!res.ok) return err(`archai daemon returned ${res.status}`, 502);
    return Response.json(await res.json());
  } catch (e) {
    return err(`archai daemon unreachable: ${String(e)}`, 502);
  }
}

function clampInt(raw: string | null, dflt: number, min: number, max: number): number {
  const n = raw == null ? NaN : parseInt(raw, 10);
  if (Number.isNaN(n)) return dflt;
  return Math.max(min, Math.min(max, n));
}
