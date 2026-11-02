import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import { createHash } from "node:crypto";

const require = createRequire(import.meta.url);
const helperFile = "install_awf_enclave_helper.cjs";
const diagnosticsFile = "install_enclave_diagnostics_trusted.cjs";
const temporaryDirectories = [];

function load(file, overrides = {}, processOverrides = {}) {
  const module = { exports: {} };
  vm.runInNewContext(fs.readFileSync(new URL(file, import.meta.url), "utf8"), {
    module,
    require: name => overrides[name] ?? require(name),
    process: { ...process, getuid: () => 0, geteuid: () => 0, ...processOverrides },
    Buffer,
    console,
  });
  return module.exports;
}

function temporaryDirectory() {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "trusted-enclave-installer-"));
  temporaryDirectories.push(directory);
  return directory;
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe.each([helperFile, diagnosticsFile])("%s regular file checks", file => {
  it("reads bounded single-link regular files and rejects oversize, empty, linked and non-regular inputs", () => {
    const { readRegular } = load(file);
    const directory = temporaryDirectory();
    const target = path.join(directory, "input");
    fs.writeFileSync(target, "verified");
    expect(readRegular(target, 8).toString()).toBe("verified");
    expect(() => readRegular(target, 7)).toThrow();
    fs.linkSync(target, `${target}-hard`);
    expect(() => readRegular(target, 8)).toThrow();
    fs.unlinkSync(`${target}-hard`);
    fs.symlinkSync(target, `${target}-link`);
    expect(() => readRegular(`${target}-link`, 8)).toThrow();
    expect(() => readRegular(directory, 1024)).toThrow();
    fs.writeFileSync(target, "");
    expect(() => readRegular(target, 8)).toThrow();
  });

  it("closes input descriptors when metadata or the final size check fails", () => {
    const closeSync = vi.fn();
    const filesystem = {
      ...fs,
      openSync: () => 12,
      closeSync,
      fstatSync: () => ({ isFile: () => true, nlink: 1, size: 8 }),
      readSync: () => 0,
    };
    expect(() => load(file, { "node:fs": filesystem }).readRegular("input", 8)).toThrow();
    expect(closeSync).toHaveBeenCalledWith(12);
  });

  it("requires both real and effective root before reading any installer input", () => {
    for (const ids of [{ getuid: () => 1001 }, { geteuid: () => 1001 }, { getuid: undefined }, { geteuid: undefined }]) {
      const openSync = vi.fn();
      const installer = load(file, { "node:fs": { ...fs, openSync } }, ids);
      const main = installer.installAWFEnclaveHelper ?? installer.installEnclaveDiagnosticsTrusted;
      expect(() => main(Array(6).fill("untrusted"))).toThrow();
      expect(openSync).not.toHaveBeenCalled();
    }
  });
});

function helperFixture() {
  const helper = Buffer.from('VERSION = "0.29.5"\n');
  const digest = data => createHash("sha256").update(data).digest("hex");
  const manifest = Buffer.from(`${digest(helper)}  enclave-storage-helper.py\n`);
  const files = { manifest, helper };
  const descriptors = new Map();
  let nextFD = 10;
  const filesystem = {
    ...fs,
    openSync: vi.fn(name => {
      const fd = nextFD++;
      descriptors.set(fd, { data: files[name], offset: 0 });
      return fd;
    }),
    fstatSync: vi.fn(fd => {
      const data = descriptors.get(fd).data;
      return { isFile: () => !!data, isDirectory: () => !data, size: data?.length, uid: 0, mode: 0o755, nlink: 1 };
    }),
    readSync: (fd, buffer, offset, length) => {
      const descriptor = descriptors.get(fd);
      const count = descriptor.data.copy(buffer, offset, descriptor.offset, descriptor.offset + length);
      descriptor.offset += count;
      return count;
    },
    closeSync: vi.fn(),
    mkdirSync: vi.fn(),
    fchmodSync: vi.fn(),
    fchownSync: vi.fn(),
    writeFileSync: vi.fn(),
    fsyncSync: vi.fn(),
    renameSync: vi.fn(),
    unlinkSync: vi.fn(),
  };
  const execFileSync = vi.fn();
  return {
    installer: load(helperFile, { "node:fs": filesystem, "node:child_process": { execFileSync } }),
    filesystem,
    execFileSync,
    args: ["manifest", "helper", digest(manifest), digest(helper), "0.29.5", "1001"],
    files,
    digest,
  };
}

