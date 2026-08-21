/**
 * The event model as the canvas reads it.
 *
 * The daemon answers in these units already — components, the flows between
 * them, and the kinds those flows carry — so nothing here re-derives the graph.
 * What this module owns is the reading: which flows belong to a selection, what
 * a component's three lists look like side by side, and how a kind is named
 * when the full name is too long to fit on an edge.
 */

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
}

/** One producer-to-observer edge. */
export interface EventFlow {
  from: string;
  to: string;
  kind: string;
  /** True when the kind drives the target; false when it is only folded. */
  trigger: boolean;
  health?: string;
}

/** One event kind, with everywhere it appears. */
export interface EventKind {
  name: string;
  pattern?: string;
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
}

export interface EventModel {
  components: EventComponent[];
  flows: EventFlow[];
  kinds: EventKind[];
}

/** Health values that are worth drawing differently. */
export type EventHealth = 'ok' | 'orphan' | 'starved' | 'ambiguous';

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
}

const HEALTH_RANK: Record<string, number> = { ok: 0, starved: 1, orphan: 2, ambiguous: 3 };

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
  return [...byPair.values()].sort((a, b) => a.id.localeCompare(b.id));
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

/** The three ways a kind can be unhealthy, in the order the header lists them. */
export const UNHEALTHY: EventHealth[] = ['orphan', 'starved', 'ambiguous'];

/**
 * How many kinds sit in each unhealthy state.
 *
 * Counted per state rather than rolled into one number: "orphan" (appended and
 * observed by nobody), "starved" (observed and appended by nobody) and
 * "ambiguous" (declared exclusive without exactly one input) are three
 * different findings, and a single total names none of them.
 */
export function healthCounts(kinds: EventKind[]): Record<EventHealth, number> {
  const counts: Record<EventHealth, number> = { ok: 0, orphan: 0, starved: 0, ambiguous: 0 };
  for (const kind of kinds) {
    const health = kind.health;
    if (health && health in counts) counts[health as EventHealth] += 1;
  }
  return counts;
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
