import { readdir, readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

// Server-side proxy to the archai daemon's graph API.
//
// Why a proxy and not a direct browser fetch: the daemon binds a random
// loopback port and sets no CORS headers, so the browser can't reach it. This
// route runs in Node, discovers the daemon for the current repo from the global
// registry (~/.arch/daemons/*.json), and forwards the request server-to-server.
// The browser only ever talks to this same-origin route.
export const dynamic = "force-dynamic";

interface DaemonRecord {
  repo_root: string;
  http_addr: string;
}

// resolveDaemonBase finds the daemon serving the repo this app runs inside.
// ARCHAI_DAEMON_URL overrides discovery; otherwise pick the registry record
// whose repo_root is the deepest ancestor of the process working directory.
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
      // skip unreadable/!json records
    }
  }
  return best ? `http://${best.http_addr}` : null;
}

export async function GET(request: Request) {
  const base = await resolveDaemonBase();
  if (!base) {
    return Response.json(
      { error: "no archai daemon found for this repo (start one with `archai daemon start`)" },
      { status: 503 },
    );
  }

  const incoming = new URL(request.url);
  const target = new URL("/api/uigraph", base);
  // The daemon's project graph is whole-repo; forward only the review base ref.
  const baseRef = incoming.searchParams.get("base");
  if (baseRef) target.searchParams.set("base", baseRef);

  try {
    const res = await fetch(target.toString(), { headers: { Accept: "application/json" } });
    if (!res.ok) {
      return Response.json({ error: `archai daemon returned ${res.status}` }, { status: 502 });
    }
    const data = await res.json();
    return Response.json(data);
  } catch (err) {
    return Response.json({ error: `archai daemon unreachable: ${String(err)}` }, { status: 502 });
  }
}
