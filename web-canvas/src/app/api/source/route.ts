import { readFile } from "node:fs/promises";
import { resolve, relative, isAbsolute } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { resolveRepoRoot } from "@/lib/server/daemon";

// Same-origin source-file provider for the File / FileTree artifact widgets.
//
//   GET /api/source?path=<repo-relative>&diff=1&base=<ref>
//
// Returns the working-tree content of a repo file (read from disk; the
// dev server's cwd is the repo root) and, when diff=1, the base-ref content via
// `git show <base>:<path>` so the widget can render an inline diff. base
// defaults to "main". Paths are confined to the repo root.
export const dynamic = "force-dynamic";

const execFileP = promisify(execFile);

function err(message: string, status: number) {
  return Response.json({ error: message }, { status });
}

/** Resolve a repo-relative path inside root, rejecting traversal. */
function safeJoin(root: string, rel: string): string | null {
  const cleaned = rel.replace(/^\/+/, "");
  const abs = resolve(root, cleaned);
  const r = relative(root, abs);
  if (r.startsWith("..") || isAbsolute(r)) return null;
  return abs;
}

async function gitShow(root: string, ref: string, path: string): Promise<string | null> {
  try {
    const { stdout } = await execFileP("git", ["-C", root, "show", `${ref}:${path}`], {
      maxBuffer: 8 * 1024 * 1024,
    });
    return stdout;
  } catch {
    // File absent at base ref (added) or git error → no base content.
    return null;
  }
}

export async function GET(request: Request) {
  const params = new URL(request.url).searchParams;
  const path = params.get("path")?.trim();
  if (!path) return err("missing path", 400);
  const wantDiff = params.get("diff") === "1" || params.get("diff") === "true";
  const baseRef = params.get("base")?.trim() || "main";

  const root = await resolveRepoRoot();
  const rel = path.replace(/^\/+/, "");
  const abs = safeJoin(root, rel);
  if (!abs) return err("path escapes repo root", 400);

  let content: string;
  try {
    content = await readFile(abs, "utf8");
  } catch {
    return err(`file not found: ${rel}`, 404);
  }

  let baseContent: string | null = null;
  if (wantDiff) {
    baseContent = await gitShow(root, baseRef, rel);
  }

  return Response.json({
    path: rel,
    content,
    baseRef,
    baseContent,
    hasDiff: baseContent != null && baseContent !== content,
  });
}
