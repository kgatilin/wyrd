"use client";

import type { AgentSubscriber } from "@ag-ui/client";
import { agentEvents, type AgentEvent } from "./events";

// Mirror live AG-UI agent activity into the event-stream data-source.
//
// The chat already receives the agent's AG-UI events (tool calls, results,
// errors). This subscriber taps the same stream and republishes the
// activity-relevant ones onto the shared EventBus so artifacts that fold
// `useEvents()` reflect what the agent actually did — no separate backend, and
// nothing archai-specific: it speaks only the AG-UI protocol vocabulary.

let seq = 0;

function publish(e: Omit<AgentEvent, "ts" | "id"> & { id?: string }): void {
  agentEvents.publish({
    id: e.id ?? `ev-${seq++}`,
    ts: Date.now(),
    type: e.type,
    summary: e.summary,
    data: e.data,
  });
}

function compact(value: unknown, max: number): string {
  let s: string;
  try {
    s = typeof value === "string" ? value : JSON.stringify(value);
  } catch {
    return "";
  }
  if (!s) return "";
  return s.length > max ? s.slice(0, max) + "…" : s;
}

/** Build an AG-UI subscriber that mirrors agent activity into the event bus. */
export function agentActivitySubscriber(): AgentSubscriber {
  return {
    onToolCallEndEvent({ event, toolCallName, toolCallArgs }) {
      const args =
        toolCallArgs && Object.keys(toolCallArgs).length ? ` · ${compact(toolCallArgs, 80)}` : "";
      publish({
        id: `tc-${event.toolCallId}`,
        type: "tool_call",
        summary: `${toolCallName}${args}`,
        data: toolCallArgs,
      });
    },
    onToolCallResultEvent({ event }) {
      publish({
        id: `tr-${event.messageId}`,
        type: "tool_result",
        summary: compact(event.content, 120),
        data: event.content,
      });
    },
    onRunErrorEvent({ event }) {
      const message = (event as { message?: string }).message ?? "run error";
      publish({ type: "log", summary: `error: ${message}`, data: event });
    },
  };
}
