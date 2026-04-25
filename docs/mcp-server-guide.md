# Archai MCP / Server Guide

Reference for running `archai serve`, the HTTP daemon, and the MCP
transport. Aimed at agents (Claude Code, Codex CLI, custom MCP clients)
and at humans wiring those agents up.

For the higher-level user guide — installation, project setup, browser
UI, editor integration — see [`user-guide.md`](user-guide.md). This
document zooms in on §4 (server) and §6 (MCP) of that guide.

> **Scope.** This guide covers what is on `main` today. The MCP
> transport surface is defined in `internal/adapter/mcp/tools.go`; the
> HTTP routes are in `internal/adapter/http/api.go`. Both are checked
> against the binary in [§7](#7-verifying-against-the-binary).

---

## 1. Architecture at a glance

```
┌──────────────────────────┐
│ MCP client (Claude Code, │
│ Codex, custom)           │
└────────────┬─────────────┘
             │ stdio (JSON-RPC 2.0)
             ▼
┌──────────────────────────┐    HTTP    ┌───────────────────────────┐
│ archai serve --mcp-stdio │ ─────────▶ │ archai serve --http       │
│ (thin client; auto-spawn │            │ (long-running daemon;     │
│  the HTTP daemon if      │            │  in-memory model + watcher)│
│  none is running)        │            │                           │
└──────────────────────────┘            │ • browser UI              │
                                        │ • /api/mcp/* JSON API     │
                                        └───────────────────────────┘
```

Two transports, one model:

- **HTTP daemon** (`archai serve --http`) holds the parsed Go model,
  watches the project root with `fsnotify`, serves the browser UI, and
  exposes every MCP tool over a JSON HTTP API under `/api/mcp/`.
- **MCP stdio thin client** (`archai serve --mcp-stdio`) is the binary
  an MCP client launches. It speaks JSON-RPC on stdio, answers
  `initialize` / `tools/list` locally, and forwards every `tools/call`
  to the worktree's HTTP daemon. If no daemon is running, it
  auto-spawns one bound to `127.0.0.1` and registers it for discovery.

A one-shot mode (`--mcp-stdio --no-daemon`) loads the model in-process
once and skips the daemon — useful for sandboxed environments or
short-lived agents that only call a few tools.

---

## 2. `archai serve`

```text
archai serve [flags]

Flags:
      --debug                   Verbose per-event logging
      --http string             HTTP transport address ("" disables HTTP;
                                default 127.0.0.1:0 binds loopback on a
                                free port, pass 0.0.0.0:PORT for LAN access)
                                (default "127.0.0.1:0")
      --idle-timeout duration   Exit the daemon after this duration with no
                                HTTP requests (0 disables; auto-started MCP
                                daemons use 15m)
      --mcp-stdio               Run as MCP stdio thin client, proxying
                                tools/call to the worktree's HTTP daemon
      --multi                   Serve every git worktree under /w/{name}/*
                                (multi-worktree mode)
      --no-daemon               With --mcp-stdio: skip auto-start and run
                                one-shot in-process (no HTTP daemon, no
                                watcher)
      --root string             Project root directory (default ".")
```

### 2.1 Operational modes

| Mode | Invocation | What runs |
|------|------------|-----------|
| Long-running HTTP daemon | `archai serve --http :8080` | Loads model, watches FS, serves browser UI + `/api/mcp/*`. |
| Headless model-keeper | `archai serve --http ""` | Loads model, watches FS, no transport. Useful as a base for future features. |
| MCP thin client (default) | `archai serve --mcp-stdio` | Discovers/auto-starts an HTTP daemon, proxies stdio → HTTP. |
| MCP one-shot | `archai serve --mcp-stdio --no-daemon` | Loads model in-process, answers MCP stdio directly. No watcher. |
| Multi-worktree | `archai serve --http :8080 --multi` | One daemon serves every git worktree under `/w/{name}/*`. |

### 2.2 Worktree-scoped daemons

`archai serve` is worktree-aware. On startup it writes a registration
record to `.arch/.worktree/<name>/serve.json`:

```json
{
  "pid": 3682664,
  "http_addr": "127.0.0.1:35729",
  "started_at": "2026-04-25T10:51:00Z"
}
```

Two helper commands inspect those records:

- `archai where` — print the URL of the daemon currently serving the
  current worktree (exits non-zero when no daemon is running).
- `archai list-daemons` — table of every live daemon under the current
  project root (worktree, PID, URL, uptime). Stale records (process no
  longer alive) are skipped automatically.

```text
$ archai list-daemons
WORKTREE              PID      URL                     UPTIME
demo-archai           3682664  http://127.0.0.1:35729  4s
```

### 2.3 Idle timeout and auto-start

The MCP thin client auto-starts a detached HTTP daemon when no live
record exists for the current worktree. The auto-started daemon is
launched with `--idle-timeout 15m` so an orphaned MCP client does not
leave a daemon running indefinitely. User-started `archai serve`
defaults to `--idle-timeout 0` (no timeout).

Auto-started daemons always bind `127.0.0.1:0` — auto-start never
exposes the daemon on the LAN. To bind a non-loopback interface,
launch `archai serve` yourself with an explicit `--http` address.

---

## 3. MCP transport

### 3.1 Protocol

Stdio JSON-RPC 2.0 (one message per line). Supported methods:

| Method | Notes |
|--------|-------|
| `initialize` | Returns `{protocolVersion, capabilities, serverInfo}`. |
| `notifications/initialized` / `initialized` | Acknowledgement; no response. |
| `tools/list` | Returns all tools with JSON Schemas. Answered locally by the thin client. |
| `tools/call` | Forwarded to the HTTP daemon (thin-client mode) or dispatched locally (`--no-daemon`). |
| `ping` | Returns `{}`. |

The `serverInfo` block reports `{"name": "archai", "version": "0.1.0"}`
and `protocolVersion` is `2024-11-05`.

### 3.2 Client configuration

#### Claude Code (`.mcp.json`)

```json
{
  "mcpServers": {
    "archai": {
      "command": "archai",
      "args": ["serve", "--mcp-stdio", "--root", "."]
    }
  }
}
```

#### Codex CLI (`config.toml`)

```toml
[mcp_servers.archai]
command = "archai"
args    = ["serve", "--mcp-stdio", "--root", "."]
```

#### Custom MCP client

Any client that can spawn a subprocess and exchange line-delimited
JSON-RPC works. Send `initialize`, then `notifications/initialized`,
then `tools/list` to discover the tool surface, then `tools/call` for
each invocation.

### 3.3 HTTP API (for non-MCP callers)

Every MCP tool also has a plain HTTP endpoint under `/api/mcp/`. This
is what the thin client forwards to and what direct callers can use
when they do not want to go through MCP.

Read endpoints (GET):

| Route | Tool |
|-------|------|
| `/api/mcp/packages` | `list_packages` |
| `/api/mcp/packages/{path}` | `get_package` |
| `/api/mcp/extract?path=a&path=b…` | `extract` (filtered) |
| `/api/mcp/targets` | `list_targets` |
| `/api/mcp/diff?target=<id>` | `diff` |

Write endpoints (POST, JSON body):

| Route | Tool | Body |
|-------|------|------|
| `/api/mcp/targets/lock` | `lock_target` | `{"id": "...", "description": "..."}` |
| `/api/mcp/targets/current` | `set_current_target` | `{"id": "..."}` |
| `/api/mcp/diff/apply` | `apply_diff` | `{"patch_yaml": "...", "target": "..."}` |
| `/api/mcp/validate` | `validate` | `{"target": "..."}` (or `{}`) |

Generic dispatcher (forward-compatible):

```text
POST /api/mcp/tools/call
Content-Type: application/json

{"name": "<tool>", "arguments": { ... }}
```

Returns the raw `ToolResult` envelope (`{content: [{type, text}], isError?}`).

---

## 4. MCP tool reference

Nine tools, defined in `internal/adapter/mcp/tools.go`. Tool-level
errors (missing target, unknown package) come back with `isError:true`
and a human-readable text block — the agent can read them and recover.
Protocol-level errors (unknown tool, malformed JSON) come back as
JSON-RPC errors.

### `extract`

Return the full extracted Go model. Optional `paths` filter.

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call",
 "params":{"name":"extract","arguments":{"paths":["internal/service"]}}}
