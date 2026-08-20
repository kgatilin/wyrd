import { createStore } from './store';
import type { AppStore } from './react';
import { initialState } from '../domain/state';
import { update } from '../domain/update';
import { createEffects } from '../effects';
import { createElkLayout } from '../adapters/elkLayout';
import { createHttpGraphSource } from '../adapters/httpGraphSource';
import { createHttpSearchSource } from '../adapters/httpSearchSource';
import { createHttpLensSource } from '../adapters/httpLensSource';
import { createDomViewport, type DomViewport } from '../adapters/domViewport';
import { createBrowserNavigation } from '../adapters/browserNavigation';
import type { LensPort } from '../domain/ports';

/**
 * App-level composition root. Builds the real elk + http + DOM-viewport adapters,
 * wires them into the store, and returns the store plus the viewport (App binds
 * the viewport to its canvas element on mount) and the lens port (the domains
 * canvas calls the daemon's analysis tools directly, as the file diff calls
 * its endpoint — a view over the review, not part of its state machine).
 */
export function createAppStore(): { store: AppStore; viewport: DomViewport; lens: LensPort } {
  const viewport = createDomViewport();
  const effects = createEffects({
    graphSource: createHttpGraphSource(),
    search: createHttpSearchSource(),
    navigation: createBrowserNavigation(),
    layout: createElkLayout(),
    viewport,
  });
  const store = createStore(initialState, update, effects);
  return { store, viewport, lens: createHttpLensSource() };
}
