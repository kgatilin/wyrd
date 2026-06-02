import { describe, it, expect, vi } from 'vitest';
import { createStore } from './store';

type S = { count: number };
type E = { type: 'inc' } | { type: 'set'; value: number };

const update = (s: S, e: E): S => {
  switch (e.type) {
    case 'inc': return { count: s.count + 1 };
    case 'set': return { count: e.value };
    default: return s;
  }
};

describe('createStore', () => {
  it('applies update and exposes new state via getState', () => {
    const store = createStore<S, E>({ count: 0 }, update, []);
    store.dispatch({ type: 'inc' });
    expect(store.getState()).toEqual({ count: 1 });
  });

  it('notifies subscribers on dispatch and stops after unsubscribe', () => {
    const store = createStore<S, E>({ count: 0 }, update, []);
    const listener = vi.fn();
    const unsub = store.subscribe(listener);
    store.dispatch({ type: 'inc' });
    expect(listener).toHaveBeenCalledTimes(1);
    unsub();
    store.dispatch({ type: 'inc' });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('notifies subscribers once per external dispatch even when an effect re-dispatches synchronously', () => {
    const reDispatch = (e: E, _getState: () => S, dispatch: (e: E) => void) => {
      if (e.type === 'inc') dispatch({ type: 'set', value: 42 });
    };
    const store = createStore<S, E>({ count: 0 }, update, [reDispatch]);
    const listener = vi.fn();
    store.subscribe(listener);
    store.dispatch({ type: 'inc' });
    expect(listener).toHaveBeenCalledTimes(1);
    expect(store.getState()).toEqual({ count: 42 });
  });

  it('runs effects after update, giving them getState and dispatch', () => {
    const seen: Array<{ type: string; count: number }> = [];
    const effect = (e: E, getState: () => S, dispatch: (e: E) => void) => {
      seen.push({ type: e.type, count: getState().count });
      if (e.type === 'inc' && getState().count === 1) dispatch({ type: 'set', value: 99 });
    };
    const store = createStore<S, E>({ count: 0 }, update, [effect]);
    store.dispatch({ type: 'inc' });
    expect(seen[0]).toEqual({ type: 'inc', count: 1 });
    expect(store.getState()).toEqual({ count: 99 });
  });
});
