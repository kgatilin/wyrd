"use client";

import * as React from 'react';
import { GraphView, MarkdownView, MermaidView } from './host-scope';
import { useGraph } from '@/lib/data/graph';
import { useEvents } from '@/lib/data/events';
import { CAPABILITIES } from './capabilities';

/**
 * The runtime values for the capability manifest — what each capability `name`
 * actually resolves to in artifact code. Kept beside {@link CAPABILITIES}
 * (the metadata) and cross-checked so the declaration and the runtime scope
 * never drift.
 */
const CAPABILITY_VALUES: Record<string, unknown> = {
  Graph: GraphView,
  Markdown: MarkdownView,
  Mermaid: MermaidView,
  useGraph,
  useEvents,
};

// Dev guard: every declared capability must have a runtime value and vice versa.
if (process.env.NODE_ENV !== 'production') {
  const declared = new Set(CAPABILITIES.map((c) => c.name));
  const provided = new Set(Object.keys(CAPABILITY_VALUES));
  for (const name of declared) {
    if (!provided.has(name)) {
      console.error(`[capabilities] "${name}" is declared but has no runtime value`);
    }
  }
  for (const name of provided) {
    if (!declared.has(name)) {
      console.error(`[capabilities] "${name}" has a runtime value but is not declared`);
    }
  }
}

/** Build the host scope passed to artifact code (React + all capabilities). */
export function buildArtifactScope(): Record<string, unknown> {
  return { React, ...CAPABILITY_VALUES };
}
