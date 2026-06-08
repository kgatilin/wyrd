import { useCallback, useEffect, useState } from 'react';

export interface ArchMotifPanelProps {
  worktree: string;
}

interface ArchMotifMetrics {
  schema: string;
  scope: string;
  nodes: number;
  edges: number;
  components: number;
  layeringScore: number;
  acyclic: boolean;
  cycles: ArchMotifCycle[];
  topCoupling: ArchMotifPackageMetric[];
  godPackages: ArchMotifPackageMetric[];
  packages: ArchMotifPackageMetric[];
  embeddings: ArchMotifEmbeddingStatus;
  warnings?: string[];
}

interface ArchMotifPackageMetric {
  id: string;
  name: string;
  group: string;
  layer?: string;
  aggregate?: string;
  fanIn: number;
  fanOut: number;
  degree: number;
  instability: number;
  dependsOn: string[];
  usedBy: string[];
}

interface ArchMotifCycle {
  packages: string[];
  edges: number;
}

interface ArchMotifEmbeddingStatus {
  textGraphPath?: string;
  vectorGraphPath?: string;
  hasTextGraph: boolean;
  hasVectors: boolean;
  vectorCount: number;
  updatedAt?: string;
  binary?: string;
}

interface ArchMotifEmbedResponse {
  status: 'ready' | 'failed' | 'unavailable';
  message: string;
  embeddings: ArchMotifEmbeddingStatus;
  stdout?: string;
  stderr?: string;
}

