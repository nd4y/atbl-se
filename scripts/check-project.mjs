// Product-specific facts; the pinned standard bundle remains unmodified.
import fs from "node:fs";
import path from "node:path";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
const root = path.resolve(import.meta.dirname, "..");
const read = (name) => fs.readFileSync(path.join(root, name), "utf8");
const files = execFileSync("git", ["ls-files", "-z"], { cwd: root, encoding: "utf8" })
  .split("\0")
  .filter((name) => name && fs.existsSync(path.join(root, name)));
const version = read("core/server.go").match(/const Version = "([^"]+)"/)[1];
assert(read("windows/Launcher.ps1").includes(`-eq '${version}'`), "Windows health version drift");
assert(read("deploy/compose.yaml").includes(`image: atblse:${version}`), "image version drift");
assert.equal(
  JSON.parse(read("docs/contracts/vacuglide2-control-api.json")).current_version,
  version,
);
assert(read("go.mod").startsWith("module github.com/nd4y/atblse\n"), "Go module name drift");
for (const name of [
  "README.md",
  "README.ru.md",
  "SECURITY.md",
  "SECURITY.ru.md",
  "docs/en/project.md",
  "docs/ru/project.md",
])
  assert(read(name).includes(version), `${name}: version drift`);
for (const file of files.filter((f) => f.startsWith("docs/en/") && f.endsWith(".md")))
  assert(files.includes(file.replace("docs/en/", "docs/ru/")), `Missing translation: ${file}`);
for (const file of files.filter((f) => f.startsWith("docs/ru/") && f.endsWith(".md")))
  assert(files.includes(file.replace("docs/ru/", "docs/en/")), `Missing translation: ${file}`);
for (const file of files.filter((f) => f.endsWith(".md") && !f.startsWith(".standards/"))) {
  const text = read(file);
  for (const match of text.matchAll(/\]\(([^\s)]+)(?:\s+"[^"]*")?\)/g)) {
    const link = match[1];
    if (/^(?:https?:|mailto:|#)/.test(link)) continue;
    const target = decodeURIComponent(link.split("#")[0]);
    if (!target) continue;
    assert(
      fs.existsSync(path.resolve(root, path.dirname(file), target)),
      `${file}: broken link ${link}`,
    );
  }
}
console.log(`PASS: project version ${version}, bilingual counterparts and local link targets`);
