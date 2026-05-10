import { existsSync } from "node:fs";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { app } from "electron";
import { is } from "@electron-toolkit/utils";
import {
  defaultPolybetProjectConfig,
  migrateRelativeFileDatabaseUrlToHomeEmbedded,
  parsePolybetProjectConfig,
  validatePolybetProjectConfigInput,
  type PolybetProjectBootstrap,
  type PolybetProjectConfig,
  type PolybetProjectConfigResult,
} from "../shared/polybet-project-config";
import { getBundledPolybetBinaryPath } from "./polybet-binary-path";
import { getPolybetEmbeddedServerDataDir } from "./polybet-embedded-dir";
import { getAppUserDataDir, getUserProfileHomeDir } from "./user-profile-paths";

const CONFIG_DIR = () => join(getUserProfileHomeDir(), ".polybet");
export const POLYBET_PROJECT_JSON = () =>
  join(CONFIG_DIR(), "polybet-project.json");

/** Legacy duplicate of JSON; removed on save / migrate so only JSON remains. */
const LEGACY_SERVER_ENV = () => join(CONFIG_DIR(), "server.env");

async function removeLegacyServerEnvIfPresent(): Promise<void> {
  const legacy = LEGACY_SERVER_ENV();
  if (!existsSync(legacy)) return;
  try {
    await rm(legacy, { force: true });
    console.log(
      `[polybet] removed legacy ${legacy} (single source: ${POLYBET_PROJECT_JSON()})`,
    );
  } catch (e) {
    console.warn("[polybet] could not remove legacy server.env:", e);
  }
}

async function migrateFromDotEnvFile(
  envPath: string,
  logLabel: string,
  rewriteRelativeDbToHomeEmbedded: boolean,
): Promise<PolybetProjectConfig | null> {
  if (!existsSync(envPath)) return null;
  try {
    const raw = await readFile(envPath, "utf-8");
    const kv = parseDotEnvLines(raw);
    let databaseUrl = kv.DATABASE_URL?.trim();
    if (!databaseUrl) return null;
    if (rewriteRelativeDbToHomeEmbedded) {
      databaseUrl = migrateRelativeFileDatabaseUrlToHomeEmbedded(
        databaseUrl,
        getPolybetEmbeddedServerDataDir(),
      );
    }
    const draft: Record<string, unknown> = {
      schemaVersion: 1,
      databaseUrl,
      host: kv.HOST?.trim() || undefined,
      port: kv.PORT?.trim() || undefined,
      outboundProxyUrl:
        kv.HTTP_PLATFORM_PROXY_URL?.trim() ||
        kv.HTTPS_PROXY?.trim() ||
        kv.ALL_PROXY?.trim() ||
        undefined,
      logLevel: kv.LOG_LEVEL?.trim() || undefined,
    };
    if (/^true$/i.test(kv.READ_ONLY_MODE ?? "")) {
      draft.readOnlyMode = true;
    }
    const validated = validatePolybetProjectConfigInput(draft);
    if (!validated.ok) return null;
    await mkdir(CONFIG_DIR(), { recursive: true });
    await mkdir(getPolybetEmbeddedServerDataDir(), { recursive: true });
    await writeFile(
      POLYBET_PROJECT_JSON(),
      `${JSON.stringify(validated.config, null, 2)}\n`,
      "utf-8",
    );
    console.log(
      `[polybet] migrated local server settings from ${logLabel} (${envPath}) → ${POLYBET_PROJECT_JSON()}`,
    );
    return validated.config;
  } catch {
    return null;
  }
}

let cachedBootstrap: PolybetProjectBootstrap | null = null;

let outboundProbeFailed = false;
let outboundProbeError: string | undefined;

export function resetOutboundProbeState(): void {
  outboundProbeFailed = false;
  outboundProbeError = undefined;
}

export function setOutboundProbeFailure(message: string): void {
  outboundProbeFailed = true;
  outboundProbeError = message;
}

export function getPolybetProjectBootstrap(): PolybetProjectBootstrap {
  if (!cachedBootstrap) {
    throw new Error("polybet bootstrap not initialized — call refreshPolybetProjectBootstrap first");
  }
  return cachedBootstrap;
}

export function refreshPolybetProjectBootstrap(
  project: PolybetProjectConfigResult,
): PolybetProjectBootstrap {
  const bundledBinaryPresent = existsSync(getBundledPolybetBinaryPath());
  const needsProjectSetup = bundledBinaryPresent && !project.ok;
  const needsOutboundVerification =
    bundledBinaryPresent &&
    project.ok &&
    project.config.gammaOutboundVerified !== true;
  const draftDefaults = needsProjectSetup
    ? draftDefaultsForRenderer()
    : undefined;
  cachedBootstrap = {
    project,
    bundledBinaryPresent,
    needsProjectSetup,
    needsOutboundVerification,
    outboundProbeFailed,
    outboundProbeError,
    draftDefaults,
  };
  return cachedBootstrap;
}

