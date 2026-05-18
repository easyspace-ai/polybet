/** Floor cents to one decimal place (0.1¢), matching server FloorCents1 and UI step. */
export function floorCents1(c: number): number {
  if (!Number.isFinite(c) || c <= 0) return c;
  return Math.floor(c * 10 + 1e-9) / 10;
}

/** Convert 0–1 Polymarket price to stable cents (×100), rounded to 0.1¢. */
export function centsFromPrice01(price01: number): number {
  if (!Number.isFinite(price01) || price01 <= 0) return 0;
  return Math.round(price01 * 1000) / 10;
}

export function trailingStopCentsFromHW(highWaterCents: number, stopLossPct: number): number {
  return floorCents1(highWaterCents * (1 - stopLossPct / 100));
}

/** Trailing stop is armed only after avg entry and band stop % are known. */
export function isTrailingStopActive(pos: {
  avgEntryCents: number;
  stopLossPct: number;
}): boolean {
  return pos.avgEntryCents > 0 && pos.stopLossPct > 0;
}

/**
 * Inverse of trailingStopCentsFromHW for UI back-solve when the user edits 触发.
 * Returns an integer percent in [1, 99] or null when inputs are invalid.
 */
export function stopLossPctFromHWAndTrigger(
  highWaterCents: number,
  triggerCents: number,
): number | null {
  const hw = floorCents1(highWaterCents);
  if (
    !Number.isFinite(hw) ||
    hw <= 0 ||
    !Number.isFinite(triggerCents) ||
    triggerCents <= 0 ||
    triggerCents >= hw
  ) {
    return null;
  }
  const rounded = Math.round((1 - triggerCents / hw) * 100);
  if (rounded < 1 || rounded > 99) return null;
  return rounded;
}

export type TrailingStopDraft = { sl: string; hw: string; trigger: string };
export type TrailingStopEditField = "hw" | "sl" | "trigger";

/**
 * Linked trailing-stop drafts — matches risksvc `TrailingStopCentsFromHW`:
 *   trigger = floorCents1(hw * (1 - sl/100))
 *
 * - Editing 高 or 损%: recompute 触发 from hw + sl (trigger follows).
 * - Editing 触发 (on blur / commit): back-solve 损% from fixed 高, then canonicalize
 *   触发 to the floored formula value. High-water is the PATCH anchor (server only
 *   accepts stopLossPct + highWaterCents).
 */
export function linkTrailingStopDraft(
  prev: TrailingStopDraft,
  field: TrailingStopEditField,
  value: string,
): TrailingStopDraft {
  const next = { ...prev, [field]: value };
  const hw = parseFloat(next.hw);
  const sl = parseFloat(next.sl);

  if (field === "hw" || field === "sl") {
    if (Number.isFinite(hw) && hw > 0 && Number.isFinite(sl) && sl >= 1 && sl <= 99) {
      next.trigger = String(trailingStopCentsFromHW(floorCents1(hw), sl));
    }
    return next;
  }

  const trig = parseFloat(next.trigger);
  const pct = stopLossPctFromHWAndTrigger(hw, trig);
  if (pct != null) {
    next.sl = String(pct);
    next.trigger = String(trailingStopCentsFromHW(floorCents1(hw), pct));
  }
  return next;
}
