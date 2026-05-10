import { homedir as nodeHomedir } from "node:os";
import { resolve, sep } from "node:path";
import { app } from "electron";

/**
 * Current user's profile / home directory.
 *
 * Prefer `app.getPath("home")` in Electron (aligns with OS conventions); fall
 * back to `os.homedir()` for tests or unusual bootstrap order.
 *
 * Typical resolution:
 * - macOS / Linux: same as `$HOME`
 * - Windows: same as `%USERPROFILE%` (e.g. `C:\Users\<name>`)
 */
export function getUserProfileHomeDir(): string {
  try {
    const h = app.getPath("home");
    if (typeof h === "string" && h.length > 0) {
      return h;
    }
  } catch {
    /* e.g. app not ready in rare unit contexts */
  }
  const fallback = nodeHomedir();
  return typeof fallback === "string" && fallback.length > 0
    ? fallback
    : resolve(".");
}

/**
 * Per-application writable directory for this Electron app.
 *
 * Electron resolves `userData` under the signed-in user's profile:
 * - macOS:   ~/Library/Application Support/<ProductName>
 * - Windows: %APPDATA%\<ProductName> → under %USERPROFILE%\AppData\Roaming
 * - Linux:   ~/.config/<ProductName> (or $XDG_CONFIG_HOME when set)
 */
export function getAppUserDataDir(): string {
  return app.getPath("userData");
}

/**
 * Best-effort check that an absolute path stays under the user profile
 * (guards odd portable / symlink layouts).
 */
export function isPathUnderUserProfile(dir: string): boolean {
  const home = resolve(getUserProfileHomeDir()) + sep;
  const d = resolve(dir) + sep;
  if (process.platform === "win32") {
    return d.toLowerCase().startsWith(home.toLowerCase());
  }
  return d.startsWith(home);
}
