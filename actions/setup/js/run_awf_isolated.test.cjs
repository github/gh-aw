// @ts-check

import { describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const path = require("node:path");
const { EventEmitter } = require("node:events");
const { spawn } = require("node:child_process");
const { AWF_CONTAINERS, LOCK_SCRIPT, lockPath, assertLockStat, assertLockIdentity, openLock, dockerQuery, connectionEnvironment, daemonIdentity, preflight, exitStatus, supervise } = require("./run_awf_isolated.cjs");

function stat(overrides = {}) {
  return { isFile: () => true, nlink: 1, size: 0, mode: 0o100444, uid: 1000, dev: 1, ino: 2, ...overrides };
}

function lockIO(overrides = {}) {
  return {
    constants: fs.constants,
    openSync: vi
      .fn()
      .mockImplementationOnce(() => {
        throw Object.assign(new Error("absent"), { code: "ENOENT" });
      })
      .mockReturnValue(8),
    fchmodSync: vi.fn(),
    fstatSync: vi.fn(() => stat()),
    lstatSync: vi.fn(() => stat()),
    closeSync: vi.fn(),
    ...overrides,
  };
}

function queries({ containers = "", networks = "", endpoints = "{}", id = "daemon-one" } = {}) {
  return vi.fn(args => {
    if (args[0] === "info") return id;
    if (args[0] === "container") return containers;
    if (args[1] === "ls") return networks;
    if (args[1] === "inspect") return endpoints;
    throw new Error("Unexpected query");
  });
}

describe("AWF shared-resource compatibility guard", () => {
  it("uses a stable daemon-specific lock outside invocation TMPDIR", () => {
    expect(lockPath("daemon-one")).toMatch(/^\/tmp\/gh-aw-awf-[a-f0-9]{64}\.lock$/);
    expect(lockPath("daemon-one")).toBe(lockPath("daemon-one"));
    expect(lockPath("daemon-two")).not.toBe(lockPath("daemon-one"));
  });

  it("creates an exclusive no-follow read-only lock and explicitly fixes only new permissions", () => {
    const io = lockIO();
    expect(openLock("/coordination.lock", io, 1000)).toBe(8);
    expect(io.openSync).toHaveBeenCalledWith("/coordination.lock", fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK | fs.constants.O_CREAT | fs.constants.O_EXCL, 0o444);
    expect(io.fchmodSync).toHaveBeenCalledExactlyOnceWith(8, 0o444);
    expect(io.closeSync).not.toHaveBeenCalled();
  });

  it("allows a safe cross-user existing lock without chmod or unlink", () => {
    const io = lockIO({
      openSync: vi.fn().mockReturnValue(8),
      fstatSync: vi.fn(() => stat({ uid: 0 })),
      lstatSync: vi.fn(() => stat({ uid: 0 })),
    });
    expect(openLock("/coordination.lock", io, 1000)).toBe(8);
    expect(io.openSync).toHaveBeenLastCalledWith("/coordination.lock", fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    expect(io.fchmodSync).not.toHaveBeenCalled();
  });

  it("retries a competing creation without chmod of the winning lock", () => {
    const io = lockIO({
      openSync: vi
        .fn()
        .mockImplementationOnce(() => {
          throw Object.assign(new Error("absent"), { code: "ENOENT" });
        })
        .mockImplementationOnce(() => {
          throw Object.assign(new Error("exists"), { code: "EEXIST" });
        })
        .mockReturnValue(8),
    });
    expect(openLock("/coordination.lock", io, 1000)).toBe(8);
    expect(io.openSync).toHaveBeenCalledTimes(3);
    expect(io.fchmodSync).not.toHaveBeenCalled();
  });

  it("refuses unexpected ownership on a newly created lock", () => {
    const io = lockIO({ fstatSync: vi.fn(() => stat({ uid: 0 })) });
    expect(() => openLock("/coordination.lock", io, 1000)).toThrow("refusing to run");
    expect(io.closeSync).toHaveBeenCalledWith(8);
    expect(io.fchmodSync).not.toHaveBeenCalled();
  });

  it.each([{ isFile: () => false }, { nlink: 2 }, { size: 1 }, { mode: 0o100446 }, { mode: 0o100464 }])("rejects unsafe lock metadata %j", overrides => {
    expect(() => assertLockStat(stat(overrides))).toThrow("unsafe");
  });

  it("rejects lock inode replacement", () => {
    const io = lockIO({ lstatSync: vi.fn(() => stat({ ino: 3 })) });
    expect(() => assertLockIdentity(8, "/coordination.lock", io)).toThrow("changed");
  });

  it("refuses a symlink rather than following or deleting it", () => {
    const io = lockIO({
      openSync: vi.fn(() => {
        throw Object.assign(new Error("symlink"), { code: "ELOOP" });
      }),
    });
    expect(() => openLock("/coordination.lock", io, 1000)).toThrow("refusing to run");
    expect(io.openSync).toHaveBeenCalledTimes(1);
    expect(io.fchmodSync).not.toHaveBeenCalled();
  });

  it.each([...AWF_CONTAINERS])("refuses existing %s without deleting anything", name => {
    const query = queries({ containers: `unrelated\n${name}` });
    expect(() => preflight("daemon-one", query)).toThrow("Existing AWF containers");
    expect(query.mock.calls.every(([args]) => !args.some(arg => ["rm", "stop", "kill", "prune"].includes(arg)))).toBe(true);
  });

  it("allows unrelated containers and an absent or empty persistent AWF network", () => {
    expect(() => preflight("daemon-one", queries({ containers: "another-job" }))).not.toThrow();
    expect(() => preflight("daemon-one", queries({ networks: "bridge\nawf-net", endpoints: "{}" }))).not.toThrow();
  });

  it.each(['{"container-id":{"Name":"other-job"}}', "null", "[]", "invalid"])("refuses occupied or invalid network inspection: %s", endpoints => {
    expect(() => preflight("daemon-one", queries({ networks: "awf-net", endpoints }))).toThrow("refusing to run");
  });

  it("fails closed on daemon changes and any inspection failure", () => {
    expect(() => preflight("daemon-one", queries({ id: "daemon-two" }))).toThrow("daemon changed");
    expect(() => daemonIdentity(() => "")).toThrow("identity is unavailable");
    expect(() => dockerQuery(["info"], () => ({ status: 1, stderr: "secret", stdout: "" }))).toThrow("Docker inspection failed");
    expect(() => dockerQuery(["info"], () => ({ status: null, error: new Error("secret") }))).toThrow("Docker inspection failed");
    expect(() =>
      preflight("daemon-one", () => {
        throw new Error("failed inspection");
      })
    ).toThrow("failed inspection");
  });

  it("pins the endpoint and configuration across sudo HOME/context changes", () => {
    const query = vi.fn(() =>
      JSON.stringify({
        Name: "runner-context",
        Endpoints: { docker: { Host: "unix:///var/run/docker.sock", SkipTLSVerify: false } },
        TLSMaterial: {},
      })
    );
    const env = connectionEnvironment(query, { DOCKER_CONTEXT: "runner-context", CONFIG_TOKEN: "preserved" }, "/home/runner");
    expect(env.DOCKER_HOST).toBe("unix:///var/run/docker.sock");
    expect(env.DOCKER_CONFIG).toBe("/home/runner/.docker");
    expect(env.DOCKER_CONTEXT).toBeUndefined();
    expect(env.CONFIG_TOKEN).toBe("preserved");
  });

  it("preserves explicit TLS configuration using an absolute certificate path", () => {
    const query = () => JSON.stringify({ Name: "default", Endpoints: { docker: { Host: "tcp://daemon:2376", SkipTLSVerify: false } }, TLSMaterial: {} });
    const env = connectionEnvironment(query, { DOCKER_CONFIG: "/runner/docker", DOCKER_TLS_VERIFY: "1" }, "/home/runner");
    expect(env.DOCKER_HOST).toBe("tcp://daemon:2376");
    expect(env.DOCKER_TLS_VERIFY).toBe("1");
    expect(env.DOCKER_CERT_PATH).toBe("/runner/docker");
  });

  it("honors an explicit DOCKER_HOST instead of the stored default-context endpoint", () => {
    const query = () => JSON.stringify({ Name: "default", Endpoints: { docker: { Host: "unix:///var/run/docker.sock", SkipTLSVerify: false } }, TLSMaterial: {} });
    const env = connectionEnvironment(query, { DOCKER_HOST: "tcp://dind:2375" }, "/home/runner");
    expect(env.DOCKER_HOST).toBe("tcp://dind:2375");
    expect(env.DOCKER_CONTEXT).toBeUndefined();
  });

  it.each(["default", "runner-context"])("gives explicit DOCKER_CONTEXT=%s precedence over DOCKER_HOST", name => {
    const query = () => JSON.stringify({ Name: name, Endpoints: { docker: { Host: "unix:///selected-context/docker.sock", SkipTLSVerify: false } }, TLSMaterial: {} });
    const env = connectionEnvironment(query, { DOCKER_CONTEXT: name, DOCKER_HOST: "tcp://ignored-daemon:2375" }, "/home/runner");
    expect(env.DOCKER_HOST).toBe("unix:///selected-context/docker.sock");
    expect(env.DOCKER_CONTEXT).toBeUndefined();
  });

  it("validates the effective explicit host rather than the stored endpoint scheme", () => {
    const context = { Name: "default", Endpoints: { docker: { Host: "unix:///var/run/docker.sock", SkipTLSVerify: false } }, TLSMaterial: {} };
    expect(() => connectionEnvironment(() => JSON.stringify(context), { DOCKER_HOST: "ssh://unsupported-daemon" }, "/home/runner")).toThrow("refusing to run");
    context.Endpoints.docker.Host = "ssh://ignored-context";
    expect(connectionEnvironment(() => JSON.stringify(context), { DOCKER_HOST: "tcp://dind:2375" }, "/home/runner").DOCKER_HOST).toBe("tcp://dind:2375");
  });

  it.each([{ Host: "ssh://daemon", SkipTLSVerify: false }, { Host: "tcp://daemon:2376", SkipTLSVerify: true }, { Host: "unix:///var/run/docker.sock" }])("refuses endpoint configurations it cannot faithfully bind: %j", endpoint => {
    const query = () => JSON.stringify({ Name: "remote", Endpoints: { docker: endpoint }, TLSMaterial: {} });
    expect(() => connectionEnvironment(query, {}, "/home/runner")).toThrow("refusing to run");
  });

  it("refuses context-managed TLS instead of dropping credentials", () => {
    const query = () =>
      JSON.stringify({
        Name: "remote",
        Endpoints: { docker: { Host: "tcp://daemon:2376", SkipTLSVerify: false } },
        TLSMaterial: { docker: ["ca.pem", "cert.pem", "key.pem"] },
      });
    expect(() => connectionEnvironment(query, {}, "/home/runner")).toThrow("context-managed TLS");
  });

  it.each([
    [0, null, 0],
    [27, null, 27],
    [null, "SIGTERM", 143],
    [null, "SIGINT", 130],
    [null, null, 1],
  ])("preserves exit/signal status %s %s", (code, signal, expected) => {
    expect(exitStatus(code, signal)).toBe(expected);
  });

  it("forwards termination while keeping supervision alive until child close", async () => {
    const child = new EventEmitter();
    child.pid = 1234;
    const signals = new EventEmitter();
    const kill = vi.fn();
    const launcher = vi.fn(() => child);
    let finished = false;
    const result = supervise("awf", ["sensitive-argument"], 8, launcher, signals, kill).then(status => {
      finished = true;
      return status;
    });
    signals.emit("SIGTERM");
    await Promise.resolve();
    expect(finished).toBe(false);
    expect(kill).toHaveBeenCalledExactlyOnceWith(-1234, "SIGTERM");
    expect(launcher).toHaveBeenCalledExactlyOnceWith("awf", ["sensitive-argument"], { stdio: ["inherit", "inherit", "inherit", 8], detached: true });
    child.emit("close", 0, null);
    expect(await result).toBe(143);
    expect(signals.listenerCount("SIGTERM")).toBe(0);
  });

  it("reports spawn failure without echoing command arguments", async () => {
    const child = new EventEmitter();
    const result = supervise("missing", ["secret"], 8, () => child, new EventEmitter(), vi.fn());
    child.emit("error", new Error("secret"));
    expect(await result).toBe(1);
  });

  it("serializes real subprocesses through an inherited read-only descriptor without Docker", async () => {
    if (process.platform !== "linux") return;
    if (!process.env.TMPDIR || !path.isAbsolute(process.env.TMPDIR)) throw new Error("An absolute TMPDIR is required for AWF lock fixtures");
    const dir = fs.mkdtempSync(path.join(process.env.TMPDIR, "awf-lock-test-"));
    const file = path.join(dir, "coordination.lock");
    const firstFd = openLock(file);
    const secondFd = openLock(file);
    const children = [];
    try {
      const start = () => {
        const child = spawn("bash", ["-c", LOCK_SCRIPT, "awf-lock-test", process.execPath, "-e", 'process.stdout.write("locked\\n"); process.stdin.resume(); process.stdin.once("data", () => process.exit(0))'], {
          stdio: ["pipe", "pipe", "pipe", children.length ? secondFd : firstFd],
        });
        children.push(child);
        return child;
      };
      const first = start();
      await new Promise((resolve, reject) => {
        first.once("error", reject);
        first.once("exit", () => reject(new Error("first lock holder exited before acquisition")));
        first.stdout.once("data", resolve);
      });
      const second = start();
      let acquiredSecond = false;
      second.stdout.on("data", () => {
        acquiredSecond = true;
      });
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(acquiredSecond).toBe(false);
      // The parent descriptor also owns the flock: close it only after the
      // supervised command exits, as production does.
      const firstExited = new Promise(resolve => first.once("close", resolve));
      first.stdin.end("done\n");
      await firstExited;
      expect(acquiredSecond).toBe(false);
      fs.closeSync(firstFd);
      await new Promise((resolve, reject) => {
        if (acquiredSecond) return resolve();
        second.once("error", reject);
        second.stdout.once("data", resolve);
      });
      expect(acquiredSecond).toBe(true);
      const secondExited = new Promise(resolve => second.once("close", resolve));
      second.stdin.end("done\n");
      await secondExited;
      expect(fs.statSync(file).mode & 0o777).toBe(0o444);
    } finally {
      for (const child of children) {
        if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
      }
      try {
        fs.closeSync(firstFd);
      } catch {
        // Already closed after the first command exited.
      }
      fs.closeSync(secondFd);
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});
