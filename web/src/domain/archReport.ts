/**
 * The architecture review report (`archai.archreview/v1`) and the one mapping
 * the panel needs: report row → canvas action.
 *
 * The server already speaks uigraph's id conventions — a component id is a
 * package path, an internal id is `{package}.{Symbol}`, a file is
 * module-relative — so nothing here translates ids. What it decides is which
 * gesture a row offers, which is a reading of the row's target rather than of
 * its wording, and therefore the same decision for every section.
 */

export type ReportMode = 'review' | 'repo';
export type ReportState = 'ok' | 'flag';

export interface ArchReport {
  schema: string;
  mode: ReportMode;
  base?: ReportBase;
  sections: ReportSection[];
  totals: ReportTotals;
  index?: ReportIndex;
  /**
   * Analysis that could not run. A missing section is not a clean one, so the
   * panel shows these instead of letting silence read as a pass.
   */
  warnings?: string[];
}

export interface ReportBase {
  ref: string;
  rev: string;
}

export interface ReportSection {
  id: string;
  title: string;
  severity: number;
  state: ReportState;
  count: number;
  summary: string;
  items: ReportItem[];
  more?: number;
}

export interface ReportItem {
  text: string;
  detail?: string;
  tag?: string;
  target: ReportTarget;
}

export interface ReportTarget {
  componentId?: string;
  internalId?: string;
  memberId?: string;
  file?: string;
  edge?: ReportEdge;
  edges?: ReportEdge[];
}

export interface ReportEdge {
  from: string;
  to: string;
}

export interface ReportTotals {
  packages: number;
  edges: number;
  components: number;
}

export interface ReportIndex {
  ready: boolean;
  indexing: boolean;
  embedded: number;
  embeddable: number;
  denseAvailable: boolean;
}

/** The section whose rows offer the domains canvas (see `rowActions`). */
const SECTION_GOD_PACKAGES = 'god_packages';

/**
 * One gesture a row offers. `highlight` carries a package to focus alongside
 * the edges: focusing pulls a package and its edge neighbours into the review
 * projection, so the edge the row tells you to cut is actually drawn instead
 * of accented somewhere off the canvas.
 */
export type ReportAction =
  | { kind: 'focus'; componentId: string }
  | { kind: 'wiring'; componentId: string; internalId: string; memberId?: string }
  | { kind: 'highlight'; edges: ReportEdge[]; focus: string | null }
  | { kind: 'diff'; file: string }
  | { kind: 'source'; file: string }
  | { kind: 'domains'; package: string };

export interface ReportRowActions {
  /** What clicking the row itself does. Null when the row names no target. */
  primary: ReportAction | null;
  /** Further gestures, rendered as buttons beside the row. */
  extra: ReportAction[];
}

/**
 * The gestures a row offers, most specific target first: a row about edges
 * highlights them, a row about a symbol opens its wiring, a row about a file
 * opens it, and a row about nothing but a package puts that package on the
 * canvas.
 *
 * The mode decides which way round a file row reads. In review mode the
 * finding is that a changed file grew, so the patch comes first; in repo mode
 * there is no base and usually no patch, so the file itself does. Both
 * gestures stay on the row either way.
 *
 * Reading the target rather than the section id is what keeps this one rule:
 * "New group cycles" and "Group cycles" are the same gesture because they
 * carry the same shape of target, and a section added later inherits the
 * mapping without touching this file. The one exception is the god-package
 * row, whose action — ask that package for its latent domains — is the
 * section's finding rather than the target's shape.
 */
export function rowActions(
  mode: ReportMode,
  section: ReportSection,
  item: ReportItem
): ReportRowActions {
  const target = item.target ?? {};
  const edges = edgesOf(target);
  const extra: ReportAction[] = [];
  let primary: ReportAction | null = null;

  if (edges.length > 0) {
    primary = { kind: 'highlight', edges, focus: target.componentId ?? edges[0].from };
  } else if (target.internalId) {
    primary = {
      kind: 'wiring',
      componentId: target.componentId ?? packageOf(target.internalId),
      internalId: target.internalId,
      ...(target.memberId ? { memberId: target.memberId } : {}),
    };
  } else if (target.file) {
    primary =
      mode === 'review'
        ? { kind: 'diff', file: target.file }
        : { kind: 'source', file: target.file };
  } else if (target.componentId) {
    primary = { kind: 'focus', componentId: target.componentId };
  }

  // Whichever of the two the mode put first, the other stays available.
  if (target.file) {
    extra.push(
      primary?.kind === 'diff'
        ? { kind: 'source', file: target.file }
        : { kind: 'diff', file: target.file }
    );
  }

  // Whatever else the row points at, the package it lives in is a place to
  // stand on the canvas.
  if (target.componentId && primary?.kind !== 'focus') {
    extra.push({ kind: 'focus', componentId: target.componentId });
  }

  if (section.id === SECTION_GOD_PACKAGES && target.componentId) {
    extra.push({ kind: 'domains', package: target.componentId });
  }

  return { primary, extra };
}

/**
 * What the index status says while it stands between the reviewer and an
 * answer. The report omits the status once the index is ready, so anything
 * that arrives here is worth a line.
 */
export function indexNote(index: ReportIndex | undefined): string | null {
  if (!index || index.ready) return null;
  if (!index.denseAvailable) {
    return 'no embedder configured — the semantic lenses cannot run';
  }
  if (index.indexing) {
    return `indexing ${index.embedded}/${index.embeddable} — the semantic lenses answer once this finishes`;
  }
  return `${index.embedded}/${index.embeddable} embedded`;
}

/** The comparison a review-mode report was built against, as a header reads it. */
export function baseLabel(report: ArchReport): string | null {
  if (report.mode !== 'review' || !report.base) return null;
  const rev = report.base.rev ? ` @ ${report.base.rev.slice(0, 7)}` : '';
  return `vs ${report.base.ref}${rev}`;
}

/** The muted footer: the size of the graph the sections were read off. */
export function totalsLabel(totals: ReportTotals): string {
  const parts = [
    plural(totals.packages, 'package', 'packages'),
    plural(totals.edges, 'dependency', 'dependencies'),
  ];
  // Only worth a word when the package graph is in more than one piece; the
  // Islands section, which counts the pieces beyond the main one, owns the
  // finding itself.
  if (totals.components > 1) parts.push(`${totals.components} parts`);
  return parts.join(' · ');
}

function edgesOf(target: ReportTarget): ReportEdge[] {
  if (target.edges && target.edges.length > 0) return target.edges;
  if (target.edge) return [target.edge];
  return [];
}

/**
 * The package half of an internal id. Package paths contain dots, so the split
 * is the longest prefix before the final one — the same convention the id is
 * built with. Only reached when the server left `componentId` off a row, which
 * it does not do today.
 */
function packageOf(internalId: string): string {
  const cut = internalId.lastIndexOf('.');
  return cut === -1 ? internalId : internalId.slice(0, cut);
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}