```

### `list_packages`

Minimal summary per package — path, name, layer, interface/struct/function counts.

Request:

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call",
 "params":{"name":"list_packages","arguments":{}}}
```

Response (the inner `text` parses as JSON):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [{
      "type": "text",
      "text": "[\n  {\n    \"path\": \"internal/svc\",\n    \"name\": \"svc\",\n    \"interface_count\": 0,\n    \"struct_count\": 1,\n    \"function_count\": 1\n  }\n]"
    }]
  }
}
```

### `get_package`

Full `PackageModel` for one package — interfaces, structs, functions,
methods, dependencies, calls.

```json
{"name":"get_package","arguments":{"path":"internal/service"}}
```

### `list_targets`

All locked targets under `.arch/targets/`, sorted by id.

```json
{"name":"list_targets","arguments":{}}
```

### `lock_target`

Freeze the in-memory model into `.arch/targets/<id>/`. Materialises
`.arch/internal.yaml` for each package first, then copies the snapshot.

```json
{"name":"lock_target",
 "arguments":{"id":"v1","description":"baseline"}}
```

### `set_current_target`

Write `.arch/targets/CURRENT` and update the daemon's in-memory
`currentTarget` so the next `diff` / `validate` call sees it.

```json
{"name":"set_current_target","arguments":{"id":"v1"}}
```

### `diff`

Structured diff between current code and a target. `target` defaults
to the active `CURRENT`.

```json
{"name":"diff","arguments":{"target":"v1"}}
```

The result body has shape:

```json
{
  "current_target": "v1",
  "changes": [
    {"op":"add","kind":"struct","path":"internal/service.NewClient", "...": "..."}
  ]
}
```

`op` is one of `add`, `remove`, `change`. `kind` is one of
`package`, `interface`, `struct`, `function`, `method`, `field`,
`const`, `var`, `error`, `dep`, `layer_rule`, `type_def`.

### `apply_diff`

Apply a YAML patch (same shape as `archai diff --format yaml`) onto the
target snapshot on disk. The agent typically generates the patch by
calling `diff`, editing the change list, and feeding it back here.

```json
{"name":"apply_diff",
 "arguments":{"patch_yaml":"changes:\n  - op: add\n    kind: struct\n    ...\n",
              "target":"v1"}}
