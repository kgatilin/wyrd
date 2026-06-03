import { describe, it, expect } from 'vitest';
import { createAppStore } from './createAppStore';

const flush = () => new Promise((r) => setTimeout(r));

describe('createAppStore', () => {
  it('boots, and on GraphRequested loads a graph and lays it out (fixture fallback under jsdom)', async () => {
    const store = createAppStore();
    expect(store.getState().graph).toBeNull();

    store.dispatch({ type: 'GraphRequested' });
    // loadEffect → GraphLoaded → layoutEffect → LayoutComputed (real elk under jsdom)
    await flush();
    await flush();
    for (let i = 0; i < 50 && store.getState().geometry.laid == null; i++) await flush();

    expect(store.getState().graph).not.toBeNull();
    expect(store.getState().load.status).toBe('ready');
    expect(store.getState().geometry.laid).not.toBeNull();
  });
});
