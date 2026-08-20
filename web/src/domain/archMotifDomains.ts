import type { Diff, Internal, UIGraph } from '../types';

/**
 * The domains grid: one partition of the code laid against the other.
 *
 * `latent_domains` clusters the same symbols twice — structurally (who calls
 * whom) and semantically (what the code is about) — and reports how much the
 * two agree. This model turns that verdict into the thing it is a verdict
 * about: a contingency grid, structural clusters down the side, semantic
 * clusters across the top, each occupied intersection a cell of symbols.
 *
 * Reading it: a diagonal-heavy grid means the module boundaries match the
 * subject matter. One row smeared across many columns means the opposite — a
 * structural blob holding several semantic domains, fused by shared helpers
 * the grid names as glue.
 *
 * Pure: no React, no fetching. It takes the lens payload plus the loaded
 * graph (which supplies the kinds, the diff marks and the flow edges) and
 * returns geometry the view only has to paint.
 */

// ── Wire shapes ───────────────────────────────────────────────────────────
// snake_case throughout: this is the daemon's payload, not the app model.

/**
 * One side's clustering, as a label per node rather than a member list per
 * cluster. `labels[i]` is the cluster of `nodes[i]`; -1 means this side did not
 * place that node.
 *
 * The ids live in `RawLatentDomains.nodes`, once, however many sides read them.
 * Repeating them per side is what used to push the payload past a quarter of a
 * megabyte on a real repository.
 */
export interface RawDomainPartition {
  k: number;
  cluster_count: number;
  dominant_share: number;
  modularity: number;
  labels: number[];
}

export interface RawGlueNode {
  node: string;
  fan_in: number;
  semantic_cluster: number;
}

export interface RawLatentDomains {
  /** The analysed symbols, as archmotif node ids. Both label arrays index it. */
  nodes: string[];
  node_count: number;
  structural: RawDomainPartition;
  semantic: RawDomainPartition;
  agreement: { ami: number; nmi: number; verdict: string };
  glue: { top_fan_in?: RawGlueNode[]; glue_cluster: number; note: string };
  dropped_nodes: number;
  diff_region?: { seed_count?: number; region_size?: number; conductance?: number };
}

// ── Model ─────────────────────────────────────────────────────────────────

/** One symbol in a cell. */
export interface DomainMember {
  /** archmotif node id (`type:internal/domain.PackageModel`) — the lens's key. */
  id: string;
  /** uigraph `Internal.id` (`{package}.{Symbol}`) — the canvas's key. */
  internalId: string;
  /** Package the symbol lives in; '' when it could not be resolved. */
  componentId: string;
  name: string;
  /** Card glyph kind, from the graph when resolved. */
  kind: string;
  /** Incoming flow edges from inside the analysed set; 0 unless glue. */
  fanIn: number;
  /** Among the lens's top structural fan-in nodes — the glue to pull out. */
  glue: boolean;
  /** The review marks this symbol as changed. */
  diff?: Diff;
  /** Resolvable on the canvas, so a click can open its wiring. */
  inGraph: boolean;
}

/** Members of one cell that share a package — the card's section header. */
export interface DomainPackageBlock {
  componentId: string;
  name: string;
  members: DomainMember[];
}

/** One occupied (structural × semantic) intersection, drawn as a card. */
export interface DomainCell {
  id: string;
  row: number;
  col: number;
  structuralCluster: number;
  semanticCluster: number;
  /** Total members, including any the card does not have room to list. */
  size: number;
  /** The members the card lists, in package order. */
  packages: DomainPackageBlock[];
  /** Members beyond the card's room. */
  overflow: number;
  glueCount: number;
  changedCount: number;
  /** Sits on the leading diagonal: the two partitions agree about this group. */
  onDiagonal: boolean;
  x: number;
  y: number;
  width: number;
  height: number;
}

