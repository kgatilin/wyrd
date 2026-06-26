"use client";

import { FileIcon, LayoutDashboardIcon, BookmarkIcon, Trash2Icon } from "lucide-react";
import { useArtifactStore, type Artifact } from "@/lib/artifact/store";

export function ArtifactsSidebar() {
  const artifacts = useArtifactStore((s) => s.artifacts);
  const activeId = useArtifactStore((s) => s.activeId);

  const generated = artifacts.filter((a) => a.kind === "generated");
  const saved = artifacts.filter((a) => a.kind === "saved");

  return (
    <div className="flex h-full flex-col bg-muted/30 border-l border-border">
      <div className="flex-1 overflow-y-auto p-3">
        <Section
          title="Generated"
          empty="Nothing generated yet"
          items={generated}
          activeId={activeId}
          icon="file"
        />
        <Section
          title="Saved"
          empty="No saved dashboards"
          items={saved}
          activeId={activeId}
          icon="dashboard"
        />
      </div>

      <div className="border-t border-border p-3">
        <p className="text-center text-xs text-muted-foreground">Artifact VFS</p>
      </div>
    </div>
  );
}

function Section({
  title,
  empty,
  items,
  activeId,
  icon,
}: {
  title: string;
  empty: string;
  items: Artifact[];
  activeId: string | null;
  icon: "file" | "dashboard";
}) {
  return (
    <div className="mb-6">
      <h3 className="mb-2 px-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        {title}
      </h3>
      {items.length === 0 ? (
        <p className="px-2 py-1 text-xs text-muted-foreground/70">{empty}</p>
      ) : (
        <ul className="space-y-1">
          {items.map((artifact) => (
            <ArtifactRow
              key={artifact.id}
              artifact={artifact}
              active={artifact.id === activeId}
              icon={icon}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function ArtifactRow({
  artifact,
  active,
  icon,
}: {
  artifact: Artifact;
  active: boolean;
  icon: "file" | "dashboard";
}) {
  const setActive = useArtifactStore((s) => s.setActive);
  const saveArtifact = useArtifactStore((s) => s.saveArtifact);
  const deleteArtifact = useArtifactStore((s) => s.deleteArtifact);
  const Icon = icon === "dashboard" ? LayoutDashboardIcon : FileIcon;

  return (
    <li className="group relative">
      <button
        onClick={() => setActive(artifact.id)}
        className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors ${
          active
            ? "bg-accent text-foreground"
            : "text-foreground/80 hover:bg-accent/60 hover:text-foreground"
        }`}
        title={artifact.name}
      >
        <Icon className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="truncate">{artifact.name}</span>
      </button>

      <div className="absolute right-1 top-1/2 -translate-y-1/2 flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
        {artifact.kind === "generated" && (
          <button
            onClick={() => saveArtifact(artifact.id)}
            title="Save as dashboard"
            className="rounded p-1 text-muted-foreground hover:bg-background hover:text-foreground"
          >
            <BookmarkIcon className="h-3.5 w-3.5" />
          </button>
        )}
        <button
          onClick={() => deleteArtifact(artifact.id)}
          title="Delete"
          className="rounded p-1 text-muted-foreground hover:bg-background hover:text-destructive"
        >
          <Trash2Icon className="h-3.5 w-3.5" />
        </button>
      </div>
    </li>
  );
}