export function ArchMotifPanel({ worktree }: ArchMotifPanelProps) {
  const [metrics, setMetrics] = useState<ArchMotifMetrics | null>(null);
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading');
  const [error, setError] = useState<string | null>(null);
  const [embedding, setEmbedding] = useState<'idle' | 'running'>('idle');
  const [embedMessage, setEmbedMessage] = useState<string | null>(null);

  const loadMetrics = useCallback(async () => {
    setStatus('loading');
    setError(null);
    try {
      const res = await fetch(archMotifMetricsURL(worktree));
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg.trim() || `HTTP ${res.status}`);
      }
      const next = (await res.json()) as ArchMotifMetrics;
      setMetrics(next);
      setStatus('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setStatus('error');
    }
  }, [worktree]);

  useEffect(() => {
    void loadMetrics();
  }, [loadMetrics]);

  const runEmbeddings = async () => {
    if (embedding === 'running') return;
    setEmbedding('running');
    setEmbedMessage(null);
    try {
      const res = await fetch(archMotifEmbedURL(worktree), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      });
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg.trim() || `HTTP ${res.status}`);
      }
      const result = (await res.json()) as ArchMotifEmbedResponse;
      setEmbedMessage(result.message || result.status);
      await loadMetrics();
    } catch (err) {
      setEmbedMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setEmbedding('idle');
    }
  };

  if (status === 'loading' && !metrics) {
    return (
      <div className="hf-motif-panel">
        <div className="hf-motif-loading">
          <span className="hf-motif-spinner" aria-hidden="true" />
          Reading package graph
        </div>
      </div>
    );
  }

  if (status === 'error' && !metrics) {
    return (
      <div className="hf-motif-panel">
        <div className="hf-motif-error">{error ?? 'Failed to load ArchMotif metrics'}</div>
      </div>
    );
  }

  if (!metrics) return null;
  const cycles = metrics.cycles ?? [];
  const topCoupling = metrics.topCoupling ?? [];
  const godPackages = metrics.godPackages ?? [];

  return (
    <div className="hf-motif-panel">
      <div className="hf-motif-actions">
        <button className="hf-source-action" type="button" onClick={loadMetrics}>
          Refresh
        </button>
        <button
          className="hf-source-action primary"
          type="button"
          onClick={runEmbeddings}
          disabled={embedding === 'running'}
        >
          {embedding === 'running' ? 'Embedding...' : 'Embed'}
        </button>
      </div>

      {status === 'loading' && (
        <div className="hf-motif-inline">
          <span className="hf-motif-spinner" aria-hidden="true" />
          Refreshing metrics
        </div>
      )}
      {error && <div className="hf-motif-error">{error}</div>}
      {embedMessage && <div className="hf-motif-message">{embedMessage}</div>}

      <div className="hf-motif-grid">
        <Metric label="packages" value={metrics.nodes} />
        <Metric label="deps" value={metrics.edges} />
        <Metric label="parts" value={metrics.components} />
        <Metric label="layer" value={`${Math.round(metrics.layeringScore * 100)}%`} />
      </div>

      <section className="hf-motif-section">
        <div className="hf-motif-section-head">
          <span>Structure</span>
          <strong className={metrics.acyclic ? 'ok' : 'warn'}>{metrics.acyclic ? 'DAG' : `${cycles.length} cycles`}</strong>
        </div>
        {cycles.length === 0 ? (
          <div className="hf-motif-empty">No package cycles</div>
        ) : (
          cycles.slice(0, 4).map((cycle, idx) => (
            <div key={idx} className="hf-motif-row">
              <span className="hf-motif-row-main">{cycle.packages.join(' -> ')}</span>
              <span className="hf-motif-row-value">{cycle.edges}</span>
            </div>
          ))
        )}
      </section>

      <section className="hf-motif-section">
        <div className="hf-motif-section-head">
          <span>Coupling</span>
          <strong>{topCoupling.length}</strong>
        </div>
        {topCoupling.map((pkg) => (
          <PackageMetricRow key={pkg.id} metric={pkg} />
        ))}
      </section>

      {godPackages.length > 0 && (
        <section className="hf-motif-section">
          <div className="hf-motif-section-head">
            <span>Hotspots</span>
            <strong>{godPackages.length}</strong>
          </div>
          {godPackages.map((pkg) => (
            <PackageMetricRow key={pkg.id} metric={pkg} />
          ))}
        </section>
      )}

      <section className="hf-motif-section">
        <div className="hf-motif-section-head">
          <span>Embeddings</span>
          <strong className={metrics.embeddings.hasVectors ? 'ok' : 'warn'}>
            {metrics.embeddings.hasVectors ? `${metrics.embeddings.vectorCount} vec` : 'missing'}
          </strong>
        </div>
        <div className="hf-motif-kv">
          <span>text graph</span>
          <strong>{metrics.embeddings.hasTextGraph ? 'ready' : 'not generated'}</strong>
        </div>
        <div className="hf-motif-kv">
          <span>vector graph</span>
          <strong>{metrics.embeddings.hasVectors ? 'ready' : 'not generated'}</strong>
        </div>
        {metrics.embeddings.updatedAt && (
          <div className="hf-motif-kv">
            <span>updated</span>
            <strong>{new Date(metrics.embeddings.updatedAt).toLocaleString()}</strong>
          </div>
        )}
      </section>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="hf-motif-metric">
      <strong>{value}</strong>
      <span>{label}</span>
    </div>
  );
}

function PackageMetricRow({ metric }: { metric: ArchMotifPackageMetric }) {
  return (
    <div className="hf-motif-row">
      <span className="hf-motif-row-main" title={metric.id}>
        {metric.id}
      </span>
      <span className="hf-motif-row-value">
        {metric.fanIn}/{metric.fanOut}
      </span>
    </div>
  );
}

function archMotifMetricsURL(worktree: string): string {
  if (worktree) return `/w/${encodeURIComponent(worktree)}/api/archmotif/metrics`;
  return `${currentWorktreePrefix()}/api/archmotif/metrics`;
}

function archMotifEmbedURL(worktree: string): string {
  if (worktree) return `/w/${encodeURIComponent(worktree)}/api/archmotif/embed`;
  return `${currentWorktreePrefix()}/api/archmotif/embed`;
}

function currentWorktreePrefix(): string {
  if (typeof window === 'undefined') return '';
  const match = window.location.pathname.match(/^\/w\/[^/]+/);
  return match?.[0] ?? '';
}
