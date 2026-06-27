"use client";

import { useSyncExternalStore } from 'react';

/**
 * The event-stream data-source.
 *
 * A single stream of agent/backend events feeds a shared EventBus. Two consumers
 * read it: the chat UI (messages, tool-calls) and artifacts (which fold the same
 * events as a data-source — "what the agent did" is renderable). The live agent
 * websocket/SSE plugs into `publish`; for now a mock seeds a few events.
 */
export interface AgentEvent {
  id: string;
  ts: number;
  /** e.g. 'message' | 'tool_call' | 'tool_result' | 'log' | 'artifact' */
  type: string;
  /** Short human summary for compact rendering. */
  summary: string;
  /** Arbitrary structured payload (tool args/results, etc.). */
  data?: unknown;
}

class EventBus {
  // Starts empty: real agent activity is published from the live AG-UI stream
  // (see lib/data/agui-events.ts, wired in chat-canvas-layout). Artifacts that
  // fold the event stream show nothing until the agent actually does something.
  private events: AgentEvent[] = [];
  private listeners = new Set<() => void>();

  publish = (event: AgentEvent): void => {
    // New array reference so useSyncExternalStore detects the change.
    this.events = [...this.events, event];
    this.listeners.forEach((l) => l());
  };

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): AgentEvent[] => this.events;
}

export const agentEvents = new EventBus();

/** Reactive view of the event stream, optionally filtered by type. */
export function useEvents(type?: string): AgentEvent[] {
  const all = useSyncExternalStore(
    agentEvents.subscribe,
    agentEvents.getSnapshot,
    agentEvents.getSnapshot,
  );
  return type ? all.filter((e) => e.type === type) : all;
}
