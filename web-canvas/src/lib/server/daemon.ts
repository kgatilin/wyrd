import { readdir, readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

// Server-only helpers for discovering the archai daemon that serves the
// current repo and resolving its worktree URL prefix. Shared by the
// same-origin API proxy routes (graph, sequence) so they agree on which
// daemon and worktree to talk to.

interface DaemonRecord {
  repo_root: string;
  http_addr: string;
}

/**
 * Discover the daemon for the current repo from the global registry
 * (~/.arch/daemons/*.json), choosing the longest repo_root that is a prefix
 * of cwd. ARCHAI_DAEMON_URL overrides discovery. Returns a base URL
 * ("http://host:port") or null.
 */
export async function resolveDaemonBase(): Promise<string | null> {
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

/**
 * Resolve the repo root for the current process from the daemon registry
 * (longest repo_root that is a prefix of cwd). Falls back to cwd. Used by the
 * source proxy to read files and run `git show` against the right repo.
 */
export async function resolveRepoRoot(): Promise<string> {
  const dir = join(homedir(), ".arch", "daemons");
  let files: string[];
  try {
    files = await readdir(dir);
  } catch {
    return process.cwd();
  }
  const cwd = process.cwd();
  let best: string | null = null;
  for (const f of files) {
    if (!f.endsWith(".json")) continue;
    try {
      const rec = JSON.parse(await readFile(join(dir, f), "utf8")) as DaemonRecord;
      if (!rec.repo_root) continue;
      if (cwd === rec.repo_root || cwd.startsWith(rec.repo_root + "/")) {
        if (!best || rec.repo_root.length > best.length) best = rec.repo_root;
      }
    } catch {
      // skip
    }
  }
  return best ?? cwd;
}

// In multi-worktree mode the daemon serves under /w/<name>/; bare /api/* paths
// 302-redirect for GET and 404 for POST. Resolve the worktree prefix once (from
// the final URL of a followed GET) and cache it per base.
let cachedPrefix: { base: string; prefix: string } | null = null;

export async function worktreePrefix(base: string): Promise<string> {
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
