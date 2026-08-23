/**
 * The event model as the canvas reads it.
 *
 * The daemon answers in these units already — components, the flows between
 * them, and the kinds those flows carry — so nothing here re-derives the graph.
 * What this module owns is the reading: which flows belong to a selection, what
 * a component's three lists look like side by side, and how a kind is named
 * when the full name is too long to fit on an edge.
 */

/**
 * How one thing on the map differs from the base, in the review canvas's
 * vocabulary (`../types`), because a reader moving between the two surfaces
 * should not have to learn a second one.
 *
 * Present only when the request named a base. A model with no `base` carries no
 * marks, which is how "not compared" stays distinguishable from "unchanged".
 */
export type EventDiff = 'added' | 'removed' | 'changed';

/** The commit an event model was compared against. */
export interface EventModelBase {
  ref: string;
  rev?: string;
}

/** One component: a declaration, native or imported. */
export interface EventComponent {
  id: string;
  owns?: string;
  description?: string;
  /** "asyncapi" for an imported document; absent for a native declaration. */
  source?: string;
  source_file?: string;
  partition_key?: string[];
  has_state: boolean;
  /** The port family a single imported document stands for. */
  instances?: string[];
  inputs?: EventSlot[];
  outputs?: EventSlot[];
  state_events?: EventSlot[];
  diff?: EventDiff;
  /** The scalar facts this component changed from. Slots carry their own. */
  was?: EventComponentWas;
}

export interface EventComponentWas {
  owns?: string;
  description?: string;
  source_file?: string;
  partition_key?: string[];
  has_state?: boolean;
}

/** One entry of one of the three lists. */
export interface EventSlot {
  kind: string;
  pattern?: string;
  description?: string;
  delivery?: string;
  /** command | event | call | observe, when the source declared it. */
  role?: string;
  instances?: string[];
  diff?: EventDiff;
  /** The whole entry this one changed from, so any moved field can be shown. */
  was?: EventSlot;
}

/** One producer-to-observer edge. */
export interface EventFlow {
  from: string;
  to: string;
  kind: string;
  /** True when the kind drives the target; false when it is only folded. */
  trigger: boolean;
  health?: string;
  diff?: EventDiff;
}

/** One event kind, with everywhere it appears. */
export interface EventKind {
  name: string;
  pattern?: string;
  /** Every address the kind was declared at, when it travels more than one. */
  subjects?: string[];
  description?: string;
  partition_key?: string[];
  delivery?: string;
  health?: string;
  /** command | event, written out by the publisher. */
  class?: string;
  owner?: string;
  producers?: string[];
  triggers?: string[];
  folders?: string[];
  schema?: unknown;
  /** One instance of `schema`, built by the daemon with $refs followed. */
  example?: unknown;
  diff?: EventDiff;
  /** What this kind's identity on the wire was, before. */
  was?: EventKindWas;
}

export interface EventKindWas {
  pattern?: string;
  subjects?: string[];
  delivery?: string;
  health?: string;
  class?: string;
  owner?: string;
  /**
   * The payload contract moved. A flag rather than the old schema: it is the
   * largest field a kind carries, and a reader who wants the detail has the
   * file diff, where a schema change reads as a patch.
   */
  schema_changed?: boolean;
}

export interface EventModel {
  components: EventComponent[];
  flows: EventFlow[];
  kinds: EventKind[];
  /** Set when this model was read against a base, absent when it was not. */
  base?: EventModelBase;
}

/**
 * Health values that are worth drawing differently.
 *
 * `unwired` is the one that is about a pair rather than a kind: both ends are
 * declared and their addresses never match, so the append lands where nobody is
 * listening. The daemon draws that pair anyway and marks it, because the
 * alternative — no line — reads as a relation the model never had.
 */
export type EventHealth = 'ok' | 'orphan' | 'starved' | 'ambiguous' | 'unwired';

/**
 * Flows collapsed onto one edge per component pair. The daemon sends one flow
 * per kind, and a pair carrying six kinds would otherwise be six parallel
 * lines; the canvas draws one line and lists what travels it.
 *
 * A pair with any triggering kind is drawn as a trigger — that is the stronger
 * relation, and a solid line that also carries a folded kind is not a lie.
 */
