/** Normalize API timestamps to a UTC instant (server stores RFC3339 UTC). */
export function parseUtcInstant(iso: string): number | null {
  const raw = iso.trim();
  if (!raw) return null;
  let normalized = raw;
  if (!/[Zz]|[+-]\d{2}(:\d{2})?$/.test(raw)) {
    const base = raw.includes('T') ? raw.replace(' ', 'T') : raw.replace(' ', 'T');
    normalized = `${base}Z`;
  }
  const ms = Date.parse(normalized);
  return Number.isFinite(ms) ? ms : null;
}

function localTimePart(ms: number): string {
  return new Date(ms).toLocaleTimeString('en-US', {
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  });
}

/**
 * 开赛时间：按浏览器本地时区显示日期 + 12 小时制，与 polymarket.com 页面一致。
 * 例：8:30 PM ET 在中国显示为「5月21日 8:30 AM」。
 */
export function formatMatchupTime(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '—';

  const datePart = new Date(ms).toLocaleDateString('zh-CN', {
    month: 'long',
    day: 'numeric',
  });
  const time = localTimePart(ms);
  return `${datePart} ${time}`;
}

/** Short kickoff time only (e.g. 8:30 AM), local timezone. */
export function formatKickoffET(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '—';
  return localTimePart(ms);
}

/** Local calendar date key YYYY-MM-DD for grouping match rows. */
export function kickoffDateKeyET(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '';
  const d = new Date(ms);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

export function formatHistoryTimestamp(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '—';
  const d = new Date(ms);
  const y = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  const hm = d.toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
  return `${y}-${mo}-${day} ${hm}`;
}
