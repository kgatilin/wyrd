import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { GraphSourcePort, LayoutPort, ViewportPort } from '../domain/ports';
import { createLoadEffect } from './load';
import { createLayoutEffect } from './layout';
import { createViewportEffect } from './viewport';
import { createCommentsSeedEffect } from './comments';

export interface Ports {
  graphSource: GraphSourcePort;
  layout: LayoutPort;
  viewport: ViewportPort;
}

export function createEffects(ports: Ports): Effect<AppState, Event>[] {
  return [
    createLoadEffect(ports.graphSource),
    createLayoutEffect(ports.layout),
    createViewportEffect(ports.viewport),
    createCommentsSeedEffect(),
  ];
}