export interface EventLink {
  id: string;
  from: string;
  to: string;
  kinds: EventFlow[];
  trigger: boolean;
  /** The worst health among the kinds on this link, for the accent. */
  health: EventHealth;
  /**
   * How the pair itself changed, which is not the same question as how its
   * kinds did. A line is `removed` only when everything that travelled it is
   * gone — one removed kind out of six leaves the pair standing, and drawing
   * it as removed would say two components stopped talking when they did not.
   */
  diff?: EventDiff;
}

// Ranked by how much a reader has to do about it. `unwired` is worst: it is a
// wire that was declared on both ends and is broken, where the others are
// counts of ends that are missing or duplicated.
const HEALTH_RANK: Record<string, number> = { ok: 0, starved: 1, orphan: 2, ambiguous: 3, unwired: 4 };

/**
 * The id of the link between two components. One function rather than a
 * template literal at each site: the canvas builds the same id when a reader
 * selects an edge, and two spellings that only have to agree by convention
 * agree right up until one of them is edited.
 */
export function linkId(from: string, to: string): string {
  return `${from} \u2192 ${to}`;
}

export function buildLinks(flows: EventFlow[]): EventLink[] {
  const byPair = new Map<string, EventLink>();
  for (const flow of flows) {
    const id = linkId(flow.from, flow.to);
    const existing = byPair.get(id);
    if (existing) {
      existing.kinds.push(flow);
      existing.trigger = existing.trigger || flow.trigger;
      existing.health = worseHealth(existing.health, flow.health);
      continue;
    }
    byPair.set(id, {
      id,
      from: flow.from,
      to: flow.to,
      kinds: [flow],
      trigger: flow.trigger,
      health: normalizeHealth(flow.health),
    });
  }
  for (const link of byPair.values()) {
    link.diff = linkDiff(link.kinds);
  }
  return [...byPair.values()].sort((a, b) => a.id.localeCompare(b.id));
}

/**
 * The mark for a pair, from the marks of everything travelling it.
 *
 * All-or-nothing at the ends: a pair is added only when every kind on it is new
 * and removed only when every kind on it is gone, because those are the two
 * claims about the *pair* — that these components started talking, or stopped.
 * Anything else that moved is `changed`, and a pair whose kinds all stayed put
 * carries no mark at all.
 */
function linkDiff(kinds: EventFlow[]): EventDiff | undefined {
  if (kinds.length === 0) return undefined;
  if (kinds.every((k) => k.diff === 'added')) return 'added';
  if (kinds.every((k) => k.diff === 'removed')) return 'removed';
  return kinds.some((k) => k.diff) ? 'changed' : undefined;
}

function normalizeHealth(health: string | undefined): EventHealth {
  if (health && health in HEALTH_RANK) return health as EventHealth;
  return 'ok';
}

function worseHealth(current: EventHealth, next: string | undefined): EventHealth {
  const candidate = normalizeHealth(next);
  return HEALTH_RANK[candidate] > HEALTH_RANK[current] ? candidate : current;
}

/**
 * Components with no flow in or out. They are drawn, because a declaration
 * reaching nobody is the finding a picture of an event model exists to make
 * visible, and a node dropped for having no edges hides exactly that.
 */
export function isolatedComponents(model: EventModel): Set<string> {
  const connected = new Set<string>();
  for (const flow of model.flows) {
    connected.add(flow.from);
    connected.add(flow.to);
  }
  return new Set(model.components.filter((c) => !connected.has(c.id)).map((c) => c.id));
}

/**
 * The last two segments of a kind name. Edge labels have room for a few
 * characters, and the leading segments repeat the component the edge starts at.
 */
export function shortKind(kind: string): string {
  const parts = kind.split('.');
  if (parts.length <= 2) return kind;
  return parts.slice(-2).join('.');
}

/** Everything one component declares, in the order the model states it. */
export interface ComponentSections {
  inputs: EventSlot[];
  outputs: EventSlot[];
  stateEvents: EventSlot[];
}

