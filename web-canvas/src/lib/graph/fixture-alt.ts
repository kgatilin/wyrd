/**
 * A second, deliberately different fixture UIGraph — a retrieval/embedding
 * pipeline — used to demonstrate multiple distinct graph widgets in one
 * document.
 */
import type { UIGraph } from './types';

export const fixtureGraphAlt: UIGraph = {
  schema: 'archai-uigraph-v1',
  boundedContexts: [
    { id: 'retrieval', name: 'Retrieval' },
  ],
  components: [
    {
      id: 'internal/embed',
      name: 'embed',
      tech: 'Go - Ollama',
      desc: 'Embedding provider (Ollama / OpenAI / noop)',
      bc: 'retrieval',
      internals: [
        {
          id: 'internal/embed.Embedder',
          kind: 'iface',
          name: 'Embedder',
          exported: true,
          members: [
            { id: 'internal/embed.Embedder.Embed', kind: 'method', name: 'Embed(ctx, text string) ([]float32, error)', exported: true },
            { id: 'internal/embed.Embedder.Batch', kind: 'method', name: 'Batch(ctx, texts []string) ([][]float32, error)', exported: true, diff: 'removed' },
          ],
        },
        {
          id: 'internal/embed.Ollama',
          kind: 'class',
          name: 'Ollama',
          exported: true,
          members: [
            { id: 'internal/embed.Ollama.model', kind: 'prop', name: 'model string', exported: false },
            { id: 'internal/embed.Ollama.Embed', kind: 'method', name: 'Embed(ctx, text string) ([]float32, error)', exported: true },
          ],
        },
      ],
      ports: [
        { id: 'internal/embed:out:Embedder', side: 'right', kind: 'out', name: 'Embedder' },
      ],
    },
    {
      id: 'internal/index',
      name: 'index',
      tech: 'Go',
      desc: 'Dense vector store + kNN search',
      bc: 'retrieval',
      internals: [
        {
          id: 'internal/index.VectorStore',
          kind: 'class',
          name: 'VectorStore',
          exported: true,
          members: [
            { id: 'internal/index.VectorStore.Upsert', kind: 'method', name: 'Upsert(id string, vec []float32)', exported: true },
            { id: 'internal/index.VectorStore.KNN', kind: 'method', name: 'KNN(vec []float32, k int) []Hit', exported: true },
          ],
        },
      ],
      ports: [
        { id: 'internal/index:out:VectorStore', side: 'right', kind: 'out', name: 'VectorStore' },
      ],
    },
    {
      id: 'internal/retrieval',
      name: 'retrieval',
      tech: 'Go',
      desc: 'Retrieval service — embeds queries and ranks results',
      bc: 'retrieval',
      diff: 'added',
      internals: [
        {
          id: 'internal/retrieval.Retriever',
          kind: 'iface',
          name: 'Retriever',
          exported: true,
          diff: 'added',
          members: [
            { id: 'internal/retrieval.Retriever.Search', kind: 'method', name: 'Search(ctx, q string, k int) ([]Hit, error)', exported: true, diff: 'added' },
          ],
        },
        {
          id: 'internal/retrieval.Service',
          kind: 'class',
          name: 'Service',
          exported: true,
          diff: 'added',
          members: [
            { id: 'internal/retrieval.Service.embed', kind: 'prop', name: 'embed Embedder', exported: false, diff: 'added' },
            { id: 'internal/retrieval.Service.store', kind: 'prop', name: 'store *VectorStore', exported: false, diff: 'added' },
            { id: 'internal/retrieval.Service.Search', kind: 'method', name: 'Search(ctx, q string, k int) ([]Hit, error)', exported: true, diff: 'added' },
          ],
        },
      ],
      ports: [
        { id: 'internal/retrieval:in:embed', side: 'left', kind: 'in', name: 'embed' },
        { id: 'internal/retrieval:in:index', side: 'left', kind: 'in', name: 'index' },
      ],
    },
  ],
  edges: [
    {
      id: 'edge:embed->retrieval',
      from: 'internal/embed',
      to: 'internal/retrieval',
      fromPort: 'internal/embed:out:Embedder',
      toPort: 'internal/retrieval:in:embed',
      label: 'embeds',
      diff: 'added',
    },
    {
      id: 'edge:index->retrieval',
      from: 'internal/index',
      to: 'internal/retrieval',
      fromPort: 'internal/index:out:VectorStore',
      toPort: 'internal/retrieval:in:index',
      label: 'ranks',
      diff: 'added',
    },
  ],
  relations: [
    {
      id: 'rel:retrieval-impl',
      kind: 'implements',
      fromComponentId: 'internal/retrieval',
      fromInternalId: 'internal/retrieval.Service',
      toComponentId: 'internal/retrieval',
      toInternalId: 'internal/retrieval.Retriever',
    },
  ],
};
