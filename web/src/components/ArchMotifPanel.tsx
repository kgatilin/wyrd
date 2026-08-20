import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchArchReport } from '../data/archReport';
import {
  baseLabel,
  indexNote,
  rowActions,
  totalsLabel,
  type ArchReport,
  type ReportAction,
  type ReportItem,
  type ReportMode,
  type ReportSection,
} from '../domain/archReport';

/**
 * The architecture review report: what this branch did to the structure and
 * where, or — with no base to compare against — what to refactor next.
 *
 * A section exists only for a state a reviewer acts on, so every row names a
 * finding, what to do about it, and where to click. A section whose state has
 * not occurred is one line: a clean branch reads as a handful of "none" lines
 * rather than a grid of figures nobody acts on. The figures that remain sit in
 * the muted footer, where nothing pretends they are a finding.
 */

export interface ArchReportSession {
  status: 'loading' | 'ready' | 'error';
  report: ArchReport | null;
  error: string | null;
  /** A re-read is in flight over a report already on screen. */
  refreshing: boolean;
  /**
   * Re-read the report — the Refresh button, and the model-changed SSE. Pass
   * `fresh` to make the daemon rebuild it rather than answer from its own
   * cache; the button does, the SSE does not, because the daemon has already
   * dropped and re-warmed its copy by the time that event arrives.
   */
  reload: (fresh?: boolean) => void;
}

/**
 * Fetches and caches the report, held by the app rather than by the panel: the
 * panel is an overlay the reviewer opens and closes, and re-reading both
 * package models on every open is seconds of daemon work for an answer that
 * has not changed.
 *
 * The daemon caches it too, warmed as each worktree's model finishes parsing,
 * so a first open no longer waits for a build. This cache still earns its place
 * above that one: the response carries no ETag, so even a warm answer is a
 * round trip and a re-render. It is dropped when the worktree or base changes,
 * and re-read on the same model-changed SSE that reloads the canvas: the report
 * and the canvas must never end up describing different working trees.
 */
