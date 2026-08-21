import { describe, expect, it } from 'vitest';
import {
  baseLabel,
  indexNote,
  rowActions,
  totalsLabel,
  type ArchReport,
  type ReportItem,
  type ReportSection,
  type ReportTarget,
} from './archReport';

function sectionOf(id: string): ReportSection {
  return { id, title: id, severity: 10, state: 'flag', count: 1, summary: '', items: [] };
}

function itemOf(target: ReportTarget): ReportItem {
  return { text: 'row', target };
}

describe('rowActions', () => {
  it('highlights every edge of a cycle and lands on the package it names', () => {
    const edges = [
      { from: 'internal/serve', to: 'internal/adapter/http' },
      { from: 'internal/adapter/http', to: 'internal/serve' },
    ];
    const actions = rowActions(
      'repo',
      sectionOf('group_cycles'),
      itemOf({ edges, componentId: 'internal/adapter/http' })
    );

    expect(actions.primary).toEqual({
      kind: 'highlight',
      edges,
      focus: 'internal/adapter/http',
    });
    // Accenting an edge nothing draws answers nothing, so the row also offers
    // the package on its own.
    expect(actions.extra).toEqual([{ kind: 'focus', componentId: 'internal/adapter/http' }]);
  });

  it('falls back to the first edge’s source when a cycle names no package', () => {
    const edges = [{ from: 'a', to: 'b' }];
    const actions = rowActions('repo', sectionOf('group_cycles'), itemOf({ edges }));
    expect(actions.primary).toEqual({ kind: 'highlight', edges, focus: 'a' });
    expect(actions.extra).toEqual([]);
  });

  it('treats a single new dependency as a one-edge highlight', () => {
    const edge = { from: 'internal/adapter/mcp', to: 'internal/serve' };
    const actions = rowActions(
      'review',
      sectionOf('edges_new'),
      itemOf({ edge, componentId: 'internal/adapter/mcp' })
    );
    expect(actions.primary).toEqual({
      kind: 'highlight',
      edges: [edge],
      focus: 'internal/adapter/mcp',
    });
  });

  it('opens a symbol’s wiring, and a member’s wiring scoped to the member', () => {
    const symbol = rowActions(
      'repo',
      sectionOf('unused_exports'),
      itemOf({ componentId: 'internal/serve', internalId: 'internal/serve.Warm' })
    );
    expect(symbol.primary).toEqual({
      kind: 'wiring',
      componentId: 'internal/serve',
      internalId: 'internal/serve.Warm',
    });
    expect(symbol.extra).toEqual([{ kind: 'focus', componentId: 'internal/serve' }]);

    const member = rowActions(
      'repo',
      sectionOf('inversions'),
      itemOf({
        componentId: 'internal/serve',
        internalId: 'internal/serve.State',
        memberId: 'internal/serve.State.Snapshot',
      })
    );
    expect(member.primary).toEqual({
      kind: 'wiring',
      componentId: 'internal/serve',
      internalId: 'internal/serve.State',
      memberId: 'internal/serve.State.Snapshot',
    });
  });

  it('opens a changed file at its patch, and an unchanged one at its code', () => {
    const target = {
      file: 'internal/adapter/mcp/tools.go',
      componentId: 'internal/adapter/mcp',
    };

    // Review mode: the finding is that a changed file grew.
    const review = rowActions('review', sectionOf('hotspot_growth'), itemOf(target));
    expect(review.primary).toEqual({ kind: 'diff', file: target.file });
    expect(review.extra).toEqual([
      { kind: 'source', file: target.file },
      { kind: 'focus', componentId: target.componentId },
    ]);

    // Repo mode: no base, so usually no patch — the file itself comes first.
    const repo = rowActions('repo', sectionOf('god_files'), itemOf(target));
    expect(repo.primary).toEqual({ kind: 'source', file: target.file });
    expect(repo.extra).toEqual([
      { kind: 'diff', file: target.file },
      { kind: 'focus', componentId: target.componentId },
    ]);
  });

  it('puts a bare package on the canvas', () => {
    const actions = rowActions('repo', sectionOf('islands'), itemOf({ componentId: 'internal/plugins' }));
    expect(actions.primary).toEqual({ kind: 'focus', componentId: 'internal/plugins' });
    expect(actions.extra).toEqual([]);
  });

  it('offers the domains canvas on a god package, and nowhere else', () => {
    const god = rowActions(
      'repo',
      sectionOf('god_packages'),
      itemOf({ componentId: 'internal/adapter/mcp' })
    );
    expect(god.primary).toEqual({ kind: 'focus', componentId: 'internal/adapter/mcp' });
    expect(god.extra).toEqual([{ kind: 'domains', package: 'internal/adapter/mcp' }]);

    // Hotspot growth reports the same shape of row for a different finding:
    // the action there is to put the new dependencies elsewhere, not to
    // re-cluster the package.
    const hotspot = rowActions(
      'review',
      sectionOf('hotspot_growth'),
      itemOf({ componentId: 'internal/adapter/mcp' })
    );
    expect(hotspot.extra).toEqual([]);
  });

  it('leaves a row without a target unclickable', () => {
    const actions = rowActions('review', sectionOf('impact'), itemOf({}));
    expect(actions.primary).toBeNull();
    expect(actions.extra).toEqual([]);
  });
});

describe('indexNote', () => {
  const base = { ready: false, indexing: false, embedded: 0, embeddable: 0, denseAvailable: true };

  it('says nothing once the index is ready, or absent', () => {
    expect(indexNote(undefined)).toBeNull();
    expect(indexNote({ ...base, ready: true })).toBeNull();
  });

  it('names the two ways the index gets in the way', () => {
    expect(indexNote({ ...base, denseAvailable: false })).toContain('no embedder');
    expect(indexNote({ ...base, indexing: true, embedded: 120, embeddable: 430 })).toContain(
      'indexing 120/430'
    );
  });
});

describe('report header', () => {
  const report = (mode: 'review' | 'repo', base?: { ref: string; rev: string }): ArchReport => ({
    schema: 'wyrd.archreview/v1',
    mode,
    base,
    sections: [],
    totals: { packages: 0, edges: 0, components: 0 },
  });

  it('names the comparison in review mode only', () => {
    expect(baseLabel(report('review', { ref: 'main', rev: 'cc451e9abc' }))).toBe('vs main @ cc451e9');
    expect(baseLabel(report('repo'))).toBeNull();
  });

  it('mentions the pieces of the package graph only when there is more than one', () => {
    expect(totalsLabel({ packages: 1, edges: 1, components: 1 })).toBe('1 package · 1 dependency');
    expect(totalsLabel({ packages: 42, edges: 96, components: 3 })).toBe(
      '42 packages · 96 dependencies · 3 parts'
    );
  });
});
