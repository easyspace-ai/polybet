import { spawn, type ChildProcess } from "node:child_process";
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

let child: ChildProcess | null = null;
let dashboardOrigin: string | null = null;
let beforeQuitHooked = false;

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

  child = spawn(bin, [], {
    cwd,
    env,
    stdio: "inherit",
  });

  child.on("error", (err) => {
    console.error("[polybet] spawn error:", err);
  });

  const ok = await waitHealth(origin, 45_000);
  if (!ok) {
    console.error("[polybet] /api/health did not become ready in time");
    child.kill("SIGTERM");
    child = null;
    return false;
  }

  dashboardOrigin = origin;

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
  if (child && !child.killed) {
    child.kill("SIGTERM");
  }
  child = null;
  dashboardOrigin = null;
}
