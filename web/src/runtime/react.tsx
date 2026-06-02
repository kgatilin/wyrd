import { createContext, useContext, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import type { Store } from './store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';

export type AppStore = Store<AppState, Event>;

const StoreContext = createContext<AppStore | null>(null);

export function StoreProvider({ store, children }: { store: AppStore; children: ReactNode }) {
  return <StoreContext.Provider value={store}>{children}</StoreContext.Provider>;
}

function useStoreInstance(): AppStore {
  const store = useContext(StoreContext);
  if (!store) throw new Error('useStore/useDispatch must be used within <StoreProvider>');
  return store;
}

/**
 * Subscribe to a slice of state. The selector MUST return a referentially-stable
 * value (a primitive, or the same Set/object reference when unchanged) — reducers
 * already preserve references for untouched slices. Object-constructing selectors
 * need memoization (add a withSelector helper later if that becomes common).
 */
export function useStore<T>(selector: (state: AppState) => T): T {
  const store = useStoreInstance();
  return useSyncExternalStore(store.subscribe, () => selector(store.getState()));
}

export function useDispatch(): (event: Event) => void {
  return useStoreInstance().dispatch;
}
