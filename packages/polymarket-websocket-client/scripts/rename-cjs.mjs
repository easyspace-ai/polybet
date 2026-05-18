import fs from "node:fs";
import path from "node:path";

const dir = path.resolve("dist/cjs");

function walk(d) {
  for (const ent of fs.readdirSync(d, { withFileTypes: true })) {
    const p = path.join(d, ent.name);
    if (ent.isDirectory()) walk(p);
    else if (p.endsWith(".js")) {
      fs.renameSync(p, p.slice(0, -3) + ".cjs");
    }
  }
}

if (fs.existsSync(dir)) walk(dir);
