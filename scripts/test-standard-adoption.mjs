// Run after installing the pinned toolkit. All mutations stay in a disposable repository.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
const root = path.resolve(import.meta.dirname, "..");
const temp = fs.mkdtempSync(path.join(os.tmpdir(), "atblse-standard-test-"));
const git = (...args) =>
  execFileSync("git", args, {
    cwd: temp,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
const write = (name, value) => {
  fs.mkdirSync(path.dirname(path.join(temp, name)), { recursive: true });
  fs.writeFileSync(path.join(temp, name), value);
};
const checks = [];
try {
  fs.cpSync(path.join(root, ".standards"), path.join(temp, ".standards"), { recursive: true });
  write(".gitignore", ".standards/node_modules/\n.standards/bin/\n");
  write(".repo-policy.json", fs.readFileSync(path.join(root, ".repo-policy.json")));
  for (const name of [
    "README.md",
    "README.ru.md",
    "docs/en/development-standard.md",
    "docs/ru/development-standard.md",
  ])
    write(
      name,
      "# Isolated verification fixture\n\nThis synthetic example documents the first baseline behavior.\n",
    );
  git("init", "-q");
  git("config", "user.name", "Synthetic verification");
  git("config", "user.email", "fixture@example.invalid");
  git("add", ".");
  git("-c", "core.hooksPath=/dev/null", "commit", "-qm", "baseline");
  const base = git("rev-parse", "HEAD");
  const docs = () => {
    for (const name of [
      "README.md",
      "README.ru.md",
      "docs/en/development-standard.md",
      "docs/ru/development-standard.md",
    ])
      write(
        name,
        "# Isolated verification fixture\n\nThe candidate changes validation and adds a recoverable local workflow.\n",
      );
  };
  for (const name of [
    "valid",
    "bad-format",
    "missing-language",
    "whitespace-only",
    "deleted-docs",
    "synthetic-secret",
    "scanner-unavailable",
  ]) {
    git("reset", "--hard", base);
    git("clean", "-fd");
    docs();
    if (name === "bad-format") write("sample.js", "const value= {x:1};;\n");
    if (name === "missing-language") fs.unlinkSync(path.join(temp, "README.ru.md"));
    if (name === "whitespace-only")
      write("README.ru.md", git("show", `${base}:README.ru.md`) + "\n\n");
    if (name === "deleted-docs") fs.unlinkSync(path.join(temp, "docs/ru/development-standard.md"));
    if (name === "synthetic-secret")
      write("synthetic-secret.txt", "ghp_" + crypto.randomBytes(20).toString("hex") + "\n");
    git("add", ".");
    git("-c", "core.hooksPath=/dev/null", "commit", "-qm", name);
    const binary = path.join(
      temp,
      ".standards/bin",
      process.platform === "win32" ? "gitleaks.exe" : "gitleaks",
    );
    if (name === "scanner-unavailable") fs.renameSync(binary, binary + ".held");
    let result;
    try {
      result = spawnSync(
        process.execPath,
        [".standards/check.mjs", "ci", base, git("rev-parse", "HEAD")],
        { cwd: temp, encoding: "utf8", timeout: 30000 },
      );
    } finally {
      if (name === "scanner-unavailable") fs.renameSync(binary + ".held", binary);
    }
    assert.equal(result.error, undefined, `${name}: checker did not finish`);
    assert.equal(
      result.status === 0,
      name === "valid",
      `${name}: unexpected checker outcome\n${result.stdout}\n${result.stderr}`,
    );
    checks.push({ name, outcome: name === "valid" ? "accepted" : "rejected" });
  }
  console.log(
    JSON.stringify({ checks, source: "installed pinned toolkit", externalServices: false }),
  );
} finally {
  assert(
    path.dirname(temp) === os.tmpdir() && path.basename(temp).startsWith("atblse-standard-test-"),
  );
  fs.rmSync(temp, { recursive: true, force: true });
}
