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
  assert.throws(() => ensureSecureDirectory(link), /symlinked component/);
});

test("ensureSecureDirectory rejects a symlinked ancestor several levels above the target", t => {
  const root = tmpDir(t);
  const real = path.join(root, "real");
  fs.mkdirSync(real);
  const link = path.join(root, "linked");
  fs.symlinkSync(real, link, "dir");
  const nested = path.join(link, "child", "grandchild");
  assert.throws(() => ensureSecureDirectory(nested), /symlinked component/);
  // The symlink target itself must stay untouched (no directories created through it).
  assert.equal(fs.readdirSync(real).length, 0);
});

test("ensureSecureDirectory rejects a pre-existing directory that is group/other-writable", t => {
  const root = tmpDir(t);
  const dir = path.join(root, "insecure");
  fs.mkdirSync(dir);
  fs.chmodSync(dir, 0o777); // chmod bypasses umask, unlike the mkdirSync mode option
  assert.throws(() => ensureSecureDirectory(dir), /untrusted or insecurely-permissioned directory/);
});

test("ensureSecureDirectory rejects a permissive directory several levels above the target", t => {
  const root = tmpDir(t);
  const insecureParent = path.join(root, "insecure");
  fs.mkdirSync(insecureParent);
  fs.chmodSync(insecureParent, 0o777);
  const nested = path.join(insecureParent, "child");
  assert.throws(() => ensureSecureDirectory(nested), /untrusted or insecurely-permissioned directory/);
  // Nothing was created underneath the untrusted ancestor.
  assert.deepEqual(fs.readdirSync(insecureParent), []);
});

test("ensureSecureDirectory accepts the standard sticky, world-writable /tmp model even when not owned by us", t => {
  // os.tmpdir() itself is the trust boundary and is not walked component by
  // component, so directories like the real system /tmp (sticky + world
  // writable, usually root-owned) must not be rejected outright.
  const stat = fs.lstatSync(os.tmpdir());
  if (typeof process.getuid === "function" && stat.uid !== process.getuid()) {
    assert.doesNotThrow(() => ensureSecureDirectory(os.tmpdir()));
  }
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
