import { createStore } from './store';
import type { AppStore } from './react';
import { initialState } from '../domain/state';
import { update } from '../domain/update';
import { createEffects } from '../effects';
import { createElkLayout } from '../adapters/elkLayout';
import { createHttpGraphSource } from '../adapters/httpGraphSource';
import { createDomViewport, type DomViewport } from '../adapters/domViewport';
import { createBrowserNavigation } from '../adapters/browserNavigation';

/**
 * App-level composition root. Builds the real elk + http + DOM-viewport adapters,
 * wires them into the store, and returns the store plus the viewport (App binds
 * the viewport to its canvas element on mount).
 */
export function createAppStore(): { store: AppStore; viewport: DomViewport } {
  const viewport = createDomViewport();
  const effects = createEffects({
    graphSource: createHttpGraphSource(),
    navigation: createBrowserNavigation(),
    layout: createElkLayout(),
    viewport,
  });
  const store = createStore(initialState, update, effects);
  return { store, viewport };
}