describe("AWF helper installation", () => {
  it("publishes the authenticated helper and validates the narrow sudoers policy before rename", () => {
    const { installer, filesystem, execFileSync, args } = helperFixture();
    installer.installAWFEnclaveHelper(args);
    expect(filesystem.fchmodSync).toHaveBeenCalledWith(expect.any(Number), 0o644);
    expect(filesystem.fchmodSync).toHaveBeenCalledWith(expect.any(Number), 0o440);
    expect(filesystem.renameSync).toHaveBeenCalledTimes(2);
    expect(filesystem.writeFileSync.mock.calls[1][1].toString()).toBe("#1001 ALL=(root) NOPASSWD: /usr/bin/python3 -I /var/lib/awf/enclave-storage-helper.py *\n");
    expect(execFileSync.mock.calls[0]).toEqual(["/usr/bin/python3", ["-I", "/var/lib/awf/enclave-storage-helper.py", "install"], { env: { PATH: "/usr/bin:/bin", LANG: "C" }, stdio: "inherit", timeout: 120000 }]);
    expect(execFileSync.mock.calls[1][0]).toBe("/usr/sbin/visudo");
    expect(execFileSync.mock.calls[1][1][1]).toMatch(new RegExp(`^/proc/${process.pid}/fd/`));
    expect(execFileSync.mock.invocationCallOrder[1]).toBeLessThan(filesystem.renameSync.mock.invocationCallOrder[1]);
  });

  it.each([
    [2, "a".repeat(64), "Reviewed checksum manifest changed"],
    [3, "b".repeat(64), "Helper checksum mismatch"],
    [4, "0.29.6", "Helper version mismatch"],
    [4, "v0.29.5", "Invalid helper version"],
    [4, "0.29.5\n", "Invalid helper version"],
    [5, "0", "Invalid installing user"],
    [5, "2147483648", "Invalid installing user"],
    [5, "1001\n", "Invalid installing user"],
  ])("rejects invalid argument %i before publication", (index, value, message) => {
    const { installer, filesystem, args } = helperFixture();
    args[index] = value;
    expect(() => installer.installAWFEnclaveHelper(args)).toThrow(message);
    expect(filesystem.renameSync).not.toHaveBeenCalled();
  });

  it.each(["duplicate", "unbound"])("rejects %s manifest entries", kind => {
    const { installer, filesystem, args, files, digest } = helperFixture();
    files.manifest = kind === "duplicate" ? Buffer.concat([files.manifest, files.manifest]) : Buffer.from(`${"c".repeat(64)}  enclave-storage-helper.py\n`);
    args[2] = digest(files.manifest);
    expect(() => installer.installAWFEnclaveHelper(args)).toThrow("not bound");
    expect(filesystem.renameSync).not.toHaveBeenCalled();
  });

  it("rejects writable or runner-owned installation directories and closes their descriptors", () => {
    for (const info of [
      { uid: 1001, mode: 0o755 },
      { uid: 0, mode: 0o777 },
    ]) {
      const { installer, filesystem } = helperFixture();
      filesystem.fstatSync.mockReturnValue({ isDirectory: () => true, ...info });
      expect(() => installer.openDirectory("/var/lib/awf", true)).toThrow("Unsafe helper installation directory");
      expect(filesystem.closeSync).toHaveBeenCalledTimes(1);
    }
  });

  it("does not publish an invalid sudoers policy", () => {
    const { installer, filesystem, execFileSync, args } = helperFixture();
    execFileSync.mockImplementation(command => {
      if (command === "/usr/sbin/visudo") throw new Error("invalid policy");
    });
    expect(() => installer.installAWFEnclaveHelper(args)).toThrow("invalid policy");
    expect(filesystem.renameSync).toHaveBeenCalledTimes(1);
    expect(filesystem.unlinkSync).toHaveBeenCalledTimes(2);
  });
});

describe("trusted diagnostics installation", () => {
  it.each(["x64", "arm64"])("rejects a corrupt executable before reading the collector or modifying protected destinations (%s)", arch => {
    const directory = temporaryDirectory();
    const executable = path.join(directory, "node");
    fs.writeFileSync(executable, "untrusted executable");
    const mkdirSync = vi.fn();
    const { installEnclaveDiagnosticsTrusted } = load(diagnosticsFile, { "node:fs": { ...fs, mkdirSync } }, { arch });
    expect(() => installEnclaveDiagnosticsTrusted([executable, "missing-collector"])).toThrow("node-checksum");
    expect(mkdirSync).not.toHaveBeenCalled();
  });

  it("rejects a changed collector before modifying protected destinations", () => {
    const directory = temporaryDirectory();
    const executable = path.join(directory, "node");
    const collector = path.join(directory, "collector.cjs");
    fs.writeFileSync(executable, "fixture executable");
    fs.writeFileSync(collector, "untrusted collector");
    const mkdirSync = vi.fn();
    const digests = ["e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb", "a".repeat(64)];
    const crypto = {
      createHash: () => ({ update: () => ({ digest: () => digests.shift() }) }),
    };
    const { installEnclaveDiagnosticsTrusted } = load(diagnosticsFile, { "node:fs": { ...fs, mkdirSync }, "node:crypto": crypto }, { arch: "x64" });
    expect(() => installEnclaveDiagnosticsTrusted([executable, collector])).toThrow("collector-checksum");
    expect(mkdirSync).not.toHaveBeenCalled();
  });

  it("rejects unsafe protected directories", () => {
    const directory = temporaryDirectory();
    fs.chmodSync(directory, 0o777);
    const { protectedDirectory } = load(diagnosticsFile);
    expect(() => protectedDirectory(directory)).toThrow("unsafe-directory");
  });

  it("verifies already-installed ownership, mode and contents without replacing them", () => {
    const directory = temporaryDirectory();
    const target = path.join(directory, "installed");
    const data = Buffer.from("verified");
    fs.writeFileSync(target, data, { mode: 0o600 });
    const filesystem = { ...fs, lstatSync: name => ({ ...fs.lstatSync(name), uid: 0 }), writeFileSync: vi.fn() };
    const { installVerified } = load(diagnosticsFile, { "node:fs": filesystem });
    installVerified(target, data, 0o600);
    expect(filesystem.writeFileSync).not.toHaveBeenCalled();
    expect(() => installVerified(target, Buffer.from("modified"), 0o600)).toThrow("installed-checksum");
    fs.chmodSync(target, 0o644);
    expect(() => installVerified(target, data, 0o600)).toThrow("unsafe-installed-file");
  });

  it("creates new files exclusively with their exact installed mode", () => {
    const target = path.join(temporaryDirectory(), "new-node");
    const { installVerified } = load(diagnosticsFile);
    installVerified(target, Buffer.from("verified"), 0o500);
    expect(fs.readFileSync(target).toString()).toBe("verified");
    expect(fs.statSync(target).mode & 0o7777).toBe(0o500);
  });
});