/** Main-process only: persist successful Polymarket Gamma outbound probe. */
export async function markGammaOutboundVerifiedOnDisk(options?: {
  /** When set (including empty string), replaces `outboundProxyUrl` before writing. */
  outboundProxyUrl?: string | null;
}): Promise<void> {
  const cur = await loadPolybetProjectConfigFromDisk();
  if (!cur.ok) return;
  const next: PolybetProjectConfig = {
    ...cur.config,
    gammaOutboundVerified: true,
  };
  if (options && "outboundProxyUrl" in options) {
    const p = options.outboundProxyUrl?.trim();
    next.outboundProxyUrl = p ? p : undefined;
  }
  await mkdir(CONFIG_DIR(), { recursive: true });
  await writeFile(
    POLYBET_PROJECT_JSON(),
    `${JSON.stringify(next, null, 2)}\n`,
    "utf-8",
  );
  await removeLegacyServerEnvIfPresent();
  resetOutboundProbeState();
  refreshPolybetProjectBootstrap({ ok: true, config: next });
}

function parseDotEnvLines(raw: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of raw.split(/\r?\n/)) {
    const t = line.trim();
    if (!t || t.startsWith("#")) continue;
    const eq = t.indexOf("=");
    if (eq <= 0) continue;
    const k = t.slice(0, eq).trim();
    let v = t.slice(eq + 1).trim();
    if (
      (v.startsWith('"') && v.endsWith('"')) ||
      (v.startsWith("'") && v.endsWith("'"))
    ) {
      v = v.slice(1, -1);
    }
    out[k] = v;
  }
  return out;
}

function devRepoRoot(): string {
  return join(app.getAppPath(), "..", "..");
}

/** Migrate legacy ~/.polybet/server.env into polybet-project.json when JSON is missing. */
async function tryMigrateFromHomeServerEnv(): Promise<PolybetProjectConfig | null> {
  const envPath = LEGACY_SERVER_ENV();
  const cfg = await migrateFromDotEnvFile(
    envPath,
    "~/.polybet/server.env (legacy)",
    true,
  );
  if (cfg && existsSync(envPath)) {
    try {
      await rm(envPath, { force: true });
      console.log(
        `[polybet] removed legacy ${envPath} after migration to ${POLYBET_PROJECT_JSON()}`,
      );
    } catch (e) {
      console.warn("[polybet] could not remove legacy server.env:", e);
    }
  }
  return cfg;
}

/** Dev: migrate repo-root `.env` into ~/.polybet/ (DB path → ~/.polybet/embedded). */
async function tryMigrateFromRepoRootDotEnv(): Promise<PolybetProjectConfig | null> {
  if (!is.dev) return null;
  const envPath = join(devRepoRoot(), ".env");
  return migrateFromDotEnvFile(envPath, "repo-root .env", true);
}

/** Packaged: migrate userData `.env` when JSON is absent (paths relative to userData). */
async function tryMigrateFromPackagedDotEnv(): Promise<PolybetProjectConfig | null> {
  if (is.dev) return null;
  const userData = getAppUserDataDir();
  const envPath = join(userData, ".env");
  return migrateFromDotEnvFile(envPath, "userData .env", false);
}

export async function loadPolybetProjectConfigFromDisk(): Promise<PolybetProjectConfigResult> {
  const path = POLYBET_PROJECT_JSON();
  try {
    const raw = await readFile(path, "utf-8");
    return { ok: true, config: parsePolybetProjectConfig(raw) };
  } catch (err) {
    if (
      err &&
      typeof err === "object" &&
      "code" in err &&
      (err as NodeJS.ErrnoException).code === "ENOENT"
    ) {
      const migrated =
        (await tryMigrateFromHomeServerEnv()) ??
        (await tryMigrateFromRepoRootDotEnv()) ??
        (await tryMigrateFromPackagedDotEnv());
      if (migrated) {
        return { ok: true, config: migrated };
      }
      return {
        ok: false,
        error: {
          message: `Missing ${path}. Save initial settings from the setup screen.`,
          details: ["ENOENT"],
        },
      };
    }
    const message = err instanceof Error ? err.message : String(err);
    return {
      ok: false,
      error: {
        message: `Invalid ${path}: ${message}`,
      },
    };
  }
}

export async function savePolybetProjectConfigJson(
  input: unknown,
): Promise<{ ok: true } | { ok: false; errors: string[] }> {
  const validated = validatePolybetProjectConfigInput(input);
  if (!validated.ok) {
    return { ok: false, errors: [validated.error.message] };
  }
  const cfg: PolybetProjectConfig = {
    ...validated.config,
    gammaOutboundVerified: false,
  };
  try {
    await mkdir(CONFIG_DIR(), { recursive: true });
    await mkdir(getPolybetEmbeddedServerDataDir(), { recursive: true });
    await writeFile(
      POLYBET_PROJECT_JSON(),
      `${JSON.stringify(cfg, null, 2)}\n`,
      "utf-8",
    );
    await removeLegacyServerEnvIfPresent();
    resetOutboundProbeState();
    refreshPolybetProjectBootstrap({ ok: true, config: cfg });
    return { ok: true };
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    return { ok: false, errors: [msg] };
  }
}

export function draftDefaultsForRenderer(): PolybetProjectConfig {
  if (is.dev) {
    return defaultPolybetProjectConfig("dev", {
      devEmbeddedDataDir: getPolybetEmbeddedServerDataDir(),
    });
  }
  return defaultPolybetProjectConfig("packaged");
}
