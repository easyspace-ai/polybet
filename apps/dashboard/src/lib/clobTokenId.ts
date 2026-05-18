/**
 * CLOB token id helpers — mirrors server `polyexec.CLOBAssetIDForAPI` /
 * `normalizeTokenID` used for market WS subscribe and internal cache keys.
 */

/** Decimal uint256 string for Polymarket CLOB HTTP + market WebSocket `assets_ids`. */
export function clobAssetIdForAPI(hexOrDec: string): string {
  const s = hexOrDec.trim();
  if (!s) return "";
  if (s.startsWith("0x") || s.startsWith("0X")) {
    try {
      return BigInt(s).toString(10);
    } catch {
      return "";
    }
  }
  try {
    return BigInt(s).toString(10);
  } catch {
    return s;
  }
}

/** Internal cache key: 0x + 64 hex (matches server `normalizeTokenID`). */
export function normalizeTokenId(id: string | undefined | null): string {
  if (!id) return "";
  const raw = id.trim();
  if (!raw) return "";
  const lower = raw.toLowerCase();
  if (lower.startsWith("0x")) {
    let hex = lower.slice(2);
    if (!/^[0-9a-f]+$/.test(hex)) {
      return lower.length >= 66 ? lower.slice(0, 66) : "0x" + hex.padStart(64, "0");
    }
    hex = hex.padStart(64, "0");
    if (hex.length > 64) hex = hex.slice(-64);
    return "0x" + hex;
  }
  try {
    let hex = BigInt(raw).toString(16);
    hex = hex.padStart(64, "0");
    if (hex.length > 64) hex = hex.slice(-64);
    return "0x" + hex;
  } catch {
    const h = lower.replace(/^0x/, "");
    return "0x" + h.padStart(64, "0");
  }
}
