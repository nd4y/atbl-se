// Initial baseline formatting; pinned toolkit files are immutable.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
const root = fileURLToPath(new URL("../", import.meta.url));
const prettier = await import("../.standards/node_modules/prettier/index.mjs");
const options = JSON.parse(fs.readFileSync(path.join(root, ".standards/format.json"), "utf8"));
let checked = 0,
  changed = 0;
async function walk(relative) {
  for (const entry of fs.readdirSync(path.join(root, relative), { withFileTypes: true })) {
    const name = path.posix.join(relative, entry.name);
    if ([".git", ".standards", "node_modules", "out", "dist", "data"].includes(entry.name))
      continue;
    if (entry.isDirectory()) {
      await walk(name);
      continue;
    }
    if (!entry.isFile() || entry.name === "package-lock.json") continue;
    const info = await prettier.getFileInfo(name, { resolveConfig: false });
    if (!info.inferredParser) continue;
    const absolute = path.join(root, name),
      before = fs.readFileSync(absolute, "utf8");
    const after = await prettier.format(before, { ...options, filepath: name });
    checked++;
    if (before !== after) {
      fs.writeFileSync(absolute, after);
      changed++;
    }
  }
}
await walk("");
console.log(JSON.stringify({ formattedFiles: checked, changed }));
