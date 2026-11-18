const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const crypto = require("node:crypto");
const childProcess = require("node:child_process");

const source = fs.readFileSync(path.join(__dirname, "runtime_isolation.cjs"), "utf8");

// Virtualize only Linux runtime paths; all physical fixtures stay in this project.
function fixture(realDocker = false, dockerEnvironment) {
  const directory = fs.mkdtempSync(path.join(__dirname, ".runtime-isolation-test-"));
  const temporary = path.join(directory, "shared-temp");
  fs.mkdirSync(temporary);
  const mutations = [];
  const map = value => (typeof value === "string" && value.startsWith("/tmp/gh-aw") ? path.join(temporary, value.slice("/tmp/".length)) : value);
  const virtualFS = new Proxy(fs, {
    get(target, name) {
      if (["lstatSync", "readFileSync", "writeFileSync", "chmodSync", "appendFileSync", "rmSync", "realpathSync", "readdirSync"].includes(name)) {
        return (file, ...args) => {
          if (["writeFileSync", "rmSync", "chmodSync"].includes(name)) mutations.push([name, file]);
          return target[name](map(file), ...args);
        };
      }
      if (name === "mkdtempSync")
        return prefix => {
          const result = fs.mkdtempSync(map(prefix));
          return prefix.startsWith("/tmp/") ? `/tmp/${path.basename(result)}` : result;
        };
      return target[name];
    },
  });
  const dockerCalls = [];
  const module = { exports: {} };
  vm.runInNewContext(source, {
    module,
    require(name) {
      if (name === "node:fs") return virtualFS;
      if (name === "node:path") return path;
      if (name === "node:crypto") return crypto;
      if (name === "node:child_process")
        return {
          spawnSync(command, args, options) {
            dockerCalls.push([command, args]);
            return realDocker ? childProcess.spawnSync(command, args, { ...options, env: dockerEnvironment }) : { status: 0 };
          },
        };
      throw new Error(`Unexpected dependency ${name}`);
    },
    process,
    console: { ...console, warn: () => {} },
  });
  const environment = () => {
    const runner = path.join(directory, "runner");
    fs.mkdirSync(runner, { recursive: true });
    const id = crypto.randomBytes(5).toString("hex");
    return { RUNNER_TEMP: runner, GITHUB_ENV: path.join(directory, `env-${id}`), GITHUB_STATE: path.join(directory, `state-${id}`) };
  };
  return { ...module.exports, directory, temporary, map, mutations, dockerCalls, environment, dispose: () => fs.rmSync(directory, { recursive: true, force: true }) };
}

