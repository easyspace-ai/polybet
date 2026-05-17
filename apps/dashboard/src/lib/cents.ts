/** Floor cents to one decimal place (0.1¢), matching server FloorCents1 and UI step. */
export function floorCents1(c: number): number {
  if (!Number.isFinite(c) || c <= 0) return c;
  return Math.floor(c * 10 + 1e-9) / 10;
}

export function trailingStopCentsFromHW(highWaterCents: number, stopLossPct: number): number {
  return floorCents1(highWaterCents * (1 - stopLossPct / 100));
}
