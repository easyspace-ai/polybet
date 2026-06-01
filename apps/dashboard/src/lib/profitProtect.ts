import type { RiskPositionRow } from "@/lib/api";
import { floorCents1 } from "@/lib/cents";

export type ProfitProtectCentsConfig = { arm: number; stop: number };

export function globalProfitProtectEnabled(configValue: string | undefined): boolean {
  return (configValue ?? "true").trim().toLowerCase() !== "false";
}

/** Per-position effective enable: inherit global unless explicitly overridden. */
export function effectiveProfitProtectEnabled(
  pos: RiskPositionRow,
  globalEnabled: boolean,
): boolean {
  if (pos.profitProtectEnabledEffective != null) {
    return pos.profitProtectEnabledEffective;
  }
  if (pos.profitProtectUseEnableOverride === true) {
    return !!pos.profitProtectEnableOverride;
  }
  // Legacy: custom thresholds implied enabled (before per-position enable override).
  if (pos.profitProtectCustom) {
    return true;
  }
  return globalEnabled;
}

export function positionHasProfitProtectOverride(pos: RiskPositionRow): boolean {
  return (
    !!pos.profitProtectUseEnableOverride ||
    !!pos.profitProtectCustom ||
    (pos.profitProtectArmCentsOverride ?? 0) > 0 ||
    (pos.profitProtectStopCentsOverride ?? 0) > 0 ||
    (pos.profitProtectArmPctOverride ?? 0) > 0 ||
    (pos.profitProtectDrawdownOverride ?? 0) > 0
  );
}

export function resolveProfitProtectCents(pos: RiskPositionRow): ProfitProtectCentsConfig | null {
  if (!effectiveProfitProtectEnabled(pos, pos.profitProtectEnabled !== false)) return null;
  if (pos.profitProtectMode !== "cents") return null;
  const arm = pos.profitProtectArmCentsOverride ?? pos.profitProtectArmCents ?? 0;
  const stop = pos.profitProtectStopCentsOverride ?? pos.profitProtectStopCents ?? 0;
  if (arm <= 0 || stop <= 0 || stop >= arm) return null;
  return { arm: floorCents1(arm), stop: floorCents1(stop) };
}

/** Live armed state: persisted flag or mark already crossed activation threshold. */
export function profitProtectEffectiveArmed(pos: RiskPositionRow, markCents: number): boolean {
  if (!effectiveProfitProtectEnabled(pos, pos.profitProtectEnabled !== false)) {
    return false;
  }
  if (pos.profitProtectEffectiveArmed) return true;
  if (pos.profitProtectArmed) return true;
  const cfg = resolveProfitProtectCents(pos);
  if (!cfg || markCents <= 0) return false;
  return markCents >= cfg.arm;
}

export function profitProtectShouldTrigger(
  pos: RiskPositionRow,
  markCents: number,
): { arm: number; stop: number } | null {
  const cfg = resolveProfitProtectCents(pos);
  if (!cfg || markCents <= 0) return null;
  if (!profitProtectEffectiveArmed(pos, markCents)) return null;
  if (markCents > cfg.stop) return null;
  return cfg;
}
