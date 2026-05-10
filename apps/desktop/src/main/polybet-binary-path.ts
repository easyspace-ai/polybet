import { join } from "node:path";
import { app } from "electron";

/** Path to the bundled `polybet` Go server binary (dev + packaged layouts). */
export function getBundledPolybetBinaryPath(): string {
  const name = process.platform === "win32" ? "polybet.exe" : "polybet";
  return join(app.getAppPath(), "resources", "bin", name).replace(
    "app.asar",
    "app.asar.unpacked",
  );
}
