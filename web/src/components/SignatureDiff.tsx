export interface SignatureDiffProps {
  before?: string;
  after?: string;
}

export function SignatureDiff({ before, after }: SignatureDiffProps) {
  if (!before && !after) return null;
  return (
    <div className="hf-signature-diff">
      {before && <span className="before">{before}</span>}
      {before && after && <span className="arrow">-&gt;</span>}
      {after && <span className="after">{after}</span>}
    </div>
  );
}
