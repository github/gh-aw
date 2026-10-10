const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { ensureSecureDirectory, writeFileSecure } = require("./secure_write.cjs");

function tmpDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "secure-write-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function mode(filePath) {
  return fs.lstatSync(filePath).mode & 0o777;
}

test("ensureSecureDirectory creates missing parents with restrictive mode", t => {
  const root = tmpDir(t);
  const nested = path.join(root, "a", "b", "c");
  ensureSecureDirectory(nested);
  assert.equal(fs.lstatSync(nested).isDirectory(), true);
  assert.equal(mode(nested), 0o700);
});

test("ensureSecureDirectory is idempotent for an already-valid directory", t => {
  const root = tmpDir(t);
  const dir = path.join(root, "out");
  ensureSecureDirectory(dir);
  assert.doesNotThrow(() => ensureSecureDirectory(dir));
});

test("ensureSecureDirectory rejects a symlink planted at the target path", t => {
  const root = tmpDir(t);
  const real = path.join(root, "real");
  fs.mkdirSync(real);
  const link = path.join(root, "linked");
  fs.symlinkSync(real, link, "dir");
  assert.throws(() => ensureSecureDirectory(link), /non-directory output path/);
});

test("writeFileSecure creates a new file with 0600 mode and exact content", t => {
  const dir = tmpDir(t);
  const target = path.join(dir, "out.json");
  writeFileSecure(target, JSON.stringify({ ok: true }));
  assert.equal(fs.readFileSync(target, "utf8"), JSON.stringify({ ok: true }));
  assert.equal(mode(target), 0o600);
});

test("writeFileSecure is safely repeatable against the same fixed path", t => {
  const dir = tmpDir(t);
  const target = path.join(dir, "out.json");
  writeFileSecure(target, "first");
  writeFileSecure(target, "second");
  assert.equal(fs.readFileSync(target, "utf8"), "second");
  // No leftover staging files from either invocation.
  assert.deepEqual(fs.readdirSync(dir), ["out.json"]);
});

test("writeFileSecure replaces a pre-existing symlink at the destination without following it", t => {
  const dir = tmpDir(t);
  const outside = path.join(dir, "outside.json");
  fs.writeFileSync(outside, "do-not-touch");
  const target = path.join(dir, "out.json");
  fs.symlinkSync(outside, target);
  writeFileSecure(target, "safe-content");
  assert.equal(fs.readFileSync(outside, "utf8"), "do-not-touch");
  assert.equal(fs.lstatSync(target).isSymbolicLink(), false);
  assert.equal(fs.readFileSync(target, "utf8"), "safe-content");
});

test("writeFileSecure replaces a pre-existing hard link at the destination without truncating the shared inode", t => {
  const dir = tmpDir(t);
  const outside = path.join(dir, "outside.json");
  fs.writeFileSync(outside, "do-not-touch");
  const target = path.join(dir, "out.json");
  fs.linkSync(outside, target);
  writeFileSecure(target, "safe-content");
  assert.equal(fs.readFileSync(outside, "utf8"), "do-not-touch");
  assert.equal(fs.readFileSync(target, "utf8"), "safe-content");
});

test("writeFileSecure creates missing parent directories securely", t => {
  const root = tmpDir(t);
  const target = path.join(root, "nested", "dir", "out.json");
  writeFileSecure(target, "content");
  assert.equal(fs.readFileSync(target, "utf8"), "content");
  assert.equal(mode(path.dirname(target)), 0o700);
});
