const ET_ZONE = 'America/New_York';

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

/**
 * Kickoff time in US Eastern — matches Polymarket sports UI (e.g. 8:30 AM → 上午8:30).
 * Uses explicit Intl timeZone so display never falls back to browser local (e.g. UTC+8).
 */
export function formatKickoffET(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '—';
  const d = new Date(ms);
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: ET_ZONE,
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  }).formatToParts(d);

  const hour = parts.find((p) => p.type === 'hour')?.value ?? '';
  const minute = parts.find((p) => p.type === 'minute')?.value ?? '';
  const dayPeriod = parts.find((p) => p.type === 'dayPeriod')?.value ?? '';
  const zhPeriod = dayPeriod === 'AM' ? '上午' : dayPeriod === 'PM' ? '下午' : '';
  return `${zhPeriod}${hour}:${minute}`;
}

/** Eastern calendar date key YYYY-MM-DD for grouping match rows. */
export function kickoffDateKeyET(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '';
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: ET_ZONE,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(new Date(ms));
  const y = parts.find((p) => p.type === 'year')?.value ?? '';
  const m = parts.find((p) => p.type === 'month')?.value ?? '';
  const d = parts.find((p) => p.type === 'day')?.value ?? '';
  return `${y}-${m}-${d}`;
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