describe("private runtime ownership", () => {
  let context;
  afterEach(() => context?.dispose());

  it("allocates separate roots and state, reuses only explicitly owned roots, and isolates cleanup", async () => {
    context = fixture();
    const first = context.environment();
    const second = context.environment();
    await Promise.all([Promise.resolve().then(() => context.allocate(first)), Promise.resolve().then(() => context.allocate(second))]);
    expect(first.GH_AW_RUNTIME_ROOT).not.toBe(second.GH_AW_RUNTIME_ROOT);
    expect(first.GH_AW_SETUP_ROOT).not.toBe(second.GH_AW_SETUP_ROOT);
    expect(fs.statSync(context.map(first.GH_AW_RUNTIME_ROOT)).mode & 0o777).toBe(0o700);
    expect(fs.statSync(first.GH_AW_SETUP_ROOT).mode & 0o777).toBe(0o700);
    expect(fs.readFileSync(first.GITHUB_STATE, "utf8")).toContain(`GH_AW_RUNTIME_ID=${first.GH_AW_RUNTIME_ID}`);
    expect(context.allocate(first).GH_AW_RUNTIME_ROOT).toBe(first.GH_AW_RUNTIME_ROOT);
    fs.writeFileSync(path.join(context.map(second.GH_AW_RUNTIME_ROOT), "marker"), "second");
    expect(context.cleanup(first)).toBe(true);
    expect(fs.readFileSync(path.join(context.map(second.GH_AW_RUNTIME_ROOT), "marker"), "utf8")).toBe("second");
    expect(context.dockerCalls[0][1].slice(2)).toEqual(["awmg-mcpg", "awmg-cli-proxy", "awmg-proxy"].map(name => `${name}-${first.GH_AW_RESOURCE_SUFFIX}`));
  });

  it("rejects shared paths, unsafe modes, wrong tokens, symlink roots, and unowned roots", () => {
    context = fixture();
    const env = context.environment();
    context.allocate(env);
    const root = context.map(env.GH_AW_RUNTIME_ROOT);
    expect(() => context.validate("/tmp/gh-aw", env.GH_AW_RUNTIME_TOKEN)).toThrow();
    expect(() => context.validate(env.GH_AW_RUNTIME_ROOT, "0".repeat(64))).toThrow();
    fs.chmodSync(root, 0o755);
    expect(context.cleanup(env)).toBe(false);
    fs.chmodSync(root, 0o700);
    fs.renameSync(root, `${root}-original`);
    fs.symlinkSync(`${root}-original`, root);
    expect(context.cleanup(env)).toBe(false);
    expect(fs.existsSync(`${root}-original`)).toBe(true);
    expect(context.dockerCalls).toEqual([]);
  });

  it("uses saved action ownership instead of later mutable environment", () => {
    context = fixture();
    const first = context.environment();
    const second = context.environment();
    context.allocate(first);
    context.allocate(second);
    const saved = Object.fromEntries(
      Object.entries(first)
        .filter(([name]) => name.startsWith("GH_AW_"))
        .map(([name, value]) => [`STATE_${name}`, value])
    );
    context.cleanup({ ...second, ...saved });
    expect(fs.existsSync(context.map(first.GH_AW_RUNTIME_ROOT))).toBe(false);
    expect(fs.existsSync(context.map(second.GH_AW_RUNTIME_ROOT))).toBe(true);
    expect(fs.existsSync(second.GH_AW_SETUP_ROOT)).toBe(true);
  });

  it("does not adopt orphaned previous invocations or follow cleanup symlinks", () => {
    context = fixture();
    const orphan = context.environment();
    const next = context.environment();
    context.allocate(orphan);
    context.allocate(next);
    fs.writeFileSync(path.join(context.map(orphan.GH_AW_RUNTIME_ROOT), "keep"), "orphan");
    fs.symlinkSync(context.map(orphan.GH_AW_RUNTIME_ROOT), path.join(context.map(next.GH_AW_RUNTIME_ROOT), "outside"));
    context.cleanup(next);
    expect(fs.readFileSync(path.join(context.map(orphan.GH_AW_RUNTIME_ROOT), "keep"), "utf8")).toBe("orphan");
  });

  it("relocates nested dependencies, prompts, runner-temp aliases, and Docker names without collapsing RO/RW trees", () => {
    context = fixture();
    const env = context.environment();
    context.allocate(env);
    const directory = path.join(env.GH_AW_SETUP_ROOT, "actions");
    fs.mkdirSync(path.join(directory, "nested"), { recursive: true });
    const input = [
      'const root = "/tmp/gh-aw/mcp-logs";',
      'const stage = path.join(process.env.RUNNER_TEMP || os.tmpdir(), "gh-aw", "actions");',
      'const nested = path.join(runnerTemp, "gh-aw/mcp-config/gateway.json");',
      'const redact = path.join("/tmp", "gh-aw/mcp-config/pi-agent.json");',
      'const agents = path.join(os.tmpdir(), "gh-aw");',
      'const temporary = path.join(os.tmpdir(), "gh-aw-mcp-gateway-");',
      "const config = `${process.env.RUNNER_TEMP}/gh-aw/prompts/example.md`;",
      "docker rm -f awmg-mcpg awmg-cli-proxy awmg-proxy",
    ].join("\n");
    const file = path.join(directory, "nested", "dependency.cjs");
    fs.writeFileSync(file, input);
    context.relocate(directory, env.GH_AW_RUNTIME_ROOT, env.GH_AW_RUNTIME_TOKEN, env.GH_AW_RESOURCE_SUFFIX, env.GH_AW_SETUP_ROOT);
    const output = fs.readFileSync(file, "utf8");
    expect(output).toContain(`${env.GH_AW_RUNTIME_ROOT}/mcp-logs`);
    expect(output).toContain(`path.join("${env.GH_AW_SETUP_ROOT}", "actions")`);
    expect(output).toContain(`path.join("${env.GH_AW_SETUP_ROOT}/mcp-config/gateway.json")`);
    expect(output).toContain(`path.join("${env.GH_AW_RUNTIME_ROOT}/mcp-config/pi-agent.json")`);
    expect(output).toContain(`const agents = path.join("${env.GH_AW_RUNTIME_ROOT}")`);
    expect(output).toContain('path.join(os.tmpdir(), "gh-aw-mcp-gateway-")');
    expect(output).toContain("${process.env.GH_AW_SETUP_ROOT}/prompts/example.md");
    expect(output).toContain(`awmg-cli-proxy-${env.GH_AW_RESOURCE_SUFFIX}`);
    expect(context.relocateText(output, env.GH_AW_RUNTIME_ROOT, env.GH_AW_RESOURCE_SUFFIX, env.GH_AW_SETUP_ROOT)).toBe(output);
    expect(() => context.validateDestination(path.join(env.GH_AW_SETUP_ROOT, "..", "outside"), env.GH_AW_SETUP_ROOT)).toThrow();
  });

  it("keeps arbitrary setup-root characters out of executable shell/template source", () => {
    context = fixture();
    const root = "/tmp/gh-aw-run.Test123";
    const setup = '/runner/a "quote" `backtick` $(touch unsafe) ${value}/gh-aw-setup.Test';
    const shell = context.relocateText('printf "%s" "${RUNNER_TEMP}/gh-aw/actions"', root, "Test123", setup, "asset.sh");
    expect(shell).toBe('printf "%s" "${GH_AW_SETUP_ROOT}/actions"');
    const js = context.relocateText("const file = `${process.env.RUNNER_TEMP}/gh-aw/actions`;", root, "Test123", setup);
    expect(js).toBe("const file = `${process.env.GH_AW_SETUP_ROOT}/actions`;");
    expect(vm.runInNewContext(`${js} file`, { process: { env: { GH_AW_SETUP_ROOT: setup } } })).toBe(`${setup}/actions`);
  });

  it("requires staged modules to be regular files within the owned staging tree", () => {
    context = fixture();
    const env = context.environment();
    context.allocate(env);
    const root = env.GH_AW_SETUP_ROOT;
    const destination = path.join(root, "custom staging", "telemetry");
    const filename = "action_conclusion_otlp.cjs";
    const modulePath = path.join(destination, filename);
    fs.mkdirSync(destination, { recursive: true });
    fs.writeFileSync(modulePath, "");
    expect(context.stagedModulePath(destination, root, filename)).toBe(modulePath);
    expect(() => context.stagedModulePath(path.dirname(root), root, filename)).toThrow();
    fs.symlinkSync(destination, path.join(root, "redirect"));
    expect(() => context.stagedModulePath(path.join(root, "redirect"), root, filename)).toThrow();
    fs.renameSync(modulePath, `${modulePath}.original`);
    fs.symlinkSync(`${modulePath}.original`, modulePath);
    expect(() => context.stagedModulePath(destination, root, filename)).toThrow();
  });

  it("rolls back newly allocated roots when ownership publication fails", () => {
    context = fixture();
    const env = context.environment();
    env.GITHUB_ENV = context.directory;
    expect(() => context.allocate(env)).toThrow();
    expect(fs.readdirSync(context.temporary)).toEqual([]);
    expect(fs.readdirSync(env.RUNNER_TEMP)).toEqual([]);
  });
});

