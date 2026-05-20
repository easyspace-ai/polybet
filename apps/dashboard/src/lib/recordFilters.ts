export type DateRangeFilter = 'all' | 'today' | 'yesterday' | '7d' | '30d' | 'custom';

export interface RecordFilterState {
  league: string | null;
  dateRange: DateRangeFilter;
  /** YYYY-MM-DD，与 customEndDate 联用 */
  customStartDate: string | null;
  customEndDate: string | null;
  marketQuery: string;
}

export const DEFAULT_RECORD_FILTERS: RecordFilterState = {
  league: null,
  dateRange: 'all',
  customStartDate: null,
  customEndDate: null,
  marketQuery: '',
};

function startOfLocalDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function parseDateInput(value: string | null | undefined): Date | null {
  const v = (value ?? '').trim();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(v)) return null;
  const [y, m, d] = v.split('-').map(Number);
  const dt = new Date(y, m - 1, d);
  if (dt.getFullYear() !== y || dt.getMonth() !== m - 1 || dt.getDate() !== d) return null;
  return dt;
}

export function dateRangeBounds(range: DateRangeFilter): { from: number; to: number } | null {
  if (range === 'all' || range === 'custom') return null;
  const now = new Date();
  const todayStart = startOfLocalDay(now).getTime();
  const dayMs = 24 * 60 * 60 * 1000;
  switch (range) {
    case 'today':
      return { from: todayStart, to: todayStart + dayMs };
    case 'yesterday':
      return { from: todayStart - dayMs, to: todayStart };
    case '7d':
      return { from: todayStart - 7 * dayMs, to: todayStart + dayMs };
    case '30d':
      return { from: todayStart - 30 * dayMs, to: todayStart + dayMs };
    default:
      return null;
  }
}

function customDateBounds(filters: RecordFilterState): { from: number; to: number } | null {
  const start = parseDateInput(filters.customStartDate);
  const end = parseDateInput(filters.customEndDate);
  if (!start && !end) return null;

  const dayMs = 24 * 60 * 60 * 1000;
  let from = start ? startOfLocalDay(start).getTime() : 0;
  let to = end ? startOfLocalDay(end).getTime() + dayMs : startOfLocalDay(new Date()).getTime() + dayMs;

  if (start && end && from > to) {
    [from, to] = [to - dayMs, from + dayMs];
  }
  return { from, to };
}

/** @deprecated Use recordTimestampInRange */
export function isoInDateRange(iso: string, range: DateRangeFilter): boolean {
  if (range === 'custom') return true;
  const bounds = dateRangeBounds(range);
  if (!bounds) return true;
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return false;
  return t >= bounds.from && t < bounds.to;
}

export function recordTimestampInRange(iso: string, filters: RecordFilterState): boolean {
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return false;

  if (filters.dateRange === 'custom' || filters.customStartDate || filters.customEndDate) {
    const bounds = customDateBounds({ ...filters, dateRange: 'custom' });
    if (!bounds) return true;
    return t >= bounds.from && t < bounds.to;
  }

  const bounds = dateRangeBounds(filters.dateRange);
  if (!bounds) return true;
  return t >= bounds.from && t < bounds.to;
}

export function hasActiveRecordFilters(filters: RecordFilterState): boolean {
  return (
    !!filters.league ||
    filters.dateRange !== 'all' ||
    !!filters.customStartDate ||
    !!filters.customEndDate ||
    !!filters.marketQuery.trim()
  );
}

export function matchesLeagueTag(
  league: string | null | undefined,
  sport: string | null | undefined,
  tag: string | null,
): boolean {
  if (!tag) return true;
  const needle = tag.toLowerCase();
  const lg = (league ?? '').toLowerCase();
  const sp = (sport ?? '').toLowerCase();
  return lg === needle || lg.includes(needle) || sp === needle || sp.includes(needle);
}

export function matchesMarketQuery(title: string | null | undefined, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return (title ?? '').toLowerCase().includes(q);
}

export function sortByUpdatedDesc<T extends { updatedAt?: string; createdAt?: string }>(rows: T[]): T[] {
  return [...rows].sort((a, b) => {
    const ta = new Date(a.updatedAt ?? a.createdAt ?? 0).getTime();
    const tb = new Date(b.updatedAt ?? b.createdAt ?? 0).getTime();
    return tb - ta;
  });
}
