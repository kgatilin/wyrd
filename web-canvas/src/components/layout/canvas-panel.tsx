"use client";

export function CanvasPanel() {
  return (
    <div className="flex h-full items-center justify-center bg-background">
      <div className="text-center">
        <p className="text-lg text-muted-foreground">
          Canvas — artifacts render here
        </p>
        <p className="mt-2 text-sm text-muted-foreground/60">
          (Milestone 2: D2 diagram rendering)
        </p>
      </div>
    </div>
  );
}
