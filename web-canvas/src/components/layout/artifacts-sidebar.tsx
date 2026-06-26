"use client";

import { FileIcon, LayoutDashboardIcon } from "lucide-react";

interface ArtifactItem {
  id: string;
  name: string;
  type: "diagram" | "code" | "dashboard";
}

// Static placeholder data for milestone 1
const generatedArtifacts: ArtifactItem[] = [
  { id: "gen-1", name: "Architecture Overview", type: "diagram" },
  { id: "gen-2", name: "Package Dependencies", type: "diagram" },
];

const savedDashboards: ArtifactItem[] = [
  { id: "dash-1", name: "Service Map", type: "dashboard" },
  { id: "dash-2", name: "API Surface", type: "dashboard" },
];

export function ArtifactsSidebar() {
  return (
    <div className="flex h-full flex-col bg-muted/30 border-l border-border">
      <div className="flex-1 overflow-y-auto p-3">
        {/* Generated Section */}
        <div className="mb-6">
          <h3 className="mb-2 px-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            Generated
          </h3>
          <ul className="space-y-1">
            {generatedArtifacts.map((artifact) => (
              <li key={artifact.id}>
                <button
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-foreground/80 hover:bg-accent hover:text-foreground transition-colors"
                  disabled
                  title="Artifacts are not functional in this milestone"
                >
                  <FileIcon className="h-4 w-4 text-muted-foreground" />
                  <span className="truncate">{artifact.name}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>

        {/* Saved Section */}
        <div>
          <h3 className="mb-2 px-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            Saved
          </h3>
          <ul className="space-y-1">
            {savedDashboards.map((artifact) => (
              <li key={artifact.id}>
                <button
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-foreground/80 hover:bg-accent hover:text-foreground transition-colors"
                  disabled
                  title="Dashboards are not functional in this milestone"
                >
                  <LayoutDashboardIcon className="h-4 w-4 text-muted-foreground" />
                  <span className="truncate">{artifact.name}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      </div>

      <div className="border-t border-border p-3">
        <p className="text-center text-xs text-muted-foreground">
          Artifact VFS preview
        </p>
      </div>
    </div>
  );
}
