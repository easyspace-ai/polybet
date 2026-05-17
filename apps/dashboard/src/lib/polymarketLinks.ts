/** Build Polymarket /event URL from slug (matches market page). */
export function polymarketEventUrl(slug: string | null | undefined): string | null {
  const s = slug?.trim().replace(/^event\//, "").replace(/\/+$/, "");
  if (!s) return null;
  return `https://polymarket.com/event/${s}`;
}

/** Prefer server-computed officialUrl; else derive from polySlug. */
export function resolvePolymarketEventUrl(
  officialUrl: string | null | undefined,
  polySlug: string | null | undefined,
): string | null {
  const direct = officialUrl?.trim();
  if (direct) return direct;
  return polymarketEventUrl(polySlug);
}
