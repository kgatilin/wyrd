import { resolveDaemonBase, worktreePrefix } from "@/lib/server/daemon";

// Same-origin proxy to the archai daemon's /api/sequence endpoint. Given a
// package path it returns the package's call-sequence diagrams as Mermaid
// `sequenceDiagram` source, projected to the type-interaction level (cross-type
// calls only; intra-type chatter collapsed). See the Go handler
// internal/adapter/http/sequence_api.go.
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
  const pkg = params.get("package")?.trim();
  if (!pkg) return err("missing package", 400);
  const depth = params.get("depth")?.trim();

  const prefix = await worktreePrefix(base);
  const qs = new URLSearchParams({ package: pkg });
  if (depth) qs.set("depth", depth);

  try {
    const res = await fetch(`${base}${prefix}/api/sequence?${qs.toString()}`, {
      headers: { Accept: "application/json" },
    });
    if (!res.ok) return err(`archai sequence returned ${res.status}`, 502);
    return Response.json(await res.json());
  } catch (e) {
    return err(`archai daemon unreachable: ${String(e)}`, 502);
  }
}
