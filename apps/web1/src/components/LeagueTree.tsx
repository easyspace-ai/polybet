import { cn } from '../lib/utils';

export interface LeagueNavTag {
  /** Lowercase tag (matches `eventClassificationTags` / `group.league` case-insensitively) */
  tag: string;
  /** Shown in sidebar (e.g. NBA) */
  label: string;
  count: number;
}

interface LeagueTreeProps {
  tags: LeagueNavTag[];
  selectedTag: string;
  onSelectTag: (tag: string) => void;
}

/**
 * Market league sidebar: flat list of configured classification tags with counts.
 */
export function LeagueTree({ tags, selectedTag, onSelectTag }: LeagueTreeProps) {
  return (
    <div className="p-2">
      <p className="px-2 py-2 font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-mut">
        联赛
      </p>
      {tags.map(({ tag, label, count }) => (
        <button
          key={tag}
          onClick={() => onSelectTag(tag)}
          className={cn(
            'w-full flex items-center justify-between px-2 py-1.5 mt-0.5 text-[13px] transition-colors',
            selectedTag === tag
              ? 'bg-tm-bg-el text-tm-tx border-l-2 border-tm-accent pl-[6px]'
              : 'text-tm-tx-dim hover:text-tm-tx hover:bg-tm-bg-el/60',
          )}
        >
          <span className="truncate text-left font-medium">{label}</span>
          <span className="font-mono text-[10px] text-tm-tx-mut ml-2 shrink-0">{count}</span>
        </button>
      ))}
    </div>
  );
}
