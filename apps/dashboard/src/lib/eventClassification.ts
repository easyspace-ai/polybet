/** Lowercase league / sport tags — same shape as Settings → 赛事分类 (`eventClassificationTags`). */

export const DEFAULT_EVENT_CLASSIFICATION_TAGS = ['nba', 'nhl'];

export function parseEventClassificationTags(raw: string): string[] {
  if (!raw.trim()) return [...DEFAULT_EVENT_CLASSIFICATION_TAGS];
  try {
    const p = JSON.parse(raw) as unknown;
    if (!Array.isArray(p)) return [...DEFAULT_EVENT_CLASSIFICATION_TAGS];
    return p
      .map((x) => String(x).trim().toLowerCase())
      .filter(Boolean);
  } catch {
    return [...DEFAULT_EVENT_CLASSIFICATION_TAGS];
  }
}

export function leagueMatchesEventTag(league: string, tagLower: string): boolean {
  return league.trim().toLowerCase() === tagLower.trim().toLowerCase();
}