export function useArchReportSession(
  worktree: string,
  baseRef: string,
  open: boolean
): ArchReportSession {
  const key = `${worktree}\n${baseRef}`;
  // The counter is what re-runs the read; `fresh` rides with it so the request
  // it triggers knows whether the reviewer asked for a rebuild.
  const [reloadState, setReloadState] = useState({ token: 0, fresh: false });
  const [data, setData] = useState<{
    status: 'loading' | 'ready' | 'error';
    report: ArchReport | null;
    error: string | null;
  }>({ status: 'loading', report: null, error: null });
  const [refreshing, setRefreshing] = useState(false);
  // Which report is loaded or in flight, worktree and base included. While
  // this matches, the effect below is a no-op — that is what makes reopening
  // the panel free, and a switch of worktree or base a fresh read.
  const loaded = useRef<string | null>(null);

  useEffect(() => {
    if (!open) return;
    const stamp = `${key}#${reloadState.token}`;
    if (loaded.current === stamp) return;
    // A reload of the same review is a refresh: the report on screen stays
    // while the daemon is asked again, instead of blinking back to a spinner
    // under a chatty model-changed stream.
    const isRefresh = loaded.current !== null && loaded.current.startsWith(`${key}#`);
    loaded.current = stamp;
    let cancelled = false;
    if (isRefresh) setRefreshing(true);
    else setData({ status: 'loading', report: null, error: null });
    fetchArchReport(worktree, baseRef, reloadState.fresh).then(
      (report) => {
        if (cancelled) return;
        setRefreshing(false);
        setData({ status: 'ready', report, error: null });
      },
      (err: unknown) => {
        if (cancelled) return;
        setRefreshing(false);
        // A failed read is not a cache entry: reopening should ask the daemon
        // again instead of replaying the same error. A failed *refresh* leaves
        // the last report the daemon actually answered on screen.
        loaded.current = null;
        if (isRefresh) return;
        setData({
          status: 'error',
          report: null,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    );
    return () => {
      cancelled = true;
    };
  }, [open, key, reloadState, worktree, baseRef]);

  const reload = useCallback(
    (fresh = false) => setReloadState((prev) => ({ token: prev.token + 1, fresh })),
    []
  );
  return { ...data, refreshing, reload };
}

export interface ArchMotifPanelProps {
  /** The cached session, owned by the app (see useArchReportSession). */
  session: ArchReportSession;
  /**
   * Runs a row's gesture on the canvas. One entry point rather than six
   * callbacks, because `rowActions` already decided what a row offers — the
   * app only has to carry each kind out.
   */
  onAction: (action: ReportAction) => void;
}

export function ArchMotifPanel({ session, onAction }: ArchMotifPanelProps) {
  const { report, status, error, refreshing, reload } = session;

  if (!report) {
    return (
      <div className="hf-report">
        {status === 'error' ? (
          <div className="hf-report-error">{error ?? 'Failed to read the review report'}</div>
        ) : (
          <div className="hf-report-loading">
            <span className="hf-report-spinner" aria-hidden="true" />
            Reading the architecture
          </div>
        )}
      </div>
    );
  }

  const note = indexNote(report.index);
  const base = baseLabel(report);
  const warnings = report.warnings ?? [];

  return (
    <div className="hf-report">
      <div className="hf-report-head">
        <span className={`hf-report-mode ${report.mode}`}>
          {report.mode === 'review' ? 'this branch' : 'whole repository'}
        </span>
        {base && <span className="hf-report-base">{base}</span>}
        <span className="hf-spacer" />
        <button className="hf-source-action" type="button" onClick={() => reload(true)}>
          Refresh
        </button>
      </div>

      {refreshing && (
        <div className="hf-report-inline">
          <span className="hf-report-spinner" aria-hidden="true" />
          Refreshing
        </div>
      )}

      {/* A section that could not run is not a section that found nothing. */}
      {warnings.map((warning, index) => (
        <div key={index} className="hf-report-warning">
          did not run: {warning}
        </div>
      ))}

      {note && <div className="hf-report-index">{note}</div>}

      {report.sections.map((section) => (
        <SectionView key={section.id} mode={report.mode} section={section} onAction={onAction} />
      ))}

      <div className="hf-report-totals">{totalsLabel(report.totals)}</div>
    </div>
  );
}

function SectionView({
  mode,
  section,
  onAction,
}: {
  mode: ReportMode;
  section: ReportSection;
  onAction: (action: ReportAction) => void;
}) {
  if (section.state === 'ok') {
    return (
      <div className="hf-report-section ok" data-section={section.id}>
        <span className="hf-report-section-title">{section.title}</span>
        <span className="hf-report-summary">{section.summary}</span>
      </div>
    );
  }
  return (
    <section className="hf-report-section flag" data-section={section.id}>
      <div className="hf-report-section-head">
        <span className="hf-report-section-title">{section.title}</span>
        <strong className="hf-report-count">{section.count}</strong>
      </div>
      <div className="hf-report-summary">{section.summary}</div>
      {section.items.map((item, index) => (
        <RowView
          key={`${section.id}:${index}`}
          mode={mode}
          section={section}
          item={item}
          onAction={onAction}
        />
      ))}
      {section.more ? <div className="hf-report-more">and {section.more} more</div> : null}
    </section>
  );
}

function RowView({
  mode,
  section,
  item,
  onAction,
}: {
  mode: ReportMode;
  section: ReportSection;
  item: ReportItem;
  onAction: (action: ReportAction) => void;
}) {
  const actions = rowActions(mode, section, item);
  return (
    <div className="hf-report-row">
      <button
        className="hf-report-row-main"
        type="button"
        disabled={!actions.primary}
        title={actions.primary ? actionTitle(actions.primary) : item.text}
        onClick={() => actions.primary && onAction(actions.primary)}
      >
        <span className="hf-report-row-head">
          <span className="hf-report-row-text">{item.text}</span>
          {item.tag && <span className={`hf-report-tag ${item.tag}`}>{item.tag}</span>}
        </span>
        {item.detail && <span className="hf-report-row-detail">{item.detail}</span>}
      </button>
      {actions.extra.length > 0 && (
        <div className="hf-report-row-acts">
          {actions.extra.map((action) => (
            <button
              key={action.kind}
              className={`hf-report-act ${action.kind}`}
              type="button"
              title={actionTitle(action)}
              onClick={() => onAction(action)}
            >
              {actionGlyph(action)}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function actionGlyph(action: ReportAction): string {
  switch (action.kind) {
    case 'focus':
      return '⌖';
    case 'wiring':
      return '⇄';
    case 'highlight':
      return '◎';
    case 'diff':
      return '±';
    case 'source':
      return '<>';
    case 'domains':
      return 'Domains';
  }
}

function actionTitle(action: ReportAction): string {
  switch (action.kind) {
    case 'focus':
      return `Show ${action.componentId} on the canvas`;
    case 'wiring':
      return `Open the wiring of ${action.memberId ?? action.internalId}`;
    case 'highlight':
      return action.edges.length === 1
        ? `Accent ${action.edges[0].from} → ${action.edges[0].to} on the canvas`
        : `Accent all ${action.edges.length} edges of this cycle on the canvas`;
    case 'diff':
      return `Open the patch of ${action.file}`;
    case 'source':
      return `Read ${action.file}`;
    case 'domains':
      return `Cluster ${action.package} structurally and semantically`;
  }
}
