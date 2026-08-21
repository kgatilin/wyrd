import { createStore } from './store';
import type { AppStore } from './react';
import { initialState } from '../domain/state';
import { update } from '../domain/update';
import { createEffects } from '../effects';
import { createElkLayout } from '../adapters/elkLayout';
import { createHttpGraphSource } from '../adapters/httpGraphSource';
import { createHttpSearchSource } from '../adapters/httpSearchSource';
import { createHttpLensSource } from '../adapters/httpLensSource';
import { createHttpDomainsSource } from '../adapters/httpDomainsSource';
import { createHttpEventModelSource } from '../adapters/httpEventModelSource';
import { createDomViewport, type DomViewport } from '../adapters/domViewport';
import { createBrowserNavigation } from '../adapters/browserNavigation';
import type { DomainsPort, EventModelPort, LensPort } from '../domain/ports';

/**
 * App-level composition root. Builds the real elk + http + DOM-viewport adapters,
 * wires them into the store, and returns the store plus the viewport (App binds
 * the viewport to its canvas element on mount) and the ports the two overlay
 * canvases read: the lens port for readiness and the domains port for the
 * partition itself, and the event-model port for the event canvas. All three
 * are views over the review, as the file diff is, rather than part of its state
 * machine.
 */
export function createAppStore(): {
  store: AppStore;
  viewport: DomViewport;
  lens: LensPort;
  domains: DomainsPort;
  events: EventModelPort;
} {
  const viewport = createDomViewport();
  const effects = createEffects({
    graphSource: createHttpGraphSource(),
    search: createHttpSearchSource(),
    navigation: createBrowserNavigation(),
    layout: createElkLayout(),
    viewport,
  });
  const store = createStore(initialState, update, effects);
  return {
    store,
    viewport,
    lens: createHttpLensSource(),
    domains: createHttpDomainsSource(),
    events: createHttpEventModelSource(),
  };
}
