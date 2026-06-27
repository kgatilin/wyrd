import { readdir, readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

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

interface DaemonRecord {
  repo_root: string;
  http_addr: string;
}

async function resolveDaemonBase(): Promise<string | null> {
  const override = process.env.ARCHAI_DAEMON_URL;
  if (override) return override.replace(/\/$/, "");

  const dir = join(homedir(), ".arch", "daemons");
  let files: string[];
  try {
    files = await readdir(dir);
  } catch {
    return null;
  }

  const cwd = process.cwd();
  let best: DaemonRecord | null = null;
  for (const f of files) {
    if (!f.endsWith(".json")) continue;
    try {
      const rec = JSON.parse(await readFile(join(dir, f), "utf8")) as DaemonRecord;
      if (!rec.http_addr || !rec.repo_root) continue;
      if (cwd === rec.repo_root || cwd.startsWith(rec.repo_root + "/")) {
        if (!best || rec.repo_root.length > best.repo_root.length) best = rec;
      }
    } catch {
      // skip
    }
  }
  return best ? `http://${best.http_addr}` : null;
}

// In multi-worktree mode the daemon serves under /w/<name>/; bare /api/* paths
// 302-redirect for GET and 404 for POST. Resolve the worktree prefix once (from
// the final URL of a followed GET) and cache it per base.
let cachedPrefix: { base: string; prefix: string } | null = null;
async function worktreePrefix(base: string): Promise<string> {
  if (cachedPrefix?.base === base) return cachedPrefix.prefix;
  let prefix = "";
  try {
    const r = await fetch(`${base}/api/uigraph`, { headers: { Accept: "application/json" } });
    const m = new URL(r.url).pathname.match(/^(\/w\/[^/]+)\//);
    prefix = m ? m[1] : "";
  } catch {
    prefix = "";
  }
  cachedPrefix = { base, prefix };
  return prefix;
}

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
