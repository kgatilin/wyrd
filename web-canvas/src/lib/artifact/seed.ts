"use client";

import { useEffect } from 'react';
import { useArtifactStore } from './store';
import { welcomeDashboardFile } from './welcome-dashboard';

const WELCOME_ID = 'welcome-dashboard';

/**
 * Ensures the canvas always opens on something useful. If the user has no saved
 * dashboards (first run, or they deleted them all), seed the welcome dashboard
 * as a SAVED artifact so it persists and shows under "Saved" — not as a
 * freshly-generated file. Whatever is present, make sure one artifact is active.
 *
 * Saved artifacts persist to localStorage, so a returning user keeps their own
 * dashboards and this never overwrites them.
 */
export function useSeedArtifacts(): void {
  useEffect(() => {
    const store = useArtifactStore.getState();
    const hasSaved = store.artifacts.some((a) => a.kind === 'saved');

    if (!hasSaved && !store.artifacts.some((a) => a.id === WELCOME_ID)) {
      store.writeArtifact({ id: WELCOME_ID, name: 'Welcome', content: welcomeDashboardFile });
      store.saveArtifact(WELCOME_ID);
      store.setActive(WELCOME_ID);
      return;
    }

    if (store.activeId === null) {
      const first = store.artifacts.find((a) => a.kind === 'saved') ?? store.artifacts[0];
      if (first) store.setActive(first.id);
    }
  }, []);
}
