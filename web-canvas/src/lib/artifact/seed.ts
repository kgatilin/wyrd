"use client";

import { useEffect } from 'react';
import { useArtifactStore } from './store';
import { welcomeDashboardFile } from './welcome-dashboard';

const WELCOME_ID = 'welcome-dashboard';

/**
 * Version of the seeded welcome dashboard. Bump whenever
 * {@link welcomeDashboardFile} changes so returning users — whose old copy is
 * persisted in localStorage — pick up the new version. Only the system-seeded
 * "Welcome" artifact is refreshed; the user's own saved dashboards are untouched.
 */
const WELCOME_VERSION = 2;
const WELCOME_VERSION_KEY = 'archai-canvas-welcome-version';

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
      writeWelcomeVersion();
      return;
    }

    // Refresh an existing seeded welcome dashboard when its version is stale, so
    // a returning user gets the latest seed instead of a buggy cached copy.
    // writeArtifact activates the target, so restore the user's prior focus.
    const welcome = store.artifacts.find((a) => a.id === WELCOME_ID);
    if (welcome && readWelcomeVersion() < WELCOME_VERSION) {
      const prevActive = store.activeId;
      store.writeArtifact({ id: WELCOME_ID, name: 'Welcome', content: welcomeDashboardFile });
      store.saveArtifact(WELCOME_ID);
      writeWelcomeVersion();
      if (prevActive && prevActive !== WELCOME_ID) store.setActive(prevActive);
    }

    if (store.activeId === null) {
      const first = store.artifacts.find((a) => a.kind === 'saved') ?? store.artifacts[0];
      if (first) store.setActive(first.id);
    }
  }, []);
}

function readWelcomeVersion(): number {
  if (typeof window === 'undefined') return WELCOME_VERSION;
  return Number(window.localStorage.getItem(WELCOME_VERSION_KEY)) || 0;
}

function writeWelcomeVersion(): void {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(WELCOME_VERSION_KEY, String(WELCOME_VERSION));
}
