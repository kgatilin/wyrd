import type { Diff, Internal, InternalKind, Member } from '../types';

/**
 * The structure rendered inside an expanded package card.
 *
 * The card mirrors what `wyrd diagram generate` emits as D2: a package is a
 * set of source-file containers, each holding class shapes, each class shape a
 * header plus a two-column body. Grouping by file is what makes a package
 * readable — it is the unit the code is actually written and reviewed in —
 * and it is the one thing the flat symbol grid had no way to express.
 *
 * This module derives that structure only. Geometry is added by the layout
 * pass, so the shape stays a pure function of the graph and can be tested
 * without ELK.
 */

/** A block that aggregates same-kind leaf symbols declared in one file. */
export type AggregateKind = 'consts' | 'vars' | 'errors';

export type BlockKind = InternalKind | AggregateKind;

/** One row of a class body. */
export interface CardRow {
  id: string;
  kind: Member['kind'] | 'type' | 'symbol';
  name: string;
  /** Formatted parameter list, methods only. */
  params?: string;
  /** Right-hand column. */
  type?: string;
  exported?: boolean;
  diff?: Diff;
  /** Internal this row belongs to — an aggregate block mixes several. */
  internalId: string;
  /** Set when the row came from a real member (comment / focus target). */
  memberId?: string;
}

/** A class shape: one symbol, or one aggregate of leaf symbols. */
export interface CardBlock {
  id: string;
  kind: BlockKind;
  name: string;
  /** Detected DDD stereotype, shown as a chip. Absent for aggregates. */
  stereotype?: string;
  exported?: boolean;
  diff?: Diff;
  rows: CardRow[];
  /** Real internals covered; a single symbol block covers exactly one. */
  internalIds: string[];
  /** The symbol's own id, absent for aggregate blocks. */
  internalId?: string;
  x?: number;
  y?: number;
  w?: number;
  h?: number;
}

/** A source-file container. */
export interface CardFile {
  id: string;
  /** File name as shown in the container header, e.g. "options.go". */
  label: string;
  /**
   * Source path as the graph recorded it — a bare name for most symbols, a
   * path for some. Absent for the unknown-file bucket, which names no file.
   * Resolve it against the package id with `sourceFilePath` before handing it
   * to the daemon.
   */
  path?: string;
  diff?: Diff;
  blocks: CardBlock[];
  x?: number;
  y?: number;
  w?: number;
  h?: number;
}

/** Label used when a symbol carries no source file (e.g. combined models). */
export const UNKNOWN_SOURCE_FILE = '(unknown)';

/**
 * Relation kinds the diagram draws, matching what the D2 writer emits:
 * intra-package dependencies plus implementations.
 *
 * `calls` is deliberately absent. It is by far the largest kind the projection
 * produces — more than half of every relation in a real repository — and it
 * says "this body invokes that function", which is a level of detail the
 * structural diagram is not about. The symbol wiring overlay still shows call
 * edges; that view is where they are the point.
 */
const DIAGRAM_RELATION_KINDS: ReadonlySet<string> = new Set(['uses', 'returns', 'implements', 'extends']);

/** Whether a relation belongs on the diagram rather than in the wiring overlay. */
export function isDiagramRelation(relation: { kind: string }): boolean {
  return DIAGRAM_RELATION_KINDS.has(relation.kind);
}

/**
 * Order symbols take inside a file container, mirroring the D2 writer:
 * interfaces, structs, functions, type definitions, then the aggregated
 * constants / variables / errors blocks.
 */
const KIND_ORDER: BlockKind[] = ['iface', 'class', 'func', 'type', 'consts', 'vars', 'errors'];

const AGGREGATED: Record<string, { kind: AggregateKind; title: string }> = {
  const: { kind: 'consts', title: 'Constants' },
  var: { kind: 'vars', title: 'Variables' },
  error: { kind: 'errors', title: 'Errors' },
};

/**
 * Human label for a row's left column: a method shows its parameter list, and
 * everything else shows the bare name.
 */
export function rowLabel(row: CardRow): string {
  if (row.kind === 'method') return `${row.name}(${row.params ?? ''})`;
  return row.name;
}

/**
 * Full one-line text of a row, used for tooltips and width estimation.
 * Unnamed parameters carry only a type, so the label may be empty.
 */
export function rowText(row: CardRow): string {
  const label = rowLabel(row);
  if (!row.type) return label;
  return label ? `${label} ${row.type}` : row.type;
}

/**
 * Collapse a set of child diffs into the diff of their container. A container
 * whose children agree takes their state; mixed or partial change reads as
 * "changed", the same rule the component card already uses for its own header.
 */
function aggregateDiff(diffs: (Diff | undefined)[]): Diff | undefined {
  const present = diffs.filter((d): d is Diff => Boolean(d));
  if (present.length === 0) return undefined;
  if (present.length === diffs.length && present.every((d) => d === present[0])) return present[0];
  return 'changed';
}

