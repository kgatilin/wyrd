export interface Store<S, E> {
  getState(): S;
  dispatch(event: E): void;
  subscribe(listener: () => void): () => void;
}

export type Reducer<S, E> = (state: S, event: E) => S;
export type Effect<S, E> = (event: E, getState: () => S, dispatch: (event: E) => void) => void;

export function createStore<S, E>(
  initial: S,
  update: Reducer<S, E>,
  effects: Effect<S, E>[] = []
): Store<S, E> {
  let state = initial;
  const listeners = new Set<() => void>();
  let depth = 0;

  const getState = (): S => state;
  const notify = () => listeners.forEach((l) => l());

  const dispatch = (event: E): void => {
    state = update(state, event);
    depth++;
    try {
      for (const fx of effects) fx(event, getState, dispatch);
    } finally {
      depth--;
    }
    if (depth === 0) notify();
  };

  const subscribe = (listener: () => void): (() => void) => {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  };

  return { getState, dispatch, subscribe };
}
