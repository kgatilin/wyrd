# wyrd Helm chart

Runs the wyrd daemon in Kubernetes for one Go repository: the review UI, the
HTTP API and MCP behind one ClusterIP Service, with the search index — dense
embeddings and BM25 — in Qdrant.

What a release creates:

- a Deployment with one wyrd pod (a single writer, `Recreate` strategy);
- a PersistentVolumeClaim for the data volume: the checkout, wyrd's parsed
  models and the Go module cache, all rebuildable;
- a Service, a ServiceAccount, and optionally an Ingress;
- optionally Qdrant, from the `qdrant/qdrant` chart, with its own volume.

What it does **not** do: check the repository out or keep it up to date. The
checkout is expected at `repo.path` on the data volume, and you supply the
containers that put it there (`sync.initContainers`, `sync.containers`).
Authentication to your git host, the branch and the fetch period are theirs to
configure.

## Requirements

- Kubernetes 1.25+, Helm 3.8+.
- Qdrant 1.10 or newer when you bring your own. The bundled one is 1.19.1.
- An embedding provider. The default is Gemini on Vertex AI; an
  OpenAI-compatible endpoint or an Ollama server work too.

## Install

```bash
helm dependency build charts/wyrd
helm install wyrd charts/wyrd --namespace wyrd --create-namespace -f my-values.yaml
```

From a release:

```bash
helm install wyrd oci://ghcr.io/kgatilin/charts/wyrd --version <version> \
  --namespace wyrd --create-namespace -f my-values.yaml
```

A minimal `my-values.yaml` — Gemini through GKE workload identity, bundled
Qdrant, the repository cloned and followed by git-sync:

```yaml
embedder:
  provider: vertex
  dim: 768
  vertex:
    project: acme-search          # the Google Cloud project to bill
    location: global

serviceAccount:
  annotations:
    iam.gke.io/gcp-service-account: wyrd@acme-search.iam.gserviceaccount.com

sync:
  initContainers:
    - name: clone
      image: registry.k8s.io/git-sync/git-sync:v4.4.0
      args:
        - --repo=https://git.example.com/acme/shop.git
        - --ref=main
        - --root=/data/git
        - --link={{ .Values.repo.path }}
        - --one-time
      envFrom: [{secretRef: {name: shop-git-credentials}}]
      volumeMounts: [{name: data, mountPath: /data}]
  containers:
    - name: git-sync
      image: registry.k8s.io/git-sync/git-sync:v4.4.0
      args:
        - --repo=https://git.example.com/acme/shop.git
        - --ref=main
        - --root=/data/git
        - --link={{ .Values.repo.path }}
        - --period=60s
      envFrom: [{secretRef: {name: shop-git-credentials}}]
      volumeMounts: [{name: data, mountPath: /data}]
```

The Google service account needs `roles/aiplatform.user` on the project, and
the Kubernetes service account (`<namespace>/<release name>`) needs
`roles/iam.workloadIdentityUser` on it. Without workload identity, put a
credentials JSON in a Secret and name it in
`embedder.vertex.credentialsSecret`.

Then:

```bash
kubectl -n wyrd port-forward svc/wyrd 8080:80
curl -s localhost:8080/readyz                 # {"ready":true}
open http://localhost:8080/                   # the review UI
```

The UI, its API and its assets live under `/w/<name>/`, where `<name>` is the
directory name of the checkout (`repo` by default); MCP is at
`/w/<name>/mcp`.

## What happens on start

1. `sync.initContainers` run. wyrd does not start without a checkout.
2. `bootstrap-collection` runs `wyrd qdrant bootstrap`: it creates the
   collection if it is missing — a dense vector of `embedder.dim` dimensions
   and a sparse BM25 vector — and verifies it either way. While Qdrant is
   still starting it fails and is retried. It changes nothing once the
   collection exists.
