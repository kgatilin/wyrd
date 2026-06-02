import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { createStore } from './store';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import { update } from '../domain/update';
import { createEffects } from '../effects';
import type { GraphSourcePort, LayoutPort, ViewportPort } from '../domain/ports';

const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'bc1', name: 'Core' }],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [{ id: 'a.i', kind: 'class', name: 'Ai', members: [] }], ports: [] }],
  edges: [],
  comments: [],
};
const flush = () => new Promise((r) => setTimeout(r));

// Fake layout: echoes whether each component was expanded into its x coordinate,
// so the test can prove the effect re-ran with the new interaction state.
const fakeLayout: LayoutPort = {
  compute: (g, interaction) =>
    Promise.resolve({ ...g, components: g.components.map((c) => ({ ...c, x: interaction.expanded.has(c.id) ? 100 : 0 })) }),
};
const fakeGraphSource: GraphSourcePort = { load: () => Promise.resolve(graph) };
const fakeViewport: ViewportPort = { scrollToComponent: () => {}, fitZoom: () => null };

describe('integration: load → layout → toggle', () => {
  it('loads a graph, lays it out, and re-lays out on expand', async () => {
    const effects = createEffects({ graphSource: fakeGraphSource, layout: fakeLayout, viewport: fakeViewport });
    const store = createStore<AppState, Event>(initialState, update, effects);

    store.dispatch({ type: 'GraphRequested' });
    await flush(); // loadEffect → GraphLoaded → layoutEffect
    await flush(); // layout promise resolves → LayoutComputed

    expect(store.getState().graph).toBe(graph);
    const laid1 = store.getState().geometry.laid!;
    // 'a' is the first component → seeded expanded by initialExpanded
    expect(laid1.components.find((c) => c.id === 'a')!.x).toBe(100);

    // Collapse it and confirm the layout effect re-ran with the new state.
    store.dispatch({ type: 'ComponentToggled', id: 'a' });
    await flush();
    const laid2 = store.getState().geometry.laid!;
    expect(laid2.components.find((c) => c.id === 'a')!.x).toBe(0);
  });
});
