// Produce provenance immediately after the offline compiler succeeds.
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";
const root = fileURLToPath(new URL("../", import.meta.url)),
  out = process.env.AUTOBLOW_RESULTS;
if (!out) throw Error("AUTOBLOW_RESULTS required");
const sha = (b) => crypto.createHash("sha256").update(b).digest("hex");
const sourceFiles = {};
function walk(relative) {
  const absolute = path.join(root, relative);
  if (fs.statSync(absolute).isDirectory()) {
    for (const name of fs.readdirSync(absolute).sort()) walk(relative + "/" + name);
    return;
  }
  if (relative.endsWith("_test.go")) return;
  sourceFiles[relative] = sha(fs.readFileSync(absolute));
}
for (const relative of ["go.mod", "go.sum", "core", "internal", "cmd"]) walk(relative);
const binaries = {};
for (const name of [
  "autoblow-server",
  "autoblow-catalog-import",
  "autoblow-server.exe",
  "autoblow-catalog-import.exe",
  "AutoblowLocal.exe",
  "ca-certificates.crt",
]) {
  const b = fs.readFileSync(path.join(out, name));
  binaries[name] = { sha256: sha(b), bytes: b.length };
}
fs.writeFileSync(
  path.join(out, "build-receipt.json"),
  JSON.stringify(
    {
      version: fs.readFileSync(path.join(out, "version.txt"), "utf8").trim(),
      go: fs.readFileSync(path.join(out, "go-version.txt"), "utf8").trim(),
      sourceFiles,
      binaries,
    },
    null,
    2,
  ) + "\n",
);