/** Effective diff of a symbol: its own flag, or "changed" when a member moved. */
function internalDiff(internal: Internal): Diff | undefined {
  if (internal.diff) return internal.diff;
  for (const m of internal.members ?? []) {
    if (m.diff) return 'changed';
  }
  return undefined;
}

function memberRow(internal: Internal, member: Member): CardRow {
  return {
    id: member.id,
    kind: member.kind,
    name: member.name,
    params: member.params,
    type: member.type,
    exported: member.exported,
    diff: member.diff,
    internalId: internal.id,
    memberId: member.id,
  };
}

/**
 * Order rows take inside a class body, mirroring the D2 writer: a type
 * definition leads with its underlying type, a struct lists fields before
 * methods, a function lists parameters before its return.
 *
 * The projection sorts members by id so diff-synthesized rows land
 * deterministically, which on a struct interleaves fields and methods
 * alphabetically. Ranking by kind restores the reading order; a stable sort
 * keeps the projection's order within each kind.
 */
const ROW_KIND_ORDER: CardRow['kind'][] = ['type', 'param', 'prop', 'const', 'method', 'return'];

function rowRank(kind: CardRow['kind']): number {
  const index = ROW_KIND_ORDER.indexOf(kind);
  return index < 0 ? ROW_KIND_ORDER.length : index;
}

/** Rows of a single-symbol block. */
function symbolRows(internal: Internal): CardRow[] {
  const rows: CardRow[] = [];
  // A type definition leads with its underlying type, then its constants —
  // the same body the D2 writer emits for an enum.
  if (internal.kind === 'type' && internal.type) {
    rows.push({
      id: `${internal.id}#type`,
      kind: 'type',
      name: 'type',
      type: internal.type,
      exported: internal.exported,
      internalId: internal.id,
    });
  }
  for (const member of internal.members ?? []) {
    rows.push(memberRow(internal, member));
  }
  return rows.sort((a, b) => rowRank(a.kind) - rowRank(b.kind));
}

function symbolBlock(internal: Internal): CardBlock {
  return {
    id: internal.id,
    kind: internal.kind,
    name: internal.name,
    stereotype: internal.stereotype,
    exported: internal.exported,
    diff: internalDiff(internal),
    rows: symbolRows(internal),
    internalIds: [internal.id],
    internalId: internal.id,
  };
}

/**
 * Fold every constant / variable / error of one file into a single block, the
 * way the D2 writer does. Twenty package constants are one box with twenty
 * rows, not twenty boxes competing with the types around them.
 */
function aggregateBlock(fileId: string, kind: AggregateKind, title: string, internals: Internal[]): CardBlock {
  const rows: CardRow[] = internals.map((internal) => ({
    id: internal.id,
    kind: 'symbol',
    name: internal.name,
    type: internal.type,
    exported: internal.exported,
    diff: internalDiff(internal),
    internalId: internal.id,
  }));
  return {
    id: `${fileId}#${kind}`,
    kind,
    name: title,
    exported: internals.some((internal) => internal.exported),
    diff: aggregateDiff(rows.map((row) => row.diff)),
    rows,
    internalIds: internals.map((internal) => internal.id),
  };
}

function fileLabel(sourceFile?: string): string {
  const trimmed = (sourceFile ?? '').trim();
  if (!trimmed) return UNKNOWN_SOURCE_FILE;
  return trimmed.split('/').pop() || trimmed;
}

/**
 * Build the file → block → row structure of a package card.
 *
 * Files are ordered by name so a card's shape stays stable across reloads and
 * across scope switches; the unknown-file bucket sorts last.
 */
export function buildCardModel(internals: Internal[]): CardFile[] {
  const byFile = new Map<string, Internal[]>();
  for (const internal of internals ?? []) {
    const label = fileLabel(internal.sourceFile);
    const bucket = byFile.get(label);
    if (bucket) bucket.push(internal);
    else byFile.set(label, [internal]);
  }

  const labels = [...byFile.keys()].sort((a, b) => {
    if (a === UNKNOWN_SOURCE_FILE) return 1;
    if (b === UNKNOWN_SOURCE_FILE) return -1;
    return a.localeCompare(b);
  });

  return labels.map((label) => {
    const internals = byFile.get(label) ?? [];
    const blocks: CardBlock[] = [];

    for (const internal of internals) {
      if (!AGGREGATED[internal.kind]) blocks.push(symbolBlock(internal));
    }
    for (const [kind, spec] of Object.entries(AGGREGATED)) {
      const leaves = internals.filter((internal) => internal.kind === kind);
      if (leaves.length > 0) blocks.push(aggregateBlock(label, spec.kind, spec.title, leaves));
    }

    blocks.sort((a, b) => KIND_ORDER.indexOf(a.kind) - KIND_ORDER.indexOf(b.kind));

    return {
      id: label,
      label,
      // Files group by base name, and a package is one directory, so every
      // internal in the bucket recorded the same path.
      path: internals.find((internal) => internal.sourceFile?.trim())?.sourceFile,
      diff: aggregateDiff(blocks.map((block) => block.diff)),
      blocks,
    };
  });
}
