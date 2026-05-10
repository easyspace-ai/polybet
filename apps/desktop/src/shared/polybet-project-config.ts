/**
 * Polybet embedded-server settings stored under the user home directory as JSON
 * (~/.polybet/polybet-project.json). Not stored in SQLite — one file for the
 * whole local stack (DB URL, listen addr, outbound proxy, etc.).
 */

import { join } from "node:path";
import { pathToFileURL } from "node:url";

export interface PolybetProjectConfig {
  schemaVersion: 1;
  /** SQLite or other URL understood by cmd/server (required). */
  databaseUrl: string;
  /** HTTP bind host for the Go server (default 127.0.0.1). */
  host: string;
  /** TCP port for the Go server (default 7633). */
  port: string;
  /** Optional; forwarded to Go as HTTP_PLATFORM_PROXY_URL (Gamma / CLOB / WS). */
  outboundProxyUrl?: string;
  /** Set by the desktop shell after Gamma HTTPS probe succeeds (never user-supplied on save). */
  gammaOutboundVerified?: boolean;
  readOnlyMode?: boolean;
  logLevel?: string;
}

export interface PolybetProjectConfigError {
  message: string;
  /** Field-level or structural validation messages. */
  details?: string[];
}

export type PolybetProjectConfigResult =
  | { ok: true; config: PolybetProjectConfig }
  | { ok: false; error: PolybetProjectConfigError };

/** Snapshot exposed to the renderer at preload time (sync IPC). */
export interface PolybetProjectBootstrap {
  project: PolybetProjectConfigResult;
  bundledBinaryPresent: boolean;
  /** Bundled Go server exists but ~/.polybet/polybet-project.json is missing or invalid. */
  needsProjectSetup: boolean;
  /** Pre-filled form defaults when `needsProjectSetup` is true. */
  draftDefaults?: PolybetProjectConfig;
  /** Config file OK but Polymarket Gamma probe not recorded yet (or after proxy change). */
  needsOutboundVerification: boolean;
  /** Last boot: Gamma probe failed after Go was healthy — fix proxy and retry. */
  outboundProbeFailed: boolean;
  outboundProbeError?: string;
}

export const DEFAULT_HOST = "127.0.0.1";
export const DEFAULT_PORT = "7633";
export const DEFAULT_LOG_LEVEL = "info";

/** Defaults for packaged app (cwd = Electron userData when spawning). */
export const DEFAULT_PACKAGED_DATABASE_URL =
  "file:./router.db?_pragma=foreign_keys(1)";

/** Absolute `file:` URL for SQLite under a directory (e.g. ~/.polybet/embedded). */
export function sqliteDatabaseUrlUnderDir(
  absDir: string,
  fileName = "router.db",
): string {
  const filePath = join(absDir, fileName);
  return `${pathToFileURL(filePath).href}?_pragma=foreign_keys(1)`;
}

/**
 * When migrating from a repo-root `.env`, rewrite relative `file:` URLs so the
 * DB lives under ~/.polybet/embedded instead of the monorepo tree.
 */
export function migrateRelativeFileDatabaseUrlToHomeEmbedded(
  databaseUrl: string,
  homeEmbeddedDir: string,
): string {
  const trimmed = databaseUrl.trim();
  const qIndex = trimmed.indexOf("?");
  const basePart = qIndex >= 0 ? trimmed.slice(0, qIndex) : trimmed;
  if (!basePart.toLowerCase().startsWith("file:")) {
    return databaseUrl;
  }
  const withoutScheme = basePart.slice("file:".length);
  let fileName: string | null = null;
  if (withoutScheme.startsWith("./")) {
    const rel = withoutScheme.slice(2);
    fileName = rel.split(/[/\\]/).pop() || "router.db";
  } else if (!/[/\\]/.test(withoutScheme)) {
    fileName = withoutScheme || "router.db";
  }
  if (!fileName) {
    return databaseUrl;
  }
  return sqliteDatabaseUrlUnderDir(homeEmbeddedDir, fileName);
}

export function defaultPolybetProjectConfig(
  mode: "dev" | "packaged",
  options?: { devEmbeddedDataDir?: string },
): PolybetProjectConfig {
  let databaseUrl: string;
  if (mode === "dev") {
    const dir = options?.devEmbeddedDataDir?.trim();
    if (!dir) {
      throw new Error("defaultPolybetProjectConfig(dev) requires devEmbeddedDataDir");
    }
    databaseUrl = sqliteDatabaseUrlUnderDir(dir);
  } else {
    databaseUrl = DEFAULT_PACKAGED_DATABASE_URL;
  }
  return {
    schemaVersion: 1,
    databaseUrl,
    host: DEFAULT_HOST,
    port: DEFAULT_PORT,
    readOnlyMode: false,
    logLevel: DEFAULT_LOG_LEVEL,
  };
}

