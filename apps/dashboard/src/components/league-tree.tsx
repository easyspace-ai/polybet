export interface LeagueNavTag {
  tag: string;
  label: string;
  count: number;
}

interface LeagueTreeProps {
  tags: LeagueNavTag[];
  selectedTag: string;
  onSelectTag: (tag: string) => void;
}

export function LeagueTree({ tags, selectedTag, onSelectTag }: LeagueTreeProps) {
  return (
    <div className="px-2 py-2">
      <div className="font-mono text-[10px] uppercase tracking-widest text-muted-foreground px-3 py-2">联赛</div>
      {tags.map((t) => {
        const active = t.tag === selectedTag;
        return (
          <button
            key={t.tag}
            onClick={() => onSelectTag(t.tag)}
            className={[
              "w-full text-left group flex items-center justify-between rounded px-3 py-2 text-sm transition-colors",
              active
                ? "bg-surface-hover text-foreground border-l-2 border-primary -ml-px"
                : "text-muted-foreground hover:bg-surface/60 hover:text-foreground",
            ].join(" ")}
          >
            <span className="font-medium truncate">{t.label}</span>
            <span className="font-mono text-[10px] text-muted-foreground shrink-0">{t.count}</span>
          </button>
        );
      })}
    </div>
  );
}
