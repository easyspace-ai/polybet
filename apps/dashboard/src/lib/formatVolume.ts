/** Dollar amount only, Polymarket-style ($2.56M). */
export function formatVolumeAmount(usd: number | null | undefined): string | null {
  if (usd == null || !Number.isFinite(usd) || usd <= 0) return null;
  if (usd >= 1_000_000) {
    const m = usd / 1_000_000;
    const s = m >= 10 ? m.toFixed(1) : m.toFixed(2);
    return `$${s}M`;
  }
  if (usd >= 1_000) {
    return `$${(usd / 1_000).toFixed(1)}K`;
  }
  return `$${Math.round(usd)}`;
}

/** @deprecated Prefer formatVolumeAmount + label 交易量 */
export function formatEventVolume(usd: number | null | undefined): string | null {
  const amt = formatVolumeAmount(usd);
  return amt ? `${amt} 交易量` : null;
}