export function parsePolybetProjectConfig(raw: string): PolybetProjectConfig {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (e) {
    throw new Error(
      `Invalid polybet-project JSON: ${e instanceof Error ? e.message : "parse failed"}`,
    );
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("polybet-project.json: expected a JSON object");
  }
  const obj = parsed as Record<string, unknown>;
  if (obj.schemaVersion !== 1) {
    throw new Error("polybet-project.json: unsupported schemaVersion (expected 1)");
  }

  const databaseUrl = requiredNonEmptyString(obj.databaseUrl, "databaseUrl");
  const host = optionalWithDefault(
    obj.host,
    "host",
    DEFAULT_HOST,
    (s) => s.length > 0,
  );
  const portRaw =
    typeof obj.port === "number" && Number.isInteger(obj.port)
      ? String(obj.port)
      : obj.port;
  const port = optionalWithDefault(
    portRaw,
    "port",
    DEFAULT_PORT,
    isValidPortString,
  );
  const outboundProxyUrl = optionalUrl(obj.outboundProxyUrl, "outboundProxyUrl");
  const readOnlyMode = optionalBoolean(obj.readOnlyMode, "readOnlyMode");
  const logLevel = optionalWithDefault(
    obj.logLevel,
    "logLevel",
    DEFAULT_LOG_LEVEL,
    (s) => s.length > 0,
  );
  const gammaOutboundVerified = optionalBoolean(
    obj.gammaOutboundVerified,
    "gammaOutboundVerified",
  );

  validateDatabaseUrl(databaseUrl);

  return {
    schemaVersion: 1,
    databaseUrl,
    host,
    port,
    outboundProxyUrl,
    gammaOutboundVerified,
    readOnlyMode,
    logLevel,
  };
}

export function validatePolybetProjectConfigInput(
  input: unknown,
): PolybetProjectConfigResult {
  try {
    const raw =
      typeof input === "string" ? input : JSON.stringify(input ?? {});
    return { ok: true, config: parsePolybetProjectConfig(raw) };
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return { ok: false, error: { message } };
  }
}

function requiredNonEmptyString(value: unknown, field: string): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error(`polybet-project.json: ${field} must be a non-empty string`);
  }
  return value.trim();
}

function optionalWithDefault(
  value: unknown,
  field: string,
  defaultVal: string,
  check: (s: string) => boolean,
): string {
  if (value === undefined || value === null || value === "") {
    return defaultVal;
  }
  if (typeof value !== "string") {
    throw new Error(`polybet-project.json: ${field} must be a string`);
  }
  const s = value.trim();
  if (!check(s)) {
    throw new Error(`polybet-project.json: invalid ${field}`);
  }
  return s;
}

function optionalBoolean(value: unknown, field: string): boolean | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value !== "boolean") {
    throw new Error(`polybet-project.json: ${field} must be a boolean when set`);
  }
  return value;
}

function optionalUrl(
  value: unknown,
  field: string,
): string | undefined {
  if (value === undefined || value === null || value === "") return undefined;
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error(`polybet-project.json: ${field} must be a non-empty string when set`);
  }
  const s = value.trim();
  try {
    const u = new URL(s);
    const ok =
      u.protocol === "http:" ||
      u.protocol === "https:" ||
      u.protocol === "socks5:" ||
      u.protocol === "socks5h:";
    if (!ok) {
      throw new Error(`unsupported protocol for ${field}`);
    }
  } catch (e) {
    throw new Error(
      `polybet-project.json: ${field} must be a valid proxy URL (${e instanceof Error ? e.message : ""})`,
    );
  }
  return s;
}

function isValidPortString(s: string): boolean {
  const n = Number(s);
  return Number.isInteger(n) && n >= 1 && n <= 65535;
}

function validateDatabaseUrl(url: string): void {
  const lower = url.toLowerCase();
  if (
    lower.startsWith("file:") ||
    lower.startsWith("postgres://") ||
    lower.startsWith("postgresql://")
  ) {
    return;
  }
  throw new Error(
    "polybet-project.json: databaseUrl must be a file: or postgres URL",
  );
}

/** Apply validated project config to env passed to the Go child process. */
export function applyPolybetProjectConfigToEnv(
  base: NodeJS.ProcessEnv,
  cfg: PolybetProjectConfig,
): NodeJS.ProcessEnv {
  const env = { ...base };
  env.DATABASE_URL = cfg.databaseUrl;
  env.HOST = cfg.host;
  env.PORT = cfg.port;
  if (cfg.outboundProxyUrl) {
    env.HTTP_PLATFORM_PROXY_URL = cfg.outboundProxyUrl;
  } else {
    delete env.HTTP_PLATFORM_PROXY_URL;
  }
  env.READ_ONLY_MODE = cfg.readOnlyMode ? "true" : "false";
  if (cfg.logLevel) {
    env.LOG_LEVEL = cfg.logLevel;
  }
  // Ensure Go does not read a different file that overrides our JSON.
  delete env.SPORTS_ROUTER_ENV_FILE;
  return env;
}