/** One structural row or semantic column of the grid. */
export interface DomainAxis {
  cluster: number;
  index: number;
  /** Members of this cluster that landed in the grid. */
  size: number;
  label: string;
  /** Top-left of the band, in grid coordinates. */
  offset: number;
  /** Height (row) or width (column) of the band. */
  extent: number;
}

/** Flow between two cells, aggregated: one line per ordered cell pair. */
export interface DomainCellEdge {
  id: string;
  from: string;
  to: string;
  weight: number;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

/** What the header says, straight off the lens. */
export interface DomainHeader {
  verdict: string;
  ami: number;
  nmi: number;
  structuralK: number;
  semanticK: number;
  structuralModularity: number;
  semanticModularity: number;
  structuralDominantShare: number;
  semanticDominantShare: number;
  nodeCount: number;
  droppedNodes: number;
  glue: DomainMember[];
  glueNote: string;
}

/** Where the branch's changes land in the grid. */
export interface DomainDiffSummary {
  changedMembers: number;
  /** One cell = a local change; many = the change cuts across domains. */
  cells: number;
  structuralClusters: number;
  semanticClusters: number;
}

export interface DomainGrid {
  rows: DomainAxis[];
  cols: DomainAxis[];
  cells: DomainCell[];
  edges: DomainCellEdge[];
  header: DomainHeader;
  diff: DomainDiffSummary;
  width: number;
  height: number;
  rowHeaderWidth: number;
  colHeaderHeight: number;
  /** Symbols one partition placed and the other did not. Normally 0. */
  unplaced: number;
}

// Card metrics. A cell's size follows its member count so a big group reads as
// one, with a floor so a two-symbol cell is still a card and not a sliver.
const CELL_MIN_W = 190;
const CELL_MAX_W = 300;
const CELL_MIN_H = 84;
const CARD_HEAD_H = 24;
const PKG_HEAD_H = 17;
const ROW_H = 17;
const CARD_PAD = 12;
const MAX_VISIBLE_MEMBERS = 12;
const GRID_GAP = 10;
const ROW_HEADER_W = 96;
const COL_HEADER_H = 34;

/**
 * Assemble the grid. `graph` supplies what the lens does not carry — each
 * symbol's kind and diff mark, and the flow edges between cells — so a member
 * the loaded graph cannot place is flagged rather than dropped.
 */
export function buildDomainGrid(raw: RawLatentDomains, graph: UIGraph | null): DomainGrid {
  const index = buildGraphIndex(graph);
  const fanIn = new Map<string, number>();
  const glueIds = new Set<string>();
  for (const node of raw.glue?.top_fan_in ?? []) {
    fanIn.set(node.node, node.fan_in);
    glueIds.add(node.node);
  }

  // Contingency: every symbol the two partitions both placed, keyed by pair.
  // Both sides label the same node list by position, so the join is an index
  // rather than a lookup — and a symbol either side left unplaced (-1) has no
  // intersection to sit in.
  const nodes = raw.nodes ?? [];
  const structuralLabels = raw.structural?.labels ?? [];
  const semanticLabels = raw.semantic?.labels ?? [];
  const buckets = new Map<string, { row: number; col: number; ids: string[] }>();
  let unplaced = 0;
  for (let i = 0; i < nodes.length; i++) {
    const structuralCluster = structuralLabels[i];
    const semanticCluster = semanticLabels[i];
    if (structuralCluster == null || semanticCluster == null || structuralCluster < 0 || semanticCluster < 0) {
      unplaced++;
      continue;
    }
    const key = cellId(structuralCluster, semanticCluster);
    let bucket = buckets.get(key);
    if (!bucket) {
      bucket = { row: structuralCluster, col: semanticCluster, ids: [] };
      buckets.set(key, bucket);
    }
    bucket.ids.push(nodes[i]);
  }

  const overlap = new Map<string, number>();
  for (const [key, bucket] of buckets) overlap.set(key, bucket.ids.length);

  const rowSizes = countBy(buckets, (bucket) => bucket.row);
  const colSizes = countBy(buckets, (bucket) => bucket.col);
  const { rowOrder, colOrder } = orderDiagonally(rowSizes, colSizes, overlap);

  const rowIndex = positionOf(rowOrder);
  const colIndex = positionOf(colOrder);

  // Empty intersections never become cells: the grid collapses to what exists.
  const cells: DomainCell[] = [];
  // Every member's cell, not just the ones a card has room to list — a cell's
  // flow is the whole group's, and a weight that counted only the visible
  // twelve would shrink as a cell grew.
  const cellOfMember = new Map<string, string>();
  for (const [key, bucket] of buckets) {
    const members = bucket.ids
      .map((id) => resolveMember(id, index, fanIn, glueIds))
      .sort(compareMembers);
    for (const member of members) cellOfMember.set(member.internalId, key);
    const visible = members.slice(0, MAX_VISIBLE_MEMBERS);
    const packages = groupByPackage(visible);
    const row = rowIndex.get(bucket.row) ?? 0;
    const col = colIndex.get(bucket.col) ?? 0;
    cells.push({
      id: key,
      row,
      col,
      structuralCluster: bucket.row,
      semanticCluster: bucket.col,
      size: members.length,
      packages,
      overflow: members.length - visible.length,
      glueCount: members.filter((member) => member.glue).length,
      changedCount: members.filter((member) => member.diff != null).length,
      onDiagonal: row === col,
      x: 0,
      y: 0,
      width: cellWidth(members.length),
      height: cellHeight(packages.length, visible.length, members.length > visible.length),
    });
  }

  const rows = buildAxis(rowOrder, rowSizes, 'S', cells, (cell) => cell.row, (cell) => cell.height, COL_HEADER_H);
  const cols = buildAxis(colOrder, colSizes, 'M', cells, (cell) => cell.col, (cell) => cell.width, ROW_HEADER_W);
  for (const cell of cells) {
    const rowBand = rows[cell.row];
    const colBand = cols[cell.col];
    cell.x = colBand.offset;
    cell.y = rowBand.offset;
    cell.width = colBand.extent;
    cell.height = rowBand.extent;
  }
  cells.sort((a, b) => a.row - b.row || a.col - b.col);

  const width = cols.length === 0 ? ROW_HEADER_W : cols[cols.length - 1].offset + cols[cols.length - 1].extent + GRID_GAP;
  const height = rows.length === 0 ? COL_HEADER_H : rows[rows.length - 1].offset + rows[rows.length - 1].extent + GRID_GAP;

  return {
    rows,
    cols,
    cells,
    edges: aggregateCellEdges(cells, cellOfMember, graph),
    header: buildHeader(raw, index, fanIn),
    diff: summarizeDiff(cells),
    width,
    height,
    rowHeaderWidth: ROW_HEADER_W,
    colHeaderHeight: COL_HEADER_H,
    unplaced,
  };
}

/**
 * The query a scope puts on the domains endpoint. Diff scope asks the daemon
 * for the change region; repo scope drops methods and fields (a few hundred
 * nodes instead of thousands, which is what keeps the O(n²) similarity pass and
 * the two spectral solves answerable at repository scale).
 */
export function domainsQueryForScope(scope: ArchMotifScopeInput): Record<string, string> {
  switch (scope.kind) {
    case 'diff':
      return { scope: 'diff' };
    case 'package':
      return { scope: 'package', package: scope.package ?? '' };
    default:
      return { scope: 'repo' };
  }
}

/** The scope shape the model needs; the app's state carries the same fields. */
export interface ArchMotifScopeInput {
  kind: 'diff' | 'repo' | 'package';
  package?: string;
}

// ── Internals ─────────────────────────────────────────────────────────────

function countBy(
  buckets: Map<string, { row: number; col: number; ids: string[] }>,
  pick: (bucket: { row: number; col: number; ids: string[] }) => number
): Map<number, number> {
  const sizes = new Map<number, number>();
  for (const bucket of buckets.values()) {
    const key = pick(bucket);
    sizes.set(key, (sizes.get(key) ?? 0) + bucket.ids.length);
  }
  return sizes;
}

/**
 * Greedy max-overlap ordering: the best-matching (structural, semantic) pair
 * takes position 0 on both axes, the best remaining pair position 1, and so
 * on. Clusters the other side never matches are appended largest-first. This
 * is what puts agreement on the diagonal, so disagreement is what you see off
 * it — with an arbitrary order, every grid looks scattered.
 *
 * Both orders come out of ONE pass. Ranking each axis separately would let a
 * tie break differently on the two sides, and the pair the row order calls
 * best would land off the column order's diagonal — the one thing the ordering
 * exists to prevent.
 */
function orderDiagonally(
  rowSizes: Map<number, number>,
  colSizes: Map<number, number>,
  overlap: Map<string, number>
): { rowOrder: number[]; colOrder: number[] } {
  const pairs: { row: number; col: number; weight: number }[] = [];
  for (const row of rowSizes.keys()) {
    for (const col of colSizes.keys()) {
      const weight = overlap.get(cellId(row, col)) ?? 0;
      if (weight > 0) pairs.push({ row, col, weight });
    }
  }
  pairs.sort(
    (a, b) =>
      b.weight - a.weight ||
      (rowSizes.get(b.row) ?? 0) - (rowSizes.get(a.row) ?? 0) ||
      (colSizes.get(b.col) ?? 0) - (colSizes.get(a.col) ?? 0) ||
      a.row - b.row ||
      a.col - b.col
  );

  const rowOrder: number[] = [];
  const colOrder: number[] = [];
  const placedRows = new Set<number>();
  const placedCols = new Set<number>();
  for (const pair of pairs) {
    if (placedRows.has(pair.row) || placedCols.has(pair.col)) continue;
    placedRows.add(pair.row);
    placedCols.add(pair.col);
    rowOrder.push(pair.row);
    colOrder.push(pair.col);
  }
  return {
    rowOrder: [...rowOrder, ...leftovers(rowSizes, placedRows)],
    colOrder: [...colOrder, ...leftovers(colSizes, placedCols)],
  };
}

/** Clusters the greedy pass never paired, largest first. */
function leftovers(sizes: Map<number, number>, placed: Set<number>): number[] {
  return [...sizes.keys()]
    .filter((cluster) => !placed.has(cluster))
    .sort((a, b) => (sizes.get(b) ?? 0) - (sizes.get(a) ?? 0) || a - b);
}

function positionOf(order: number[]): Map<number, number> {
  const at = new Map<number, number>();
  order.forEach((cluster, index) => at.set(cluster, index));
  return at;
}

function buildAxis(
  order: number[],
  sizes: Map<number, number>,
  prefix: string,
  cells: DomainCell[],
  bandOf: (cell: DomainCell) => number,
  extentOf: (cell: DomainCell) => number,
  headerExtent: number
): DomainAxis[] {
  const axis: DomainAxis[] = order.map((cluster, index) => ({
    cluster,
    index,
    size: sizes.get(cluster) ?? 0,
    label: `${prefix}${cluster}`,
    offset: 0,
    // A band is as big as its biggest cell — a grid column has one width.
    extent: Math.max(...cells.filter((cell) => bandOf(cell) === index).map(extentOf), 0),
  }));
  let cursor = headerExtent + GRID_GAP;
  for (const band of axis) {
    band.offset = cursor;
    cursor += band.extent + GRID_GAP;
  }
  return axis;
}

function cellWidth(memberCount: number): number {
  return Math.min(CELL_MAX_W, CELL_MIN_W + Math.round(Math.sqrt(memberCount) * 12));
}

function cellHeight(packageCount: number, visibleMembers: number, hasOverflow: boolean): number {
  const body = packageCount * PKG_HEAD_H + visibleMembers * ROW_H + (hasOverflow ? ROW_H : 0);
  return Math.max(CELL_MIN_H, CARD_HEAD_H + body + CARD_PAD);
}

/**
 * Flow that leaves a cell, aggregated per ordered cell pair. Drawing every
 * relation would redraw the hairball the grid exists to break up; one weighted
 * line per pair says how strongly two groups pull on each other, which is the
 * question a cell raises.
 */
function aggregateCellEdges(
  cells: DomainCell[],
  cellOfMember: Map<string, string>,
  graph: UIGraph | null
): DomainCellEdge[] {
  if (cells.length === 0) return [];
  const byId = new Map(cells.map((cell) => [cell.id, cell]));
  const weights = new Map<string, { from: DomainCell; to: DomainCell; weight: number }>();
  for (const relation of graph?.relations ?? []) {
    if (!relation.fromInternalId || !relation.toInternalId) continue;
    const fromId = cellOfMember.get(relation.fromInternalId);
    const toId = cellOfMember.get(relation.toInternalId);
    if (!fromId || !toId || fromId === toId) continue;
    const from = byId.get(fromId);
    const to = byId.get(toId);
    if (!from || !to) continue;
    const key = `${from.id}>${to.id}`;
    const entry = weights.get(key);
    if (entry) entry.weight++;
    else weights.set(key, { from, to, weight: 1 });
  }
  return [...weights.entries()]
    .map(([id, entry]) => ({
      id,
      from: entry.from.id,
      to: entry.to.id,
      weight: entry.weight,
      x1: entry.from.x + entry.from.width / 2,
      y1: entry.from.y + entry.from.height / 2,
      x2: entry.to.x + entry.to.width / 2,
      y2: entry.to.y + entry.to.height / 2,
    }))
    .sort((a, b) => b.weight - a.weight || a.id.localeCompare(b.id));
}

function summarizeDiff(cells: DomainCell[]): DomainDiffSummary {
  let changedMembers = 0;
  const touched = new Set<string>();
  const rows = new Set<number>();
  const cols = new Set<number>();
  for (const cell of cells) {
    if (cell.changedCount === 0) continue;
    changedMembers += cell.changedCount;
    touched.add(cell.id);
    rows.add(cell.structuralCluster);
    cols.add(cell.semanticCluster);
  }
  return {
    changedMembers,
    cells: touched.size,
    structuralClusters: rows.size,
    semanticClusters: cols.size,
  };
}

function buildHeader(
  raw: RawLatentDomains,
  index: GraphIndex,
  fanIn: Map<string, number>
): DomainHeader {
  const glueIds = new Set((raw.glue?.top_fan_in ?? []).map((node) => node.node));
  return {
    verdict: raw.agreement?.verdict ?? '',
    ami: raw.agreement?.ami ?? 0,
    nmi: raw.agreement?.nmi ?? 0,
    structuralK: raw.structural?.k ?? 0,
    semanticK: raw.semantic?.k ?? 0,
    structuralModularity: raw.structural?.modularity ?? 0,
    semanticModularity: raw.semantic?.modularity ?? 0,
    structuralDominantShare: raw.structural?.dominant_share ?? 0,
    semanticDominantShare: raw.semantic?.dominant_share ?? 0,
    nodeCount: raw.node_count ?? 0,
    droppedNodes: raw.dropped_nodes ?? 0,
    glue: (raw.glue?.top_fan_in ?? []).map((node) => resolveMember(node.node, index, fanIn, glueIds)),
    glueNote: raw.glue?.note ?? '',
  };
}

type GraphIndex = {
  internalsByComponent: Map<string, Map<string, Internal>>;
};

function buildGraphIndex(graph: UIGraph | null): GraphIndex {
  const internalsByComponent = new Map<string, Map<string, Internal>>();
  for (const component of graph?.components ?? []) {
    const internals = new Map<string, Internal>();
    for (const internal of component.internals) internals.set(internal.id, internal);
    internalsByComponent.set(component.id, internals);
  }
  return { internalsByComponent };
}

/**
 * Turn one archmotif node id into a card row. The id carries its kind as a
 * prefix (`type:`, `fn:`) and the rest is the uigraph `Internal.id`, so the
 * package is resolved by matching known components rather than by splitting on
 * a dot — package paths contain dots, and a split would be guessing.
 */
function resolveMember(
  id: string,
  index: GraphIndex,
  fanIn: Map<string, number>,
  glueIds: Set<string>
): DomainMember {
  const colon = id.indexOf(':');
  const lensKind = colon < 0 ? '' : id.slice(0, colon);
  const internalId = colon < 0 ? id : id.slice(colon + 1);
  const componentId = resolvePackage(index, internalId);
  const internal = componentId ? index.internalsByComponent.get(componentId)?.get(internalId) : undefined;
  return {
    id,
    internalId,
    componentId,
    name: internal?.name ?? symbolName(internalId, componentId),
    kind: internal?.kind ?? cardKind(lensKind),
    fanIn: fanIn.get(id) ?? 0,
    glue: glueIds.has(id),
    diff: internal?.diff,
    inGraph: internal != null,
  };
}

/** Longest component id that prefixes the symbol id. */
function resolvePackage(index: GraphIndex, internalId: string): string {
  let best = '';
  for (const componentId of index.internalsByComponent.keys()) {
    if (!internalId.startsWith(componentId + '.')) continue;
    if (componentId.length > best.length) best = componentId;
  }
  if (best !== '') return best;
  // Not in the graph: fall back to the package-path rule the ids are built on
  // — segments are '/'-separated and carry no dot, so the first dot after the
  // last slash starts the symbol.
  const slash = internalId.lastIndexOf('/');
  const dot = internalId.indexOf('.', slash + 1);
  return dot < 0 ? '' : internalId.slice(0, dot);
}

function symbolName(internalId: string, componentId: string): string {
  if (componentId && internalId.startsWith(componentId + '.')) {
    return internalId.slice(componentId.length + 1);
  }
  const dot = internalId.lastIndexOf('.');
  return dot < 0 ? internalId : internalId.slice(dot + 1);
}

/** archmotif node kinds → the card glyph vocabulary. */
function cardKind(lensKind: string): string {
  switch (lensKind) {
    case 'fn':
    case 'method':
      return 'func';
    case 'field':
      return 'prop';
    default:
      return 'type';
  }
}

/** Glue first (it is the finding), then package, then name. */
function compareMembers(a: DomainMember, b: DomainMember): number {
  if (a.glue !== b.glue) return a.glue ? -1 : 1;
  if (a.fanIn !== b.fanIn) return b.fanIn - a.fanIn;
  return a.componentId.localeCompare(b.componentId) || a.name.localeCompare(b.name);
}

function groupByPackage(members: DomainMember[]): DomainPackageBlock[] {
  const blocks: DomainPackageBlock[] = [];
  const byComponent = new Map<string, DomainPackageBlock>();
  for (const member of members) {
    const key = member.componentId || '(unresolved)';
    let block = byComponent.get(key);
    if (!block) {
      block = { componentId: member.componentId, name: packageName(key), members: [] };
      byComponent.set(key, block);
      blocks.push(block);
    }
    block.members.push(member);
  }
  return blocks;
}

function packageName(componentId: string): string {
  const slash = componentId.lastIndexOf('/');
  return slash < 0 ? componentId : componentId.slice(slash + 1);
}

function cellId(structuralCluster: number, semanticCluster: number): string {
  return `s${structuralCluster}-m${semanticCluster}`;
}