describe("Windows WSL dispatch contract (mocked, not live WSL)", () => {
  it("uses the allocator's Ubuntu root user and translates only path variables", () => {
    const calls = [];
    const module = { exports: {} };
    vm.runInNewContext(fs.readFileSync(path.join(__dirname, "../wsl.js"), "utf8"), {
      module,
      process: {
        env: {
          GH_AW_SETUP_ROOT: "C:\\runner\\setup",
          STATE_GH_AW_SETUP_ROOT: "C:\\runner\\setup",
          STATE_GH_AW_SETUP_DESTINATION: "custom staging/telemetry",
          GH_AW_RUNTIME_ROOT: "/tmp/gh-aw-run.Test123",
          GH_AW_RUNTIME_TOKEN: "a".repeat(64),
          GITHUB_STATE: "C:\\runner\\state",
        },
      },
      require(name) {
        expect(name).toBe("node:child_process");
        return {
          spawnSync(command, args, options) {
            calls.push({ command, args, options });
            return { status: 0, stdout: "/mnt/c/action/index.js\n" };
          },
        };
      },
    });
    expect(module.exports.runInWSL("node", "C:\\action\\index.js").status).toBe(0);
    expect(calls[0].args.slice(0, 5)).toEqual(["--distribution", "Ubuntu", "--user", "root", "--exec"]);
    expect(calls[1].args).toEqual(["--distribution", "Ubuntu", "--user", "root", "--exec", "node", "/mnt/c/action/index.js"]);
    expect(calls[1].options.env.WSLENV).toContain("GH_AW_SETUP_ROOT/p");
    expect(calls[1].options.env.WSLENV).toContain("STATE_GH_AW_SETUP_ROOT/p");
    expect(calls[1].options.env.WSLENV).not.toContain("GH_AW_RUNTIME_ROOT/p");
    expect(calls[1].options.env.WSLENV.split(":")).toContain("STATE_GH_AW_SETUP_DESTINATION");
  });
});

