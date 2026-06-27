"use client";

import { useSyncExternalStore } from 'react';

/**
 * The event-stream data-source: the RAW backend event log, untouched.
 *
 * It connects to a backend SSE endpoint (set by NEXT_PUBLIC_EVENTS_URL) that
 * emits each event verbatim, and exposes them to artifacts via {@link useEvents}.
 * There is no transformation or re-shaping — `type`, `data`, `subject` etc. are
 * exactly what the backend emitted, so a fold sees the real event schema.
 *
 * Generic: this canvas knows nothing about which backend produces the stream;
 * the URL is configuration, the same way the chat agent URL is.
 */
export interface AgentEvent {
  /** Monotonic sequence from the backend (SSE id). */
  seq: number;
  /** Raw event type, e.g. "agent.tool.started", "llm.chunk". */
  type: string;
  /** Full event subject, when present. */
  subject?: string;
  /** Emitting domain: "llm" | "agent" | "workflow" | "session" | … */
  source?: string;
  /** Originating session id, when present. */
  sessionId?: string;
  /** Event time in epoch ms (parsed from the backend timestamp). */
  ts: number;
  /** Raw event payload, exactly as emitted. */
  data?: unknown;
}

const DEFAULT_URL = 'http://localhost:8123/events/stream';
const MAX_EVENTS = 2000;

interface RawWire {
  type: string;
  subject?: string;
  source?: string;
  session_id?: string;
  timestamp?: string;
  data?: unknown;
}

class EventBus {
  private events: AgentEvent[] = [];
  private listeners = new Set<() => void>();
  private started = false;
  private es: EventSource | null = null;

  /** Open the SSE connection lazily on first subscriber (browser only). */
  private ensureStarted(): void {
    if (this.started || typeof window === 'undefined') return;
    this.started = true;
    const url = process.env.NEXT_PUBLIC_EVENTS_URL ?? DEFAULT_URL;
    try {
      this.es = new EventSource(url);
      this.es.onmessage = (e: MessageEvent) => {
        let raw: RawWire;
        try {
          raw = JSON.parse(e.data as string) as RawWire;
        } catch {
          return; // skip malformed line
        }
        this.append({
          seq: e.lastEventId ? Number(e.lastEventId) : this.events.length + 1,
          type: raw.type,
          subject: raw.subject,
          source: raw.source,
          sessionId: raw.session_id,
          ts: raw.timestamp ? Date.parse(raw.timestamp) : Date.now(),
          data: raw.data,
        });
      };
      // EventSource auto-reconnects; on reconnect it sends Last-Event-ID, so the
      // backend resumes after the last seq (no duplicates, no gaps).
    } catch {
      // EventSource unavailable — leave the stream empty.
    }
  }

  private append(ev: AgentEvent): void {
    const next = [...this.events, ev];
    this.events = next.length > MAX_EVENTS ? next.slice(next.length - MAX_EVENTS) : next;
    this.listeners.forEach((l) => l());
  }

  subscribe = (listener: () => void): (() => void) => {
    this.ensureStarted();
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): AgentEvent[] => this.events;
}

export const agentEvents = new EventBus();

const EMPTY: AgentEvent[] = [];

/**
 * Reactive view of the raw event stream. With `type`, returns events whose type
 * equals or starts with it (so `useEvents("agent.tool")` catches
 * `agent.tool.started`/`agent.tool.completed`).
 */
export function useEvents(type?: string): AgentEvent[] {
  const all = useSyncExternalStore(agentEvents.subscribe, agentEvents.getSnapshot, () => EMPTY);
  return type ? all.filter((e) => e.type === type || e.type.startsWith(type)) : all;
}
