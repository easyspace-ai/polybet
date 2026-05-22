/** Polymarket sports cards use Eastern calendar date for grouping (server startTime UTC → ET). */
const POLY_GAME_DATE_TZ = 'America/New_York';
const BEIJING_TZ = 'Asia/Shanghai';

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

function etDateKeyFromMs(ms: number): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: POLY_GAME_DATE_TZ,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(new Date(ms));
  const y = parts.find((p) => p.type === 'year')?.value ?? '';
  const m = parts.find((p) => p.type === 'month')?.value ?? '';
  const d = parts.find((p) => p.type === 'day')?.value ?? '';
  return `${y}-${m}-${d}`;
}

function etTimePart(ms: number): string {
  return new Date(ms).toLocaleTimeString('en-US', {
    timeZone: POLY_GAME_DATE_TZ,
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  });
}

/**
 * 开赛时间：Eastern 日历日期 + Eastern 12 小时制，与 polymarket.com 体育页一致。
 */
export function formatMatchupTime(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '—';

  const datePart = new Date(ms).toLocaleDateString('zh-CN', {
    timeZone: POLY_GAME_DATE_TZ,
    month: 'long',
    day: 'numeric',
  });
  return `${datePart} ${etTimePart(ms)}`;
}

/** Beijing wall time for a second row under kickoff (Asia/Shanghai). */
export function formatBeijingTime(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '—';
  return new Date(ms).toLocaleString('zh-CN', {
    timeZone: BEIJING_TZ,
    month: 'long',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

/** Short kickoff time only (e.g. 8:30 PM ET). */
export function formatKickoffET(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '—';
  return etTimePart(ms);
}

/** Eastern calendar date key YYYY-MM-DD for grouping match rows. */
export function kickoffDateKeyET(iso: string): string {
  const ms = parseUtcInstant(iso);
  if (ms == null) return '';
  return etDateKeyFromMs(ms);
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
