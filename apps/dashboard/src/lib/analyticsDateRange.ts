const STORAGE_KEY = "polybet-analytics-date-range";

export interface AnalyticsDateRange {
  from: string;
  to: string;
}

function todayET(): string {
  return new Date().toLocaleDateString("en-CA", { timeZone: "America/New_York" });
}

function daysAgoET(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return d.toLocaleDateString("en-CA", { timeZone: "America/New_York" });
}

function isValidDateKey(s: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(s.trim());
}

export function defaultAnalyticsDateRange(): AnalyticsDateRange {
  return { from: daysAgoET(30), to: todayET() };
}

function normalizeRange(raw: Partial<AnalyticsDateRange>): AnalyticsDateRange {
  const fallback = defaultAnalyticsDateRange();
  const from = typeof raw.from === "string" && isValidDateKey(raw.from) ? raw.from.trim() : fallback.from;
  const to = typeof raw.to === "string" && isValidDateKey(raw.to) ? raw.to.trim() : fallback.to;
  if (from > to) {
    return { from: to, to: from };
  }
  return { from, to };
}

export function loadAnalyticsDateRange(): AnalyticsDateRange {
  if (typeof window === "undefined" || typeof localStorage === "undefined") {
    return defaultAnalyticsDateRange();
  }
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (!stored) return defaultAnalyticsDateRange();
    return normalizeRange(JSON.parse(stored) as Partial<AnalyticsDateRange>);
  } catch {
    return defaultAnalyticsDateRange();
  }
}

export function saveAnalyticsDateRange(range: AnalyticsDateRange): void {
  if (typeof window === "undefined" || typeof localStorage === "undefined") return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(normalizeRange(range)));
  } catch {
    /* ignore quota / private mode */
  }
}