3. wyrd starts, parses the repository (the first time this downloads the
   repository's modules), writes the BM25 documents and embeds what the
   collection does not hold yet. A restart finds its vectors in Qdrant and its
   parsed models on the data volume, and embeds nothing.

With `requireRemote: true` (the default) wyrd exits instead of starting when
the embedder or Qdrant cannot be used: missing credentials, an unreachable
Qdrant, a collection of another dimension. Afterwards `/readyz` answers 503
while either is failing, with the reason in the body, and recovers by itself.

## Using a Qdrant you already run

```yaml
qdrant:
  enabled: false
qdrantConnection:
  url: http://qdrant.search:6333
  collection: shop                # one repository per collection
  apiKeySecret:
    name: qdrant-tokens
    key: read-write               # may read and write points
  bootstrap:
    adminApiKeySecret:
      name: qdrant-tokens
      key: admin                  # may create a collection
```

Set `qdrantConnection.bootstrap.enabled: false` to create the collection
yourself; `wyrd qdrant bootstrap --dim <n>` prints what it would need.

## Exposing the UI

wyrd has no login. `mcpAuth.tokenSecret` puts a bearer token on the MCP
endpoint, which agents inside the cluster send; the UI and the read APIs are
protected by whatever authenticates your ingress:

```yaml
mcpAuth:
  tokenSecret:
    name: wyrd-mcp
ingress:
  enabled: true
  className: nginx
  annotations:
    nginx.ingress.kubernetes.io/auth-url: https://auth.example.com/oauth2/auth
    nginx.ingress.kubernetes.io/auth-signin: https://auth.example.com/oauth2/start
  hosts:
    - host: code.example.com
      paths:
        - {path: /, pathType: Prefix}
  tls:
    - secretName: code-example-tls
      hosts: [code.example.com]
```

On a host shared with another application, list the paths wyrd owns instead
of `/`: `/w`, `/review`, `/api`, `/plugins`.

## Private Go modules

The reader runs `go list`, which downloads the repository's dependencies into
the data volume. For private ones:

```yaml
extraEnv:
  - {name: GOPROXY, value: "https://goproxy.example.com,direct"}
  - {name: GOPRIVATE, value: "git.example.com/*"}
```

## Upgrade

```bash
helm upgrade wyrd charts/wyrd -n wyrd -f my-values.yaml
```

The pod is replaced (`Recreate`), so the UI is down for the restart. Changing
`embedder.model`, `embedder.dim` or the provider starts a new set of vectors:
a different dimension needs a different collection, so change
`qdrantConnection.collection` with it.

## Uninstall

```bash
helm uninstall wyrd -n wyrd
```

This removes wyrd and its data volume. The bundled Qdrant's volume
(`qdrant-storage-<release>-qdrant-0`) is kept, as Kubernetes keeps every
StatefulSet volume: delete the claim to drop the index.

## Values

Every value is documented in [`values.yaml`](values.yaml) and typed in
[`values.schema.json`](values.schema.json); an unknown key or a combination
that cannot work (the vertex provider without a project, `qdrant.enabled:
false` without a URL) is refused when the chart is rendered.

| Key | Default | |
|---|---|---|
| `image.repository`, `image.tag` | `ghcr.io/kgatilin/wyrd`, the chart's appVersion | |
| `repo.path` | `/data/repo` | where the checkout is |
| `sync.initContainers`, `sync.containers` | `[]` | what checks the repository out; templated |
| `embedder.provider` | `vertex` | `vertex`, `openai`, `ollama` |
| `embedder.dim` | `768` | also the collection's dimension |
| `embedder.vertex.project` | — | required for `vertex` |
| `embedder.vertex.credentialsSecret` | — | ADC JSON, without workload identity |
| `qdrant.enabled` | `true` | bundle Qdrant; other keys go to its chart |
| `qdrantConnection.url` | — | required with `qdrant.enabled: false` |
| `qdrantConnection.collection` | `wyrd` | |
| `qdrantConnection.bootstrap.enabled` | `true` | create and verify the collection on start |
| `requireRemote` | `true` | fail instead of falling back to local files |
| `mcpAuth.tokenSecret` | — | bearer token for the MCP endpoint |
| `persistence.size` | `5Gi` | the data volume |
| `serviceAccount.annotations` | `{}` | workload identity |
| `ingress.enabled` | `false` | |
| `resources` | 250m / 1Gi requested, 4Gi limit | parsing a large repository is the peak |
| `extraEnv`, `extraEnvFrom`, `extraVolumes`, `extraVolumeMounts` | `[]` | |
