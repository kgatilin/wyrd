import { createStore } from './store';
import type { AppStore } from './react';
import { initialState } from '../domain/state';
import { update } from '../domain/update';
import { createEffects } from '../effects';
import { createElkLayout } from '../adapters/elkLayout';
import { createHttpGraphSource } from '../adapters/httpGraphSource';
import type { ViewportPort } from '../domain/ports';

/**
 * App-level composition root (Plan 2a). Wires the real elk + http adapters into
 * the store. The viewport is a no-op for now: App keeps pan/zoom/scroll imperative
 * and local; Plan 2b replaces this with a real `domViewport` adapter.
 */
const noopViewport: ViewportPort = {
  scrollToComponent: () => {},
  fitZoom: () => null,
};

export function createAppStore(): AppStore {
  const effects = createEffects({
    graphSource: createHttpGraphSource(),
    layout: createElkLayout(),
    viewport: noopViewport,
  });
  return createStore(initialState, update, effects);
}