```

### `validate`

`{ok, target, violations:[...]}` — `ok:true` means no drift. Same data
as the `diff` tool, packaged for CI-style checks.

```json
{"name":"validate","arguments":{}}
```

---

## 5. Agent workflows

### 5.1 Onboarding to an unfamiliar repo

Goal: answer "what does this codebase do, and where do I start?" purely
through MCP tools.

1. **Boot.** The MCP client launches `archai serve --mcp-stdio`. The
   thin client auto-spawns the HTTP daemon on first call.
2. `tools/call list_packages` → skim the layer tags and counts. Layer
   distribution is the fastest signal of how the project is split.
3. Pick the entry layer (usually `cli` or `service`):
   `tools/call list_packages` → filter to that layer →
   `tools/call get_package {"path":"cmd/app"}` for each entry point.
4. For any interesting type, follow the dependency edges in the
   returned `PackageModel.dependencies` — call `get_package` again on
   the imported packages.
5. Summarise. The model is fully structured (interfaces, methods,
   parameters, calls), so the agent can answer "where is X handled?"
   without `grep`.

When the question is about call flow rather than topology, fall back
to the CLI: `archai sequence <pkg>.<Type>.<Method> --depth 3` prints
the static call tree rooted at that symbol. The MCP surface does not
expose `sequence` today.

### 5.2 Refactor against a locked target

Goal: change the architecture intentionally and have the diff stay
clean.

1. `tools/call lock_target {"id":"v-next","description":"post-refactor"}`
   on the current shape (so we can compare against it).
2. `tools/call set_current_target {"id":"v-next"}`.
3. Edit the desired shape into `.arch/targets/v-next/model/*.yaml` —
   either by hand, or by calling `apply_diff` with a generated patch.
4. Edit Go code. The watcher refreshes the in-memory model on every
   save.
5. Loop:
   - `tools/call validate` → if `ok:true`, done.
   - Otherwise read `violations`, fix the code (or the target), and
     call `validate` again.
6. When `validate` returns `ok:true`, the refactor matches the locked
   target.

### 5.3 Pre-commit drift gate (CI parity)

Goal: fail fast when local edits drift from the locked baseline.

```text
agent → tools/call validate {}
     ← {"ok": false, "target": "v1", "violations": [...]}
agent → "Drift in internal/service: NewClient added but not in target.
         Either remove the constructor or update target v1."
```

The same call shape powers the CLI gate (`archai validate`) and the
GitHub Actions example in [`user-guide.md` §3.5](user-guide.md#35-ci-integration).

---

## 6. UI vs CLI vs MCP — when to use which

| Surface | Best for | Avoid for |
|---------|----------|-----------|
| Browser UI (`archai serve --http`) | Human exploration: layer maps, package detail, diff colour-coding, search. | Scripting, CI, agents — UI returns HTML, not structured data. |
| CLI (`archai diagram`, `archai diff`, `archai validate`, `archai sequence`, `archai overlay check`) | One-shot scripting, CI gates, generating D2 outputs, call-sequence trees. | Long agent sessions where the same model is queried many times — every CLI invocation re-parses sources. |
| MCP tools (over `archai serve --mcp-stdio`) | Agents and other programmatic clients that want structured JSON, repeated queries against a hot in-memory model, and write operations (`lock_target`, `apply_diff`). | Rendering D2 diagrams (use the CLI) and visual exploration (use the UI). |

Practical rule: humans → UI, scripts → CLI, agents → MCP. The three
surfaces share the same model and the same diff format, so anything an
agent decides through MCP can be cross-checked by a human in the
browser or by CI on the CLI.

---

## 7. Verifying against the binary

Every command in this guide was checked against `archai` built from
this repository.

```bash
# Build
go build -o archai ./cmd/archai

# Help
archai --help
archai serve --help
archai where --help
archai list-daemons --help
archai extract --help

# Smoke a daemon
mkdir demo && cd demo && go mod init example.com/demo
mkdir -p internal/svc && cat > internal/svc/svc.go <<'EOF'
package svc
type Service struct{}
func New() *Service { return &Service{} }
func (s *Service) Greet(name string) string { return "hello " + name }
EOF

archai serve --http 127.0.0.1:0 --idle-timeout 60s &
URL=$(archai where)
curl -s "$URL/api/mcp/packages"
curl -s -X POST "$URL/api/mcp/tools/call" \
     -H 'Content-Type: application/json' \
     -d '{"name":"list_packages","arguments":{}}'

# Stdio one-shot (no daemon)
( printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}\n'
  printf '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}\n'
  printf '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_packages","arguments":{}}}\n'
) | archai serve --mcp-stdio --no-daemon
```

If a command in this guide does not match your binary, your `archai`
is older than the docs — rebuild from `main`.

---

## 8. Forward pointers

- `archai sequence <symbol>` is CLI-only today. Tracking issue: see
  [`docs/roadmap.md`](roadmap.md) for the milestone plan.
- The plugin contract (M12 / M13) will let third-party plugins register
  their own MCP tools through the same `/api/mcp/tools/call`
  dispatcher. Until that lands, the tool list is fixed at the nine
  built-ins above.
- Multi-worktree mode (`archai serve --multi`) wires `/w/{name}/api/mcp/*`
  per worktree but the current MCP client always talks to a single
  worktree's daemon.

---

## References

- [`user-guide.md`](user-guide.md) — installation, project setup,
  browser UI, editor integration.
- [`roadmap.md`](roadmap.md) — milestone plan.
- `internal/adapter/mcp/tools.go` — tool definitions and JSON Schemas
  (source of truth).
- `internal/adapter/http/api.go` — HTTP route definitions.
- `internal/adapter/mcp/stdio.go`, `client.go` — stdio transport and
  thin-client implementation.
