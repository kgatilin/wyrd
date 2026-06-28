/**
 * A SAVED system doc that explains the plugin architecture — and is itself a
 * live instance of it: an ordinary artifact (the same unit the agent authors via
 * write_artifact) composing in-scope capabilities (Markdown, Mermaid, File). It
 * bakes no data; the <File> widgets pull the real seam files from the source
 * data-source. Seeded alongside the welcome dashboard (see seed.ts).
 */
export const pluginArchitectureFile = `
function Artifact() {
  return (
    <article className="artifact-doc">
      <Markdown>{\`
# The plugin seam — on your fingers

**This document is itself an artifact.** It composes the very capabilities a
plugin contributes — **Markdown**, **Mermaid**, **File** — all pulled from the
host scope. So it is both the explanation *and* a live instance of the mechanism
it describes.

## The one rule everything rests on

> The **artifact scope** is the complete and only interface between
> agent-authored content and the host.

An artifact never imports and never does its own I/O — it only references names
the host injected. So *controlling what is in scope is the plugin boundary*. A
**plugin** is nothing more than a contributor to that scope, plus the matching
**manifest** the agent is told about. One declaration, two consumers.
\`}</Markdown>

      <Mermaid chart={\`
flowchart TD
  WS["Workspace = a folder"]
  WS --> AGENT["Active agent over AG-UI"]
  WS --> PLUGS["Enabled plugins"]
  PLUGS --> CAPS["capabilities — the manifest"]
  PLUGS --> VALS["values — the implementations"]
  CAPS --> PROMPT["Agent system prompt"]
  VALS --> SCOPE["Artifact scope in the browser"]
  PROMPT -.-> AGENT
  AGENT -.-> ART["Artifact JSX"]
  SCOPE --> RUN["Compiled artifact runtime"]
  ART --> RUN
  RUN --> CANVAS["Rendered on this canvas"]
\`} />

      <Markdown>{\`
## Today: two hardcoded parallel lists

Right now the scope is baked into two globals that a dev-guard keeps in lockstep:

- **capabilities.ts** — the *manifest*: what the agent is told it may write
  (name, kind, signature, doc). Pure data, read on both server and client.
- **scope.ts** — the *values*: what each name actually resolves to at runtime,
  injected into every compiled artifact.

Open the real files — this is the whole seam:
\`}</Markdown>

      <File path="web-canvas/src/lib/artifact/capabilities.ts" height={360} />

      <File path="web-canvas/src/lib/artifact/scope.ts" height={360} />

      <Markdown>{\`
## A plugin just collapses those two lists into one module

Instead of two flat globals, each domain ships **one module** that bundles its
manifest entries together with their implementations:

    // archai/plugin.ts — one module exports manifest + impls together
    const archaiPlugin = definePlugin({
      id: 'archai',
      capabilities: [
        { name: 'Graph',    kind: 'component',   signature: '<Graph .../>',   doc: '…' },
        { name: 'useGraph', kind: 'data-source', signature: 'useGraph(spec)', doc: '…' },
        // …File, FileTree, useEvents
      ],
      values:  { Graph: GraphView, File: FileView, useGraph, useEvents },
      backend: resolveArchaiDaemon,   // the plugin owns its own endpoint
    });

The shell never imports a plugin by name — it **reduces over the enabled set**,
and both consumers fall out of the very same reduction:

    const enabled = workspace.plugins.map((id) => registry[id]);

    // (1) browser: the runtime scope for compiled artifacts
    buildArtifactScope = () => ({
      React,
      ...Object.assign({}, ...enabled.map((p) => p.values)),
    });

    // (2) agent: the manifest it is told it can write
    renderAgentDeclaration = () =>
      enabled.flatMap((p) => p.capabilities).map(render).join(newline);

**archai becomes just one such plugin** — its graph / sequence / file widgets and
their data-sources, owning its own daemon resolution. The shell stops knowing
what archai *is*; it only mounts whatever the enabled plugins contribute.

## Where the agent and the workspace fit

- A **plugin** extends what an artifact *can do* — it contributes to scope +
  manifest.
- An **agent** is *who you talk to* over AG-UI; it composes those capabilities.
  Orthogonal in identity, coupled only through the manifest above. A plugin may
  *suggest* a default agent, but the active agent is a workspace choice.
- A **workspace** (keyed by its folder) binds them: { agent, plugins[] }.
  Switching workspace re-points the agent and rebuilds scope + manifest from that
  workspace's enabled plugins.

So the plugin system is not new machinery — it is opening **one seam you already
have** (the two files above) and lifting the binding into a workspace.
\`}</Markdown>
    </article>
  );
}
`;
