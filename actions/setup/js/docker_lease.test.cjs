import { afterEach, beforeEach, describe, expect, it } from "vitest";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { LEASE_NAME, OWNER_LABEL, invocationOwner, acquire, release, assertOwner, assertServiceOwner, inspectLease, sharedResources, main } = require("./docker_lease.cjs");

describe("shared Docker daemon lease", () => {
  let root, env, lease, calls, resources, run;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-lease-test-"));
    env = { GITHUB_ENV: path.join(root, "env"), GITHUB_STATE: path.join(root, "state"), GITHUB_RUN_ID: "123", GITHUB_RUN_ATTEMPT: "1", GITHUB_JOB: "agent" };
    lease = null;
    calls = [];
    resources = { container: "", network: "", volume: "" };
    run = (args, options) => {
      calls.push(args);
      if (args[0] === "info") return "x86_64";
      if (args[0] === "image" && args[1] === "import") {
        expect(options.input.subarray(257, 262).toString()).toBe("ustar");
        expect(options.input.subarray(512, 516).toString("hex")).toBe("7f454c46");
        return "sha256:test";
      }
      if (args[0] === "image") return "";
      if (args[1] === "ls") {
        if (args.includes("--filter")) return lease ? lease.Id : "";
        return resources[args[0]];
      }
      if (args[1] === "inspect") return JSON.stringify([lease]);
      if (args[1] === "create") {
        if (lease) throw new Error("name conflict");
        const owner = args[args.indexOf("--label") + 1].split("=")[1];
        lease = { Id: "container-id", Image: "sha256:test", State: { Running: false }, Config: { Labels: { [OWNER_LABEL]: owner } } };
        return lease.Id;
      }
      if (args[1] === "start") {
        lease.State.Running = true;
        return "";
      }
      if (args[1] === "rm") {
        expect(args.at(-1)).toBe(lease.Id);
        lease = null;
        return "";
      }
      throw new Error(`Unexpected Docker call: ${args}`);
    };
  });
  afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

  it("persists unique invocation ownership before acquisition and removes only immutable owned IDs", async () => {
    const owner = await acquire({ env, run });
    expect(fs.readFileSync(env.GITHUB_ENV, "utf8")).toBe(`GH_AW_DOCKER_OWNER=${owner}\nGH_AW_DOCKER_READY=${owner}\n`);
    expect(fs.readFileSync(env.GITHUB_STATE, "utf8")).toBe(`GH_AW_DOCKER_OWNER=${owner}\nGH_AW_DOCKER_READY=${owner}\n`);
    expect(assertOwner(owner, run).Id).toBe("container-id");
    release(owner, run);
    expect(lease).toBeNull();
    expect(calls).toContainEqual(["container", "rm", "--force", "container-id"]);
    expect(calls.some(args => args[1] === "create" && args.includes("--restart") && args.includes("unless-stopped") && args.at(-1) === "/gh-aw-lease")).toBe(true);
    expect(calls).toContainEqual(["container", "start", LEASE_NAME]);
    expect(calls).toContainEqual(["image", "rm", "sha256:test"]);
    expect(calls.some(args => args.includes("/never-executed"))).toBe(false);
  });

  it("distinguishes simultaneous matrix copies, retries, and separate runs", () => {
    const owners = [env, env, { ...env, GITHUB_RUN_ATTEMPT: "2" }, { ...env, GITHUB_RUN_ID: "456" }].map(invocationOwner);
    expect(new Set(owners).size).toBe(4);
    expect(owners.every(owner => /^[a-f0-9]{64}$/.test(owner))).toBe(true);
  });

  it("queues a second invocation until its predecessor has finished cleanup", async () => {
    const first = await acquire({ env, run });
    let waited = false;
    const second = await acquire({
      env,
      run,
      pause: async () => {
        waited = true;
        expect(lease.Config.Labels[OWNER_LABEL]).toBe(first);
        release(first, run);
      },
    });
    expect(waited).toBe(true);
    expect(second).not.toBe(first);
    expect(() => assertOwner(first, run)).toThrow("not owned");
    release(first, run);
    expect(assertOwner(second, run)).toEqual(lease);
    release(second, run);
  });

  it("does not steal an interrupted or stale lease, even for the same run and attempt", async () => {
    const first = await acquire({ env, run });
    await expect(acquire({ env, run, timeoutMs: 0 })).rejects.toThrow("Timed out");
    expect(assertOwner(first, run)).toEqual(lease);
    expect(calls.filter(args => args[0] === "container" && args[1] === "rm")).toEqual([]);
  });

  it("can remove its own stopped lease after startup failed", async () => {
    const owner = await acquire({ env, run });
    lease.State.Running = false;
    release(owner, run);
    expect(lease).toBeNull();
  });

  it.each(["container", "network", "volume"])("refuses unowned stale %s resources and retains the lease", async kind => {
    resources[kind] = JSON.stringify({ Names: "awf-stale", Name: "awf-stale" });
    await expect(acquire({ env, run })).rejects.toThrow("resources remain");
    const owner = lease.Config.Labels[OWNER_LABEL];
    expect(() => release(owner, run)).toThrow("Lease retained");
    expect(lease).not.toBeNull();
    expect(calls.filter(args => args[1] === "rm")).toEqual([]);
    expect(fs.readFileSync(env.GITHUB_ENV, "utf8")).not.toContain("GH_AW_DOCKER_READY");
    await expect(main("assert", { RUNNER_ENVIRONMENT: "self-hosted", GH_AW_DOCKER_OWNER: owner })).rejects.toThrow("not owned and validated");
  });

  it("retains the lease if a failed job leaves a running sidecar", async () => {
    const owner = await acquire({ env, run });
    resources.container = JSON.stringify({ Names: "awmg-mcpg" });
    expect(() => release(owner, run)).toThrow("resources remain");
    resources.container = "";
    release(owner, run);
    expect(lease).toBeNull();
  });

  it("classifies an active MCP Scripts host listener as a shared resource", () => {
    const pidFile = path.join(root, "server.pid");
    fs.writeFileSync(pidFile, `${"a".repeat(64)}\t${process.pid}\n`, { mode: 0o600 });
    expect(sharedResources(() => "", pidFile)).toContain(`host-listener:mcp-scripts-server:${process.pid}`);
  });

  it("propagates daemon and creation failures rather than treating them as contention", async () => {
    const broken = args => {
      if (args[1] === "create") throw new Error("daemon unavailable");
      return run(args);
    };
    // Keep the import buffer check in the normal mock.
    await expect(acquire({ env, run: (args, options) => (args[1] === "import" ? run(args, options) : broken(args)) })).rejects.toThrow("daemon unavailable");
    expect(calls).toContainEqual(["image", "rm", "sha256:test"]);
    expect(lease).toBeNull();
  });

  it("handles a lease disappearing between discovery and inspection", () => {
    let lists = 0;
    expect(
      inspectLease(args => {
        if (args[1] === "ls") return lists++ === 0 ? "old-id" : "";
        throw new Error("No such container");
      })
    ).toBeNull();
  });

  it("builds a valid no-network pause executable for supported daemon architectures", () => {
    for (const architecture of ["x86_64", "aarch64"]) {
      const executable = require("./docker_lease.cjs").pauseExecutable(architecture);
      expect(executable.subarray(0, 4).toString("hex")).toBe("7f454c46");
      const archive = require("./docker_lease.cjs").tarArchive("gh-aw-lease", executable, 0o755);
      expect(archive.subarray(257, 262).toString()).toBe("ustar");
      expect(archive.subarray(512, 516)).toEqual(executable.subarray(0, 4));
    }
    expect(() => require("./docker_lease.cjs").pauseExecutable("riscv64")).toThrow("Unsupported Docker daemon architecture");
  });

  it("adopts only its own container after an ambiguous create timeout", async () => {
    const owner = await acquire({
      env,
      run: (args, options) => {
        const result = run(args, options);
        if (args[1] === "create") throw new Error("client timeout");
        return result;
      },
    });
    expect(assertOwner(owner, run)).toEqual(lease);
    release(owner, run);
  });

  it("refuses replacement or removal of a foreign service despite holding the daemon lease", () => {
    const owner = invocationOwner(env);
    const serviceRun = args => (args[1] === "ls" ? "service-id" : JSON.stringify([{ Config: { Labels: { [OWNER_LABEL]: "foreign" } } }]));
    expect(() => assertServiceOwner(owner, "awmg-mcpg", serviceRun)).toThrow("does not match");
    expect(() => assertServiceOwner(owner, "other-service", serviceRun)).toThrow("Unknown");
  });

  it("rejects cleanup without ownership, before Docker stop/rm or listener shutdown", () => {
    const scripts = ["stop_cli_proxy.sh", "stop_difc_proxy.sh", "stop_mcp_gateway.sh"];
    const bin = path.join(root, "bin");
    fs.mkdirSync(bin);
    fs.writeFileSync(path.join(bin, "docker"), "#!/bin/sh\nexit 0\n", { mode: 0o700 });
    for (const script of scripts) {
      const result = spawnSync("bash", [path.resolve(__dirname, "../sh", script), "12345"], {
        env: { ...process.env, RUNNER_ENVIRONMENT: "self-hosted", GH_AW_DOCKER_OWNER: "", PATH: `${bin}:${process.env.PATH}` },
        encoding: "utf8",
      });
      expect(result.status, result.stderr).not.toBe(0);
      expect(result.stderr).toContain("not owned");
      expect(result.stdout).not.toContain("Stopping");
    }
  });

  it("cleans owned DIFC host state even when Docker teardown inspection fails", () => {
    const bin = path.join(root, "host-cleanup-bin");
    const actions = path.join(root, "runner-temp", "gh-aw", "actions");
    const owner = "a".repeat(64);
    const stateFile = path.join(actions, "proxy-host-state");
    fs.mkdirSync(bin);
    fs.mkdirSync(actions, { recursive: true });
    fs.writeFileSync(path.join(bin, "node"), "#!/bin/sh\nexit 0\n", { mode: 0o700 });
    fs.writeFileSync(path.join(bin, "docker"), "#!/bin/sh\nexit 1\n", { mode: 0o700 });
    fs.writeFileSync(stateFile, `${owner}\t\tfalse\n`, { mode: 0o600 });

    const result = spawnSync("bash", [path.resolve(__dirname, "../sh/stop_difc_proxy.sh")], {
      env: {
        ...process.env,
        RUNNER_ENVIRONMENT: "self-hosted",
        RUNNER_TEMP: path.join(root, "runner-temp"),
        GH_AW_DOCKER_OWNER: owner,
        GH_AW_NODE: path.join(bin, "node"),
        PATH: `${bin}:${process.env.PATH}`,
      },
      encoding: "utf8",
    });

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("DIFC proxy teardown was incomplete");
    expect(fs.existsSync(stateFile)).toBe(false);
  });

  it("acquires before setup's first shared filesystem deletion and releases last", () => {
    const setup = fs.readFileSync(path.resolve(__dirname, "../setup.sh"), "utf8");
    const acquireIndex = setup.indexOf('docker_lease.cjs" acquire');
    const cleanupIndex = setup.indexOf("sudo -n rm -rf /tmp/gh-aw");
    expect(acquireIndex).toBeGreaterThanOrEqual(0);
    expect(cleanupIndex).toBeGreaterThanOrEqual(0);
    expect(acquireIndex).toBeLessThan(cleanupIndex);
    const clean = fs.readFileSync(path.resolve(__dirname, "../clean.sh"), "utf8");
    const stopListenerIndex = clean.indexOf("stop_mcp_scripts_server.sh");
    const cleanupRuntimeIndex = clean.indexOf('bash "${DESTINATION}/cleanup_runtime_tmp.sh"');
    const releaseIndex = clean.indexOf('docker_lease.cjs" release');
    expect(stopListenerIndex).toBeGreaterThanOrEqual(0);
    expect(cleanupRuntimeIndex).toBeGreaterThanOrEqual(0);
    expect(releaseIndex).toBeGreaterThanOrEqual(0);
    expect(stopListenerIndex).toBeLessThan(cleanupRuntimeIndex);
    expect(releaseIndex).toBeGreaterThan(cleanupRuntimeIndex);
    const post = fs.readFileSync(path.resolve(__dirname, "../post.js"), "utf8");
    const validateCallIndex = post.indexOf("const leaseValidated = !selfHosted || validateDockerLease();");
    const conclusionIndex = post.indexOf('require(path.join(__dirname, "js", "action_conclusion_otlp.cjs"))');
    const stopListenerCallIndex = post.indexOf("!stopMcpScriptsServer()");
    const releaseCallIndex = post.indexOf('[path.join(__dirname, "js", "docker_lease.cjs"), "release"]');
    expect(validateCallIndex).toBeGreaterThanOrEqual(0);
    expect(validateCallIndex).toBeLessThan(conclusionIndex);
    expect(stopListenerCallIndex).toBeGreaterThanOrEqual(0);
    expect(stopListenerCallIndex).toBeLessThan(releaseCallIndex);
    expect(setup).toContain('RUNNER_ENVIRONMENT:-}" == "self-hosted"');
    expect(LEASE_NAME).toBe("gh-aw-daemon-lease");
  });
});