export function sectionsOf(component: EventComponent): ComponentSections {
  return {
    inputs: component.inputs ?? [],
    outputs: component.outputs ?? [],
    stateEvents: component.state_events ?? [],
  };
}

/** How many entries a component declares in total — the node's size cue. */
export function slotCount(component: EventComponent): number {
  const sections = sectionsOf(component);
  return sections.inputs.length + sections.outputs.length + sections.stateEvents.length;
}

/** The ways a kind can be unhealthy, in the order the header lists them. */
export const UNHEALTHY: EventHealth[] = ['unwired', 'orphan', 'starved', 'ambiguous'];

/**
 * How many kinds sit in each unhealthy state.
 *
 * Counted per state rather than rolled into one number: "orphan" (appended and
 * observed by nobody), "starved" (observed and appended by nobody) and
 * "ambiguous" (declared exclusive without exactly one input) are three
 * different findings, and a single total names none of them.
 */
export function healthCounts(kinds: EventKind[]): Record<EventHealth, number> {
  const counts: Record<EventHealth, number> = { ok: 0, orphan: 0, starved: 0, ambiguous: 0, unwired: 0 };
  for (const kind of kinds) {
    const health = kind.health;
    if (health && health in counts) counts[health as EventHealth] += 1;
  }
  return counts;
}

/** The marks the header counts, in the order it lists them. */
export const DIFFS: EventDiff[] = ['added', 'changed', 'removed'];

/**
 * How many components, kinds and flows carry each mark.
 *
 * Counted per unit rather than as one total: "three components changed" and
 * "three kinds changed" are different findings, and a reader who clicks a
 * number expects the list behind it to be one kind of thing.
 */
export function diffCounts(model: EventModel | null): Record<EventDiff, number> {
  const counts: Record<EventDiff, number> = { added: 0, removed: 0, changed: 0 };
  if (!model) return counts;
  for (const item of [...model.components, ...model.kinds]) {
    if (item.diff) counts[item.diff] += 1;
  }
  return counts;
}

/** The components sitting under one mark, in the order the model lists them. */
export function componentsInDiff(model: EventModel | null, diff: EventDiff): EventComponent[] {
  return (model?.components ?? []).filter((c) => c.diff === diff);
}

/** The kinds sitting under one mark, in the order the model lists them. */
export function kindsInDiff(model: EventModel | null, diff: EventDiff): EventKind[] {
  return (model?.kinds ?? []).filter((k) => k.diff === diff);
}

/**
 * The fields a changed thing actually moved, as (label, before, after).
 *
 * The daemon sends the whole previous value rather than guessing which field a
 * reader wants, so picking out what differs is this side's job — printing every
 * field of `was` would bury the one line that moved in five that did not.
 */
export function movedFields(
  now: object,
  was: object | undefined,
  fields: string[]
): { field: string; before: string; after: string }[] {
  if (!was) return [];
  // `object` rather than an index signature on the model types: the fields
  // asked for are a caller's list of names, and widening every declaration
  // interface to accept arbitrary keys — to read six of them — would cost the
  // typo-catching those interfaces exist for.
  const from = was as Record<string, unknown>;
  const to = now as Record<string, unknown>;
  const out: { field: string; before: string; after: string }[] = [];
  for (const field of fields) {
    const before = describe(from[field]);
    const after = describe(to[field]);
    if (before !== after) out.push({ field, before, after });
  }
  return out;
}

function describe(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (Array.isArray(value)) return value.join(', ');
  return String(value);
}

/** The kinds sitting in one unhealthy state, in the order the model lists them. */
export function kindsInState(model: EventModel | null, health: EventHealth): EventKind[] {
  return (model?.kinds ?? []).filter((kind) => kind.health === health);
}

/** Every component that appends a kind, is triggered by it, or folds it. */
export function participantsOf(kind: EventKind): string[] {
  return [...new Set([...(kind.producers ?? []), ...(kind.triggers ?? []), ...(kind.folders ?? [])])];
}

export function kindByName(model: EventModel): Map<string, EventKind> {
  return new Map(model.kinds.map((kind) => [kind.name, kind]));
}

export function componentById(model: EventModel): Map<string, EventComponent> {
  return new Map(model.components.map((component) => [component.id, component]));
}
