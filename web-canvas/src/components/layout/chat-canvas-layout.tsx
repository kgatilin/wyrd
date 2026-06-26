"use client";

import { useState } from "react";
import {
  Panel,
  Group,
  Separator,
  usePanelRef,
} from "react-resizable-panels";
import { PanelLeftCloseIcon, PanelLeftOpenIcon, SidebarIcon } from "lucide-react";
import { AssistantRuntimeProvider } from "@assistant-ui/react";
import {
  useChatRuntime,
  AssistantChatTransport,
} from "@assistant-ui/react-ai-sdk";
import { lastAssistantMessageIsCompleteWithToolCalls } from "ai";

import { Thread } from "@/components/assistant-ui/thread";
import { CanvasPanel } from "./canvas-panel";
import { ArtifactsSidebar } from "./artifacts-sidebar";
import { Button } from "@/components/ui/button";
import { useSeedArtifacts } from "@/lib/artifact/seed";

export function ChatCanvasLayout() {
  useSeedArtifacts();
  const [isChatCollapsed, setIsChatCollapsed] = useState(false);
  const [isArtifactsSidebarOpen, setIsArtifactsSidebarOpen] = useState(true);
  const chatPanelRef = usePanelRef();

  const runtime = useChatRuntime({
    sendAutomaticallyWhen: lastAssistantMessageIsCompleteWithToolCalls,
    transport: new AssistantChatTransport({
      api: "/api/chat",
    }),
  });

  const toggleChat = () => {
    const panel = chatPanelRef.current;
    if (panel) {
      if (isChatCollapsed) {
        panel.expand();
      } else {
        panel.collapse();
      }
    }
  };

  const handleChatResize = (size: { asPercentage: number; inPixels: number }) => {
    // Collapsed when size is at or near 0
    setIsChatCollapsed(size.asPercentage < 1);
  };

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <div className="flex h-dvh flex-col">
        {/* Header bar with controls */}
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-border bg-background px-4">
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              onClick={toggleChat}
              title={isChatCollapsed ? "Expand chat" : "Collapse chat"}
              className="h-8 w-8"
            >
              {isChatCollapsed ? (
                <PanelLeftOpenIcon className="h-4 w-4" />
              ) : (
                <PanelLeftCloseIcon className="h-4 w-4" />
              )}
            </Button>
            <span className="text-sm font-medium">Archai Canvas</span>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setIsArtifactsSidebarOpen(!isArtifactsSidebarOpen)}
              title={isArtifactsSidebarOpen ? "Hide artifacts" : "Show artifacts"}
              className="h-8 w-8"
            >
              <SidebarIcon className="h-4 w-4" />
            </Button>
          </div>
        </header>

        {/* Main content area */}
        <div className="flex flex-1 overflow-hidden">
          <Group orientation="horizontal" className="flex-1">
            {/* Chat panel */}
            <Panel
              panelRef={chatPanelRef}
              defaultSize="35%"
              minSize="20%"
              collapsible
              collapsedSize="0%"
              onResize={handleChatResize}
              className="border-r border-border"
            >
              <div className="h-full overflow-hidden">
                <Thread />
              </div>
            </Panel>

            {/* Resize handle */}
            <Separator className="w-1 bg-border hover:bg-ring transition-colors data-[resize-handle-state=drag]:bg-ring" />

            {/* Canvas panel */}
            <Panel defaultSize="65%" minSize="30%">
              <CanvasPanel />
            </Panel>
          </Group>

          {/* Artifacts sidebar (outside Group for simpler toggle) */}
          {isArtifactsSidebarOpen && (
            <div className="w-56 shrink-0">
              <ArtifactsSidebar />
            </div>
          )}
        </div>
      </div>
    </AssistantRuntimeProvider>
  );
}