describe("sample patch invocation scope", () => {
  it("uses the current runtime root while preserving standalone temporary paths", () => {
    const { resolveSamplePatchPath } = require("./apply_samples.cjs");
    expect(resolveSamplePatchPath(0, { GH_AW_RUNTIME_ROOT: "/tmp/gh-aw-run.First123" })).toBe("/tmp/gh-aw-run.First123/gh-aw-sample-1.patch");
    expect(resolveSamplePatchPath(0, { GH_AW_RUNTIME_ROOT: "/tmp/gh-aw-run.Second123" })).toBe("/tmp/gh-aw-run.Second123/gh-aw-sample-1.patch");
    expect(resolveSamplePatchPath(0, {})).toBe(path.join(require("node:os").tmpdir(), "gh-aw-sample-1.patch"));
  });
});

describe("shared Docker daemon sidecar isolation (not full AWF)", () => {
  // Explicit opt-in requires an immutable, independently verified local image ID.
  const enabled = process.env.GH_AW_TEST_LOCAL_DOCKER === "1";
  it.skipIf(!enabled)(
    "cleanup preserves another live invocation and stale root-owned legacy data",
    () => {
      const image = process.env.GH_AW_TEST_DOCKER_IMAGE;
      expect(image).toMatch(/^sha256:[a-f0-9]{64}$/);
      expect(process.env.DOCKER_HOST || "unix:///var/run/docker.sock").toBe("unix:///var/run/docker.sock");
      expect(process.env.DOCKER_CONTEXT || "default").toBe("default");
      expect(fs.statSync("/var/run/docker.sock").isSocket()).toBe(true);
      const dockerEnvironment = { PATH: process.env.PATH, DOCKER_HOST: "unix:///var/run/docker.sock", DOCKER_CONFIG: "" };
      const context = fixture(true, dockerEnvironment);
      dockerEnvironment.DOCKER_CONFIG = path.join(context.directory, "docker-config");
      fs.mkdirSync(dockerEnvironment.DOCKER_CONFIG);
      const first = context.environment();
      const second = context.environment();
      const names = [];
      const run = args => childProcess.execFileSync("docker", args, { stdio: "pipe", encoding: "utf8", env: dockerEnvironment }).trim();
      try {
        context.allocate(first);
        context.allocate(second);
        for (const env of [first, second]) {
          const name = `awmg-mcpg-${env.GH_AW_RESOURCE_SUFFIX}`;
          names.push(name);
          run(["run", "-d", "--pull=never", "--network", "none", "--name", name, "--entrypoint", "/bin/sh", image, "-c", "sleep 120"]);
          expect(run(["inspect", "--format", "{{.State.Running}}", name])).toBe("true");
        }
        run(["run", "--rm", "--pull=never", "--network", "none", "--user", "0:0", "-v", `${context.temporary}:/fixture`, "--entrypoint", "/bin/sh", image, "-c", "mkdir /fixture/gh-aw; echo legacy > /fixture/gh-aw/marker"]);
        expect(fs.statSync(path.join(context.temporary, "gh-aw")).uid).toBe(0);
        context.cleanup(first);
        expect(run(["inspect", "--format", "{{.State.Running}}", names[1]])).toBe("true");
        expect(fs.readFileSync(path.join(context.temporary, "gh-aw", "marker"), "utf8")).toBe("legacy\n");
        context.cleanup(second);
      } finally {
        childProcess.spawnSync("docker", ["rm", "-f", ...names], { stdio: "ignore", env: dockerEnvironment });
        childProcess.spawnSync("docker", ["run", "--rm", "--pull=never", "--network", "none", "--user", "0:0", "-v", `${context.temporary}:/fixture`, "--entrypoint", "/bin/sh", image, "-c", "rm -rf /fixture/gh-aw"], {
          stdio: "ignore",
          env: dockerEnvironment,
        });
        context.dispose();
      }
    },
    30000
  );
});
