import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import childProcess from "child_process";
import crypto from "crypto";
import fs from "fs";
import path from "path";
import { main, validateEnvelope } from "./validate_recovery_envelopes.cjs";
import { main as redactSecrets, processFile } from "./redact_secrets.cjs";

const MAGIC = Buffer.from("GH-AW-OBJECT-PACK 1\n");
const hash = bytes => crypto.createHash("sha256").update(bytes).digest("hex");

describe("final recovery envelope upload boundary", () => {
  let root, repo, originalCore;
  const secret = "secret-that-must-not-be-in-an-artifact";
  function git(args, options = {}) {
    return childProcess.execFileSync("git", args, { cwd: repo, ...options });
  }
  function envelope(directory = root) {
    fs.mkdirSync(directory, { recursive: true });
    const tip = git(["rev-parse", "HEAD"]).toString().trim();
    const ids = git(["rev-list", "--objects", "--no-object-names", tip]).toString().trim().split("\n").sort();
    const chunks = [Buffer.from("GH-AW object-pack recovery preview v1\n")];
    for (const id of ids) {
      const type = git(["cat-file", "-t", id]).toString().trim();
      const bytes = git(["cat-file", type, id]);
      chunks.push(Buffer.from(`\nObject ${id} ${type} ${bytes.length}\n`), bytes);
    }
    const preview = Buffer.from(Buffer.concat(chunks).toString("utf8"));
    const pack = git(["pack-objects", "--stdout", "--window=0", "--no-reuse-delta", "--no-reuse-object"], { input: ids.join("\n") + "\n" });
    git(["index-pack", "--stdin", "--strict"], { input: pack });
    const objectFormat = git(["rev-parse", "--show-object-format"]).toString().trim();
    const metadata = { tip, objectFormat, previewHash: hash(preview), packHash: hash(pack) };
    const patch = path.join(directory, "aw-change.patch");
    const bundle = patch.replace(/\.patch$/, ".bundle");
    const bytes = Buffer.concat([MAGIC, Buffer.from(JSON.stringify(metadata) + "\n"), pack]);
    fs.writeFileSync(patch, preview);
    fs.writeFileSync(bundle, bytes);
    return { patch, bundle, bytes };
  }
  beforeEach(() => {
    root = path.resolve(".recovery-upload-" + crypto.randomUUID());
    repo = path.join(root, "repo");
    fs.mkdirSync(repo, { recursive: true });
    git(["init", "--initial-branch=main", "--object-format=sha1"]);
    git(["config", "user.name", "Upload Test"]);
    git(["config", "user.email", "upload@example.com"]);
    fs.writeFileSync(path.join(repo, "file.txt"), "base\n");
    git(["add", "."]);
    git(["commit", "-m", "base"]);
    fs.writeFileSync(path.join(repo, "file.txt"), secret);
    git(["commit", "-am", "intermediate secret"]);
    fs.writeFileSync(path.join(repo, "file.txt"), "clean final tree\n");
    git(["commit", "-am", "remove secret"]);
    originalCore = global.core;
    global.core = { warning: vi.fn(), info: vi.fn(), debug: vi.fn() };
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    global.core = originalCore;
    fs.rmSync(root, { recursive: true, force: true });
  });
  it.each(["success", "read-failure"])("redacts the native trajectory or blocks publication on %s", async outcome => {
    global.core = { ...global.core, error: vi.fn(), setFailed: vi.fn() };
    vi.stubEnv("RUNNER_TEMP", path.join(root, "runner-temp"));
    vi.stubEnv("HOME", path.join(root, "home"));
    vi.stubEnv("GITHUB_WORKSPACE", repo);
    vi.stubEnv("GH_AW_SECRET_NAMES", "MINI_TRAJECTORY_TEST");
    vi.stubEnv("SECRET_MINI_TRAJECTORY_TEST", secret);
    const exists = fs.existsSync;
    vi.spyOn(fs, "existsSync").mockImplementation(file => (String(file) === "/tmp/gh-aw" || String(file).startsWith("/tmp/gh-aw/") ? false : exists(file)));
    const artifactRoot = path.join(process.env.RUNNER_TEMP, "gh-aw");
    const trajectory = path.join(artifactRoot, "agent/mini-swe-agent.traj.json");
    fs.mkdirSync(path.dirname(trajectory), { recursive: true });
    fs.writeFileSync(
      trajectory,
      JSON.stringify({
        info: { exit_status: "RuntimeError" },
        messages: [{ role: "user", content: secret }],
      })
    );
    const read = fs.readFileSync;
    const failure =
      outcome === "read-failure"
        ? vi.spyOn(fs, "readFileSync").mockImplementation((file, ...args) => {
            if (file === trajectory) throw Object.assign(new Error(secret), { code: "EIO" });
            return read(file, ...args);
          })
        : undefined;
    try {
      await redactSecrets();
    } finally {
      failure?.mockRestore();
    }
    if (outcome === "read-failure") {
      expect(fs.existsSync(trajectory)).toBe(false);
      expect(global.core.setFailed).toHaveBeenCalled();
    } else {
      expect(global.core.setFailed).not.toHaveBeenCalled();
      expect(JSON.parse(fs.readFileSync(trajectory, "utf8"))).toEqual({
        info: { exit_status: "RuntimeError" },
        messages: [{ role: "user", content: "***REDACTED***" }],
      });
      expect(main({ roots: [artifactRoot] })).toEqual({ removed: 0 });
      const external = path.join(root, "external-trajectory.json");
      fs.writeFileSync(external, secret);
      fs.unlinkSync(trajectory);
      fs.symlinkSync(external, trajectory);
      expect(() => main({ roots: [artifactRoot] })).toThrow("escapes trusted roots");
    }
    for (const method of ["info", "warning", "error", "setFailed"]) expect(JSON.stringify(global.core[method].mock.calls)).not.toContain(secret);
  });
  it.each(["MAX_OBJECTS", "MAX_BYTES"])("removes envelopes that exceed the trusted %s upload limit", name => {
    const artifact = envelope();
    vi.stubEnv(`GH_AW_OBJECT_PACK_${name}`, "1");
    expect(validateEnvelope(artifact.bundle)).toBe(true);
    expect(fs.existsSync(artifact.bundle)).toBe(false);
  });
  it("uses the configured scan entry budget and fails rather than silently skipping artifacts", () => {
    envelope();
    vi.stubEnv("GH_AW_OBJECT_PACK_MAX_OBJECTS", "1");
    expect(() => main({ roots: [root] })).toThrow("scan exceeded");
    vi.stubEnv("GH_AW_OBJECT_PACK_MAX_OBJECTS", "100");
    expect(main({ roots: [root] })).toEqual({ removed: 0 });
  });
  it("rejects invalid repository limit configuration before allowing any upload", () => {
    envelope();
    vi.stubEnv("GH_AW_OBJECT_PACK_MAX_OBJECTS", "untrusted-value");
    expect(() => main({ roots: [root] })).toThrow("Invalid GH_AW_OBJECT_PACK_MAX_OBJECTS");
  });
  it("removes a real packed secret from intermediate history after built-in redaction", () => {
    const artifact = envelope();
    expect(git(["show", "HEAD:file.txt"]).toString()).not.toContain(secret);
    expect(fs.readFileSync(artifact.patch, "utf8")).toContain(secret);
    expect(processFile(artifact.patch, [secret])).toBeGreaterThan(0);
    expect(fs.readFileSync(artifact.bundle)).toEqual(artifact.bytes);
    expect(main({ roots: [root] })).toEqual({ removed: 1 });
    expect(fs.existsSync(artifact.bundle)).toBe(false);
    expect(fs.readFileSync(artifact.patch, "utf8")).not.toContain(secret);
  });
  it.each(["redacted", "omitted"])("rejects a pre-forged %s preview even when both metadata hashes match", kind => {
    const artifact = envelope();
    const headerEnd = artifact.bytes.indexOf(10, MAGIC.length);
    const metadata = JSON.parse(artifact.bytes.subarray(MAGIC.length, headerEnd).toString("utf8"));
    const pack = artifact.bytes.subarray(headerEnd + 1);
    if (kind === "redacted") {
      expect(processFile(artifact.patch, [secret])).toBeGreaterThan(0);
    } else {
      fs.writeFileSync(artifact.patch, "GH-AW object-pack recovery preview v1\n");
    }
    const preview = fs.readFileSync(artifact.patch);
    expect(preview.toString()).not.toContain(secret);
    metadata.previewHash = hash(preview);
    metadata.packHash = hash(pack);
    fs.writeFileSync(artifact.bundle, Buffer.concat([MAGIC, Buffer.from(JSON.stringify(metadata) + "\n"), pack]));
    expect(hash(fs.readFileSync(artifact.patch))).toBe(metadata.previewHash);
    expect(hash(pack)).toBe(metadata.packHash);
    expect(main({ roots: [root] })).toEqual({ removed: 1 });
    expect(fs.existsSync(artifact.bundle)).toBe(false);
  });
  it.each(["checksum", "object-count"])("rejects a malformed %s pack despite self-consistent envelope hashes", kind => {
    const artifact = envelope();
    const headerEnd = artifact.bytes.indexOf(10, MAGIC.length);
    const metadata = JSON.parse(artifact.bytes.subarray(MAGIC.length, headerEnd).toString("utf8"));
    const pack = Buffer.from(artifact.bytes.subarray(headerEnd + 1));
    if (kind === "checksum") {
      pack[pack.length - 1] ^= 1;
    } else {
      pack.writeUInt32BE(100001, 8);
      crypto
        .createHash("sha1")
        .update(pack.subarray(0, -20))
        .digest()
        .copy(pack, pack.length - 20);
    }
    metadata.packHash = hash(pack);
    fs.writeFileSync(artifact.bundle, Buffer.concat([MAGIC, Buffer.from(JSON.stringify(metadata) + "\n"), pack]));
    expect(main({ roots: [root] })).toEqual({ removed: 1 });
    expect(fs.existsSync(artifact.bundle)).toBe(false);
  });
  it("preserves complete unchanged SHA-256 object previews", () => {
    repo = path.join(root, "sha256");
    fs.mkdirSync(repo);
    git(["init", "--initial-branch=main", "--object-format=sha256"]);
    git(["config", "user.name", "Upload Test"]);
    git(["config", "user.email", "upload@example.com"]);
    fs.writeFileSync(path.join(repo, "file.txt"), "sha256 content\n");
    git(["add", "."]);
    git(["commit", "-m", "sha256"]);
    const artifact = envelope();
    expect(main({ roots: [root] })).toEqual({ removed: 0 });
    expect(fs.readFileSync(artifact.bundle)).toEqual(artifact.bytes);
  });
  it("preserves unchanged recovery envelopes and ordinary Git bundles byte-for-byte", () => {
    const artifact = envelope();
    const ordinary = path.join(root, "ordinary.bundle");
    git(["bundle", "create", ordinary, "HEAD"]);
    const original = fs.readFileSync(ordinary);
    expect(main({ roots: [root, root] })).toEqual({ removed: 0 });
    expect(fs.readFileSync(artifact.bundle)).toEqual(artifact.bytes);
    expect(fs.readFileSync(ordinary)).toEqual(original);
  });
  it.each(["removed", "custom-mask", "empty", "directory"])("removes envelopes with a %s preview", change => {
    const artifact = envelope(path.join(root, "claims", "claim-one"));
    if (change === "removed" || change === "directory") {
      fs.unlinkSync(artifact.patch);
      if (change === "directory") fs.mkdirSync(artifact.patch);
    } else {
      fs.writeFileSync(artifact.patch, change === "empty" ? "" : "custom mask replacement");
    }
    expect(main({ roots: [root] }).removed).toBe(1);
    expect(fs.existsSync(artifact.bundle)).toBe(false);
  });
  it("checks staging and ARC consolidation copies independently of the original", () => {
    const original = envelope();
    processFile(original.patch, [secret]);
    const arc = path.join(root, "runner", "gh-aw");
    const staging = path.join(root, "safeoutputs", "artifacts");
    for (const directory of [arc, staging]) {
      fs.mkdirSync(directory, { recursive: true });
      fs.writeFileSync(path.join(directory, "aw-change.bundle"), original.bytes);
    }
    fs.copyFileSync(original.patch, path.join(arc, "aw-change.patch"));
    expect(main({ roots: [root] }).removed).toBe(3);
  });
  it("rejects external drive roots even within the workspace or runner temp", () => {
    const artifact = envelope(path.join(root, "mounted-drive"));
    fs.unlinkSync(artifact.patch);
    vi.stubEnv("GITHUB_WORKSPACE", root);
    vi.stubEnv("RUNNER_TEMP", root);
    expect(() => main({ roots: [path.join(root, "runner")], additionalRoots: [path.dirname(artifact.bundle)] })).toThrow("Invalid additional recovery artifact roots");
    expect(fs.readFileSync(artifact.bundle)).toEqual(artifact.bytes);
  });
  it("does not accept additional roots from the inherited environment", () => {
    const artifact = envelope(path.join(root, "outside"));
    fs.unlinkSync(artifact.patch);
    vi.stubEnv("GH_AW_RECOVERY_ADDITIONAL_ROOTS", JSON.stringify([path.dirname(artifact.bundle)]));
    expect(main({ roots: [path.join(root, "runner")] }).removed).toBe(0);
    expect(fs.readFileSync(artifact.bundle)).toEqual(artifact.bytes);
  });
  it.each([[null], ["outside"], [[null]], [[1]]])("rejects malformed compiler-supplied roots: %j", additionalRoots => {
    expect(() => main({ roots: [root], additionalRoots })).toThrow("Invalid additional recovery artifact roots");
  });
  it.each(["directory", "bundle"])("rejects a %s symlink escaping an approved root", kind => {
    const approved = path.join(root, "approved");
    const outside = path.join(root, "approved-sibling");
    fs.mkdirSync(approved);
    const artifact = envelope(outside);
    fs.unlinkSync(artifact.patch);
    fs.symlinkSync(kind === "directory" ? outside : artifact.bundle, path.join(approved, kind === "directory" ? "escape" : "escape.bundle"));
    expect(() => main({ roots: [approved] })).toThrow("escapes trusted roots");
    expect(fs.readFileSync(artifact.bundle)).toEqual(artifact.bytes);
  });
  it("scans aliases inside approved roots once", () => {
    const approved = path.join(root, "approved");
    const artifact = envelope(path.join(approved, "nested"));
    fs.unlinkSync(artifact.patch);
    fs.symlinkSync(path.dirname(artifact.bundle), path.join(approved, "alias"));
    expect(main({ roots: [approved, approved] }).removed).toBe(1);
  });
  it("does not let a nested artifact root authorize a symlink escape", () => {
    const approved = path.join(root, "approved");
    const artifact = envelope(path.join(root, "outside"));
    fs.mkdirSync(approved);
    fs.unlinkSync(artifact.patch);
    const alias = path.join(approved, "alias");
    fs.symlinkSync(path.dirname(artifact.bundle), alias);
    expect(() => main({ roots: [approved], additionalRoots: [alias] })).toThrow("Invalid additional recovery artifact roots");
    expect(fs.readFileSync(artifact.bundle)).toEqual(artifact.bytes);
  });
  it("does not allow explicit drive roots to authorize external symlinks", () => {
    const approved = path.join(root, "approved");
    const mounted = path.join(root, "mounted-drive");
    const artifact = envelope(mounted);
    fs.mkdirSync(approved);
    vi.stubEnv("GITHUB_WORKSPACE", root);
    fs.unlinkSync(artifact.patch);
    fs.symlinkSync(mounted, path.join(approved, "drive-alias"));
    expect(() => main({ roots: [approved], additionalRoots: [mounted] })).toThrow("Invalid additional recovery artifact roots");
    expect(fs.readFileSync(artifact.bundle)).toEqual(artifact.bytes);
  });
  it("ignores missing approved roots", () => {
    expect(main({ roots: [path.join(root, "missing")], additionalRoots: [path.join(root, "missing", "nested")] })).toEqual({ removed: 0 });
  });
  it.each([["relative/path"], ["/"]])("rejects unsafe additional root %s", unsafe => {
    expect(() => main({ roots: [root], additionalRoots: [unsafe] })).toThrow("Invalid additional recovery artifact roots");
  });
  it("rejects additional roots outside approved bases, including via symlink", () => {
    const outside = path.join(root, "outside");
    fs.mkdirSync(outside);
    const link = path.join(root, "link");
    fs.symlinkSync(outside, link);
    vi.stubEnv("RUNNER_TEMP", path.join(root, "runner-temp"));
    vi.stubEnv("GITHUB_WORKSPACE", path.join(root, "workspace"));
    for (const candidate of [outside, link]) {
      expect(() => main({ roots: [path.join(root, "runner")], additionalRoots: [candidate] })).toThrow("Invalid additional recovery artifact roots");
    }
  });
  it("rejects an artifact root replaced with an external symlink", () => {
    const artifact = envelope(path.join(root, "external"));
    const alias = path.join(root, "artifact-root");
    fs.symlinkSync(path.dirname(artifact.bundle), alias);
    expect(() => main({ roots: [alias] })).toThrow("must not be a symbolic link");
    expect(fs.readFileSync(artifact.bundle)).toEqual(artifact.bytes);
  });
  it("allows explicit additional subdirectories without expanding the boundary", () => {
    const artifact = envelope(path.join(root, "nested"));
    fs.unlinkSync(artifact.patch);
    expect(main({ roots: [root], additionalRoots: [path.dirname(artifact.bundle)] })).toEqual({ removed: 1 });
  });
  it("removes a pack changed or appended after its preview was created", () => {
    const artifact = envelope();
    fs.appendFileSync(artifact.bundle, secret);
    expect(main({ roots: [root] }).removed).toBe(1);
  });
  it("rejects a symlink preview without following it", () => {
    const artifact = envelope();
    const original = artifact.patch + ".original";
    fs.renameSync(artifact.patch, original);
    fs.symlinkSync(original, artifact.patch);
    expect(main({ roots: [root] }).removed).toBe(1);
    expect(fs.existsSync(original)).toBe(true);
  });
  it.each(["invalid-json", "invalid-hash", "truncated"])("suppresses %s recovery metadata", kind => {
    const artifact = envelope();
    const metadata = kind === "invalid-json" ? "not-json\n" : kind === "invalid-hash" ? '{"previewHash":"invalid"}\n' : "{}";
    fs.writeFileSync(artifact.bundle, Buffer.concat([MAGIC, Buffer.from(metadata)]));
    expect(main({ roots: [root] }).removed).toBe(1);
  });
  it("fails closed when the rejected pack cannot be removed", () => {
    const artifact = envelope();
    fs.unlinkSync(artifact.patch);
    const unlink = fs.unlinkSync;
    vi.spyOn(fs, "unlinkSync").mockImplementation(filename => {
      if (filename === artifact.bundle) throw Object.assign(new Error("denied"), { code: "EACCES" });
      return unlink(filename);
    });
    expect(() => main({ roots: [root] })).toThrow("denied");
    expect(fs.existsSync(artifact.bundle)).toBe(true);
  });
  it("does not mistake short reads for ordinary bundle magic", () => {
    const artifact = envelope();
    fs.unlinkSync(artifact.patch);
    const read = fs.readSync;
    vi.spyOn(fs, "readSync").mockImplementation((fd, buffer, offset, length, position) => read(fd, buffer, offset, Math.min(length, 3), position));
    expect(main({ roots: [root] }).removed).toBe(1);
  });
  it("fails closed on directory traversal errors", () => {
    envelope();
    vi.spyOn(fs, "readdirSync").mockImplementation(() => {
      throw new Error("unreadable directory");
    });
    expect(() => main({ roots: [root] })).toThrow("unreadable directory");
  });
});
