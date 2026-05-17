#!/usr/bin/env node
// Copy Vite dist/ into server/internal/webui/dashboard-dist for go:embed.
import { cp, rm, mkdir } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const dashboardRoot = resolve(here, "..");
const repoRoot = resolve(dashboardRoot, "..", "..");
const src = join(dashboardRoot, "dist");
const dest = join(repoRoot, "server", "internal", "webui", "dashboard-dist");

await rm(dest, { recursive: true, force: true });
await mkdir(dest, { recursive: true });
await cp(src, dest, { recursive: true });
console.log(`[dashboard] embedded dist → ${dest}`);
