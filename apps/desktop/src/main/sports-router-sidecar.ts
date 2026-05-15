import { spawn, type ChildProcess, type SpawnOptions } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir } from "node:fs/promises";
import { app } from "electron";
import { is } from "@electron-toolkit/utils";
import { applyPolybetProjectConfigToEnv } from "../shared/polybet-project-config";
import type { PolybetProjectConfig } from "../shared/polybet-project-config";
import { getBundledPolybetBinaryPath } from "./polybet-binary-path";
import { getPolybetEmbeddedServerDataDir } from "./polybet-embedded-dir";
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

const MAX_RETRIES = 5;
const BASE_RETRY_MS = 1_000;
let retryCount = 0;
let restartTimeout: ReturnType<typeof setTimeout> | null = null;

export function setSidecarStatusCallback(cb: (status: SidecarStatus) => void): void {
  statusCallback = cb;
}

function emitStatus(s: SidecarStatus): void {
  statusCallback?.(s);
}

export function getLocalDashboardURL(): string | null {
  return dashboardOrigin;
}

async function waitHealth(base: string, timeoutMs: number): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${base}/api/health`, {
        signal: AbortSignal.timeout(900),
      });
      if (res.ok) return true;
    } catch {
      /* retry */
    }
    await new Promise((r) => setTimeout(r, 250));
  }
  return false;
}

function spawnSidecar(bin: string, cwd: string, env: Record<string, string | undefined>, project: PolybetProjectConfig, _origin?: string): void {
  const opts: SpawnOptions = { cwd, env, stdio: "inherit" };
  if (process.platform === "win32") {
    opts.windowsHide = true;
    // Packaged app: avoid attaching a console; dev keeps stdio for local logs.
    if (!is.dev) opts.stdio = "ignore";
  }
  child = spawn(bin, [], opts);
  emitStatus({ state: "starting" });

  child.on("error", (err) => {
    console.error("[polybet] spawn error:", err);
    child = null;
    handleCrash(err.message, project);
  });

  child.on("exit", (code, signal) => {
    if (code === 0 || signal === "SIGTERM" || signal === "SIGKILL") {
      // Intentional stop — not a crash
      return;
    }
    console.error(`[polybet] process exited unexpectedly (code=${code}, signal=${signal})`);
    child = null;
    handleCrash(`exit code ${code ?? signal}`, project);
  });
}

function handleCrash(reason: string, project: PolybetProjectConfig): void {
  if (retryCount >= MAX_RETRIES) {
    emitStatus({ state: "crashed", error: reason, willRestart: false, retryCount });
    return;
  }
  retryCount++;
  const delay = BASE_RETRY_MS * Math.pow(2, retryCount - 1);
  emitStatus({ state: "crashed", error: reason, willRestart: true, retryCount });
  restartTimeout = setTimeout(() => {
    startWithRetry(project);
  }, delay);
}

async function startWithRetry(project: PolybetProjectConfig): Promise<void> {
  const bin = getBundledPolybetBinaryPath();
  if (!existsSync(bin)) {
    emitStatus({ state: "crashed", error: "binary disappeared", willRestart: false, retryCount });
    return;
  }
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
  emitStatus({ state: "ready", origin });
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
  if (restartTimeout) {
    clearTimeout(restartTimeout);
    restartTimeout = null;
  }
  retryCount = MAX_RETRIES; // prevent auto-restart
  if (child && !child.killed) {
    child.kill("SIGTERM");
  }
  child = null;
  dashboardOrigin = null;
  emitStatus({ state: "stopped" });
}
