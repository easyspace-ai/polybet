import { cn } from '@/lib/utils';
import {
  DEFAULT_RECORD_FILTERS,
  hasActiveRecordFilters,
  type DateRangeFilter,
  type RecordFilterState,
} from '@/lib/recordFilters';

const DATE_OPTIONS: { id: DateRangeFilter; label: string }[] = [
  { id: 'all', label: '全部时间' },
  { id: 'today', label: '今天' },
  { id: 'yesterday', label: '昨天' },
  { id: '7d', label: '近 7 天' },
  { id: '30d', label: '近 30 天' },
];

const DATE_INPUT_CLASS =
  'h-8 w-[132px] px-2 text-[12px] rounded-md border border-border bg-surface focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand transition [color-scheme:light] dark:[color-scheme:dark]';

interface Props {
  filters: RecordFilterState;
  onChange: (next: RecordFilterState) => void;
  configuredTags: string[];
  marketSuggestions?: string[];
  className?: string;
}

export function RecordFiltersBar({
  filters,
  onChange,
  configuredTags,
  marketSuggestions = [],
  className,
}: Props) {
  const set = (patch: Partial<RecordFilterState>) => onChange({ ...filters, ...patch });

  const customActive =
    filters.dateRange === 'custom' || !!filters.customStartDate || !!filters.customEndDate;

  const selectPreset = (id: DateRangeFilter) => {
    set({
      dateRange: id,
      customStartDate: null,
      customEndDate: null,
    });
  };

  const onCustomStartChange = (value: string) => {
    const nextStart = value || null;
    let nextEnd = filters.customEndDate;
    if (nextStart && nextEnd && nextStart > nextEnd) {
      nextEnd = nextStart;
    }
    onChange({
      ...filters,
      dateRange: 'custom',
      customStartDate: nextStart,
      customEndDate: nextEnd,
    });
  };

  const onCustomEndChange = (value: string) => {
    const nextEnd = value || null;
    let nextStart = filters.customStartDate;
    if (nextStart && nextEnd && nextEnd < nextStart) {
      nextStart = nextEnd;
    }
    onChange({
      ...filters,
      dateRange: 'custom',
      customStartDate: nextStart,
      customEndDate: nextEnd,
    });
  };

  const todayStr = new Date().toISOString().slice(0, 10);

  return (
    <div className={cn('flex flex-col gap-3', className)}>
      {configuredTags.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-[10px] uppercase tracking-widest text-muted-foreground mr-1 shrink-0">
            类别
          </span>
          <button
            type="button"
            onClick={() => set({ league: null })}
            className={cn(
              'px-2.5 py-1 text-[11px] rounded-md border transition',
              filters.league == null
                ? 'bg-brand text-brand-foreground border-brand'
                : 'border-border text-muted-foreground hover:bg-accent',
            )}
          >
            全部
          </button>
          {configuredTags.map((tag) => {
            const active = filters.league?.toLowerCase() === tag.toLowerCase();
            return (
              <button
                key={tag}
                type="button"
                onClick={() => set({ league: tag.toLowerCase() })}
                className={cn(
                  'px-2.5 py-1 text-[11px] rounded-md border transition uppercase',
                  active
                    ? 'bg-brand text-brand-foreground border-brand'
                    : 'border-border text-muted-foreground hover:bg-accent',
                )}
              >
                {tag}
              </button>
            );
          })}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-x-1.5 gap-y-2">
        <span className="text-[10px] uppercase tracking-widest text-muted-foreground mr-1 shrink-0">
          时间
        </span>
        {DATE_OPTIONS.map((opt) => (
          <button
            key={opt.id}
            type="button"
            onClick={() => selectPreset(opt.id)}
            className={cn(
              'px-2.5 py-1 text-[11px] rounded-md border transition',
              filters.dateRange === opt.id && !customActive
                ? 'bg-brand text-brand-foreground border-brand'
                : 'border-border text-muted-foreground hover:bg-accent',
            )}
          >
            {opt.label}
          </button>
        ))}

        <span className="text-border mx-0.5 hidden sm:inline">|</span>

        <div
          className={cn(
            'flex flex-wrap items-center gap-2 rounded-md border px-2.5 py-1.5 transition',
            customActive ? 'border-brand/40 bg-brand/5' : 'border-border bg-surface/50',
          )}
        >
          <span className="text-[11px] text-muted-foreground shrink-0">自定义</span>
          <label className="flex items-center gap-1.5">
            <span className="text-[10px] text-muted-foreground">起</span>
            <input
              type="date"
              value={filters.customStartDate ?? ''}
              max={filters.customEndDate ?? todayStr}
              onChange={(e) => onCustomStartChange(e.target.value)}
              className={DATE_INPUT_CLASS}
            />
          </label>
          <span className="text-[11px] text-muted-foreground">—</span>
          <label className="flex items-center gap-1.5">
            <span className="text-[10px] text-muted-foreground">止</span>
            <input
              type="date"
              value={filters.customEndDate ?? ''}
              min={filters.customStartDate ?? undefined}
              max={todayStr}
              onChange={(e) => onCustomEndChange(e.target.value)}
              className={DATE_INPUT_CLASS}
            />
          </label>
          {customActive && (
            <button
              type="button"
              onClick={() =>
                set({
                  dateRange: 'all',
                  customStartDate: null,
                  customEndDate: null,
                })
              }
              className="text-[10px] text-muted-foreground hover:text-foreground transition"
            >
              清除区间
            </button>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[10px] uppercase tracking-widest text-muted-foreground shrink-0">
          市场
        </span>
        <input
          value={filters.marketQuery}
          onChange={(e) => set({ marketQuery: e.target.value })}
          list={marketSuggestions.length > 0 ? 'record-market-suggestions' : undefined}
          placeholder="搜索赛事 / 队名…"
          className="h-8 min-w-[200px] flex-1 max-w-md px-3 text-[12px] rounded-md border border-border bg-surface focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand transition"
        />
        {marketSuggestions.length > 0 && (
          <datalist id="record-market-suggestions">
            {marketSuggestions.map((s) => (
              <option key={s} value={s} />
            ))}
          </datalist>
        )}
        {hasActiveRecordFilters(filters) && (
          <button
            type="button"
            onClick={() => onChange(DEFAULT_RECORD_FILTERS)}
            className="text-[11px] text-muted-foreground hover:text-foreground transition"
          >
            清除筛选
          </button>
        )}
      </div>
    </div>
  );
}
