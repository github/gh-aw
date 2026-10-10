import assert from "node:assert/strict";
import { lstatSync, mkdtempSync, readdirSync, readFileSync, rmSync, symlinkSync, linkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { ensureSecureDirectory, writeFileSecure } from "./secure-write.mjs";

function tmpDir(t) {
  const dir = mkdtempSync(join(tmpdir(), "secure-write-mjs-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function mode(filePath) {
  return lstatSync(filePath).mode & 0o777;
}

test("ensureSecureDirectory rejects a symlink planted at the target path", t => {
  const root = tmpDir(t);
  const real = join(root, "real");
  ensureSecureDirectory(real);
  const link = join(root, "linked");
  symlinkSync(real, link, "dir");
  assert.throws(() => ensureSecureDirectory(link), /non-directory output path/);
});

test("writeFileSecure creates a new file with 0600 mode and is safely repeatable", t => {
  const dir = tmpDir(t);
  const target = join(dir, "pr-sous-chef-candidates-compact.json");
  writeFileSecure(target, JSON.stringify({ prs: [] }));
  writeFileSecure(target, JSON.stringify({ prs: [1] }));
  assert.equal(readFileSync(target, "utf8"), JSON.stringify({ prs: [1] }));
  assert.equal(mode(target), 0o600);
  assert.equal(mode(dirname(target)), 0o700);
  assert.deepEqual(readdirSync(dir), ["pr-sous-chef-candidates-compact.json"]);
});

test("writeFileSecure replaces a pre-existing symlink or hard link at the destination without touching the original", t => {
  const dir = tmpDir(t);
  const outside = join(dir, "outside.json");
  writeFileSync(outside, "do-not-touch");

  const symlinkTarget = join(dir, "via-symlink.json");
  symlinkSync(outside, symlinkTarget);
  writeFileSecure(symlinkTarget, "safe");
  assert.equal(readFileSync(outside, "utf8"), "do-not-touch");
  assert.equal(readFileSync(symlinkTarget, "utf8"), "safe");

  const hardlinkTarget = join(dir, "via-hardlink.json");
  linkSync(outside, hardlinkTarget);
  writeFileSecure(hardlinkTarget, "safe");
  assert.equal(readFileSync(outside, "utf8"), "do-not-touch");
  assert.equal(readFileSync(hardlinkTarget, "utf8"), "safe");
});
