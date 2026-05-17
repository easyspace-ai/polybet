import { spawn, type ChildProcess, type SpawnOptions } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir } from "node:fs/promises";
import { app } from "electron";
import { is } from "@electron-toolkit/utils";
import { applyPolybetProjectConfigToEnv } from "../shared/polybet-project-config";
import type { PolybetProjectConfig } from "../shared/polybet-project-config";
import { getBundledPolybetBinaryPath } from "./polybet-binary-path";
import { getPolybetEmbeddedServerDataDir } from "./polybet-embedded-dir";
import { loadSidecarWatchdogSettings } from "./sidecar-watchdog-settings";
import {
  getAppUserDataDir,
  isPathUnderUserProfile,
} from "./user-profile-paths";

export type SidecarStatus =
  | { state: "starting" }
  | { state: "ready"; origin: string }
  | { state: "crashed"; error: string; willRestart: boolean; retryCount: number }
  | { state: "stopped" };

let child: ChildProcess | null = null;
let dashboardOrigin: string | null = null;
let beforeQuitHooked = false;
let statusCallback: ((status: SidecarStatus) => void) | null = null;
let activeProject: PolybetProjectConfig | null = null;

const BASE_RETRY_MS = 1_000;
let retryCount = 0;
let restartTimeout: ReturnType<typeof setTimeout> | null = null;
let watchdogTimer: ReturnType<typeof setInterval> | null = null;
let watchdogFailStreak = 0;
let watchdogRestartInFlight = false;
let intentionalChildStop = false;
let maxRetries = 0;

export function setSidecarStatusCallback(cb: (status: SidecarStatus) => void): void {
  statusCallback = cb;
}

function emitStatus(s: SidecarStatus): void {
  statusCallback?.(s);
}

export function getLocalDashboardURL(): string | null {
  return dashboardOrigin;
}

function stopWatchdog(): void {
  if (watchdogTimer) {
    clearInterval(watchdogTimer);
    watchdogTimer = null;
  }
  watchdogFailStreak = 0;
}

async function probeHealth(origin: string, timeoutMs: number): Promise<boolean> {
  try {
    const res = await fetch(`${origin}/api/health`, {
      signal: AbortSignal.timeout(timeoutMs),
    });
    return res.ok;
  } catch {
    return false;
  }
}

async function waitHealth(base: string, timeoutMs: number): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await probeHealth(base, 900)) return true;
    await new Promise((r) => setTimeout(r, 250));
  }
  return false;
}

function startWatchdog(origin: string): void {
  stopWatchdog();
  const settings = loadSidecarWatchdogSettings();
  maxRetries = settings.maxRetries;

  watchdogTimer = setInterval(() => {
    void (async () => {
      if (watchdogRestartInFlight || !activeProject) return;

      if (!child || child.killed) {
        watchdogFailStreak++;
        if (watchdogFailStreak >= settings.failThreshold) {
          await restartSidecarFromWatchdog("process not running");
        }
        return;
      }

      const ok = await probeHealth(origin, settings.httpTimeoutMs);
      if (ok) {
        watchdogFailStreak = 0;
        return;
      }

      watchdogFailStreak++;
      if (watchdogFailStreak >= settings.failThreshold) {
        await restartSidecarFromWatchdog("health check failed");
      }
    })();
  }, settings.intervalSec * 1000);
}

async function restartSidecarFromWatchdog(reason: string): Promise<void> {
  if (watchdogRestartInFlight || !activeProject) return;
  watchdogRestartInFlight = true;
  watchdogFailStreak = 0;
  stopWatchdog();

  const settings = loadSidecarWatchdogSettings();
  if (child && !child.killed) {
    intentionalChildStop = true;
    child.kill("SIGTERM");
    await new Promise<void>((resolve) => {
      const t = setTimeout(resolve, settings.killGraceMs);
      child?.once("exit", () => {
        clearTimeout(t);
        resolve();
      });
    });
    if (child && !child.killed) {
      child.kill("SIGKILL");
    }
    child = null;
  }

  watchdogRestartInFlight = false;
  handleCrash(`watchdog: ${reason}`, activeProject);
}

function spawnSidecar(bin: string, cwd: string, env: Record<string, string | undefined>, project: PolybetProjectConfig): void {
  const opts: SpawnOptions = { cwd, env, stdio: "inherit" };
  if (process.platform === "win32") {
    opts.windowsHide = true;
    if (!is.dev) opts.stdio = "ignore";
  }
  child = spawn(bin, [], opts);
  emitStatus({ state: "starting" });

  child.on("error", (err) => {
    console.error("[polybet] spawn error:", err);
    child = null;
    if (intentionalChildStop) {
      intentionalChildStop = false;
      return;
    }
    handleCrash(err.message, project);
  });

  child.on("exit", (code, signal) => {
    if (intentionalChildStop) {
      intentionalChildStop = false;
      return;
    }
    if (code === 0 || signal === "SIGTERM" || signal === "SIGKILL") {
      return;
    }
    console.error(`[polybet] process exited unexpectedly (code=${code}, signal=${signal})`);
    child = null;
    handleCrash(`exit code ${code ?? signal}`, project);
  });
}

