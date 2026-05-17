import { readFileSync } from "node:fs";
import { join } from "node:path";
import { getUserProfileHomeDir } from "./user-profile-paths";

export interface SidecarWatchdogSettings {
  intervalSec: number;
  failThreshold: number;
  httpTimeoutMs: number;
  maxRetries: number;
  killGraceMs: number;
}

const DEFAULTS: SidecarWatchdogSettings = {
  intervalSec: 30,
  failThreshold: 2,
  httpTimeoutMs: 5000,
  maxRetries: 0,
  killGraceMs: 5000,
};

function parseIntConfig(raw: unknown, def: number, min: number, max: number): number {
  const n = typeof raw === "string" ? parseInt(raw, 10) : typeof raw === "number" ? raw : NaN;
  if (!Number.isFinite(n)) return def;
  return Math.min(max, Math.max(min, n));
}

/** Reads optional keys from ~/.polybet/bot-settings.json (same file as Go wsconfig). */
export function loadSidecarWatchdogSettings(): SidecarWatchdogSettings {
  try {
    const path = join(getUserProfileHomeDir(), ".polybet", "bot-settings.json");
    const data = JSON.parse(readFileSync(path, "utf8")) as Record<string, string>;
    return {
      intervalSec: parseIntConfig(data.desktopSidecarWatchdogSec, DEFAULTS.intervalSec, 5, 300),
      failThreshold: parseIntConfig(data.desktopSidecarWatchdogFailThreshold, DEFAULTS.failThreshold, 1, 10),
      httpTimeoutMs:
        parseIntConfig(data.desktopSidecarWatchdogHttpTimeoutSec, DEFAULTS.httpTimeoutMs / 1000, 1, 30) * 1000,
      maxRetries: parseIntConfig(data.desktopSidecarMaxRetries, DEFAULTS.maxRetries, 0, 999),
      killGraceMs: parseIntConfig(data.desktopSidecarKillGraceSec, DEFAULTS.killGraceMs / 1000, 1, 60) * 1000,
    };
  } catch {
    return { ...DEFAULTS };
  }
}