function handleCrash(reason: string, project: PolybetProjectConfig): void {
  stopWatchdog();
  const limit = maxRetries > 0 ? maxRetries : Number.POSITIVE_INFINITY;
  if (retryCount >= limit) {
    emitStatus({ state: "crashed", error: reason, willRestart: false, retryCount });
    return;
  }
  retryCount++;
  const delay = BASE_RETRY_MS * Math.pow(2, Math.min(retryCount - 1, 6));
  emitStatus({ state: "crashed", error: reason, willRestart: true, retryCount });
  restartTimeout = setTimeout(() => {
    void startWithRetry(project);
  }, delay);
}

async function startWithRetry(project: PolybetProjectConfig): Promise<void> {
  const bin = getBundledPolybetBinaryPath();
  if (!existsSync(bin)) {
    emitStatus({ state: "crashed", error: "binary disappeared", willRestart: false, retryCount });
    return;
  }
  activeProject = project;
  const env = applyPolybetProjectConfigToEnv({ ...process.env }, project);
  const probeHost = env.HOST === "0.0.0.0" ? "127.0.0.1" : env.HOST ?? "127.0.0.1";
  const origin = `http://${probeHost}:${env.PORT}`;
  const userData = getAppUserDataDir();
  const cwd = is.dev ? getPolybetEmbeddedServerDataDir() : userData;

  spawnSidecar(bin, cwd, env, project);
  const ok = await waitHealth(origin, 45_000);
  if (!ok) {
    handleCrash("health check timed out after restart", project);
    return;
  }
  dashboardOrigin = origin;
  retryCount = 0;
  emitStatus({ state: "ready", origin });
  startWatchdog(origin);
}

/**
 * If `resources/bin/polybet` exists and `project` is valid, spawn it and
 * expose the dashboard origin. When the binary exists but `project` is null
 * (missing/invalid ~/.polybet/polybet-project.json), returns false without
 * spawning — the renderer must show project setup first.
 */
export async function maybeStartSportsRouterSidecar(
  project: PolybetProjectConfig | null,
): Promise<boolean> {
  const bin = getBundledPolybetBinaryPath();
  retryCount = 0;
  if (restartTimeout) {
    clearTimeout(restartTimeout);
    restartTimeout = null;
  }
  stopWatchdog();

  if (!existsSync(bin)) {
    return false;
  }

  if (!project) {
    console.warn(
      "[polybet] bundled server present but polybet-project.json is missing or invalid — open setup in the app window.",
    );
    return false;
  }

  const userData = getAppUserDataDir();
  if (!is.dev && !isPathUnderUserProfile(userData)) {
    console.warn(
      "[sports-router] userData is not under the user profile — using it anyway:",
      userData,
    );
  }

  activeProject = project;
  const settings = loadSidecarWatchdogSettings();
  maxRetries = settings.maxRetries;

  const cwd = is.dev ? getPolybetEmbeddedServerDataDir() : userData;
  if (is.dev) {
    await mkdir(cwd, { recursive: true });
  }
  const env = applyPolybetProjectConfigToEnv({ ...process.env }, project);

  const probeHost = env.HOST === "0.0.0.0" ? "127.0.0.1" : env.HOST ?? "127.0.0.1";
  const origin = `http://${probeHost}:${env.PORT}`;

  spawnSidecar(bin, cwd, env, project);

  const ok = await waitHealth(origin, 45_000);
  if (!ok) {
    handleCrash("health check timed out", project);
    return false;
  }

  dashboardOrigin = origin;
  emitStatus({ state: "ready", origin });
  startWatchdog(origin);

  if (!beforeQuitHooked) {
    beforeQuitHooked = true;
    app.on("before-quit", () => {
      stopEmbeddedPolybetSidecar();
    });
  }

  return true;
}

/** Stop the embedded Go server and clear the dashboard origin (e.g. after a failed outbound probe). */
export function stopEmbeddedPolybetSidecar(): void {
  stopWatchdog();
  if (restartTimeout) {
    clearTimeout(restartTimeout);
    restartTimeout = null;
  }
  retryCount = Number.MAX_SAFE_INTEGER;
  intentionalChildStop = true;
  if (child && !child.killed) {
    child.kill("SIGTERM");
  }
  child = null;
  dashboardOrigin = null;
  activeProject = null;
  emitStatus({ state: "stopped" });
}
