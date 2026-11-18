const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");
const vm = require("node:vm");

describe("concurrent setup invocations sharing a runtime parent", () => {
  let directory;
  afterEach(() => {
    if (directory) fs.rmSync(directory, { recursive: true, force: true });
  });

  const stages = ["actions", "custom staging/telemetry"];
  it.each(stages)(
    "isolates staging in %s",
    async staging => {
      directory = fs.mkdtempSync(path.join(__dirname, ".setup-isolation-test-"));
      const temporary = path.join(directory, "shared");
      const runner = path.join(directory, "runner with spaces");
      const bin = path.join(directory, "bin");
      for (const item of [temporary, runner, bin]) fs.mkdirSync(item);
      fs.mkdirSync(path.join(temporary, "gh-aw"));
      fs.writeFileSync(path.join(temporary, "gh-aw", "legacy"), "never reclaim");
      const calls = path.join(directory, "docker-calls");
      fs.writeFileSync(path.join(bin, "docker"), '#!/bin/sh\nprintf "%s\\n" "$*" >> "$TEST_DOCKER_CALLS"\n', { mode: 0o700 });
      fs.writeFileSync(path.join(bin, "sudo"), '#!/bin/sh\necho "unexpected sudo" >&2\nexit 99\n', { mode: 0o700 });
      const preload = path.join(directory, "virtual-runtime.cjs");
      fs.writeFileSync(
        preload,
        `
const fs = require("node:fs");
const path = require("node:path");
const parent = process.env.GH_AW_TEST_VIRTUAL_TMP;
const map = file => typeof file === "string" && file.startsWith("/tmp/gh-aw-run.") ? path.join(parent, file.slice(5)) : file;
for (const name of ["lstatSync", "readFileSync", "writeFileSync", "chmodSync", "appendFileSync", "rmSync", "realpathSync", "readdirSync"]) {
  const original = fs[name];
  fs[name] = function(file, ...args) { return original.call(fs, map(file), ...args); };
}
const original = fs.mkdtempSync;
fs.mkdtempSync = function(prefix, ...args) {
  const result = original.call(fs, map(prefix), ...args);
  return prefix.startsWith("/tmp/gh-aw-run.") ? "/tmp/" + path.basename(result) : result;
};
`
      );
      const invoke = (script, env, args = []) =>
        new Promise(resolve => {
          const child = spawn(/\.(?:cjs|js)$/.test(script) ? process.execPath : "bash", [path.resolve(__dirname, "..", script), ...args], { env });
          let output = "";
          child.stdout.on("data", data => (output += data));
          child.stderr.on("data", data => (output += data));
          child.on("error", error => resolve({ status: -1, output: error.message }));
          child.on("close", status => resolve({ status, output }));
        });
      const environment = name => ({
        PATH: `${bin}:${process.env.PATH}`,
        LANG: "C.UTF-8",
        GITHUB_ACTIONS: "false",
        RUNNER_TEMP: runner,
        INPUT_DESTINATION: "",
        GITHUB_ENV: path.join(directory, `env-${name}`),
        GITHUB_STATE: path.join(directory, `state-${name}`),
        GITHUB_OUTPUT: path.join(directory, `output-${name}`),
        GH_AW_RUNTIME_ROOT: "",
        GH_AW_RUNTIME_TOKEN: "",
        GH_AW_RUNTIME_ID: "",
        GH_AW_SETUP_ROOT: "",
        GH_AW_SETUP_ID: "",
        GH_AW_RESOURCE_SUFFIX: "",
        GH_AW_SKIP_SETUP_OTLP: "1",
        GH_AW_TEST_VIRTUAL_TMP: temporary,
        GH_AW_RUNTIME_TMP_DIR: "",
        NODE_OPTIONS: `--require ${JSON.stringify(preload)}`,
        TEST_DOCKER_CALLS: calls,
        OTEL_EXPORTER_OTLP_ENDPOINT: "",
        GH_AW_OTLP_ENDPOINTS: "",
      });
      const readEnvironment = env =>
        Object.fromEntries(
          fs
            .readFileSync(env.GITHUB_ENV, "utf8")
            .trim()
            .split("\n")
            .map(line => {
              const index = line.indexOf("=");
              return [line.slice(0, index), line.slice(index + 1)];
            })
        );
      const first = environment("first");
      const second = environment("second");
      if (staging !== "actions") {
        const allocation = await invoke("js/runtime_isolation.cjs", first, ["allocate"]);
        expect(allocation.status, allocation.output).toBe(0);
        Object.assign(first, readEnvironment(first));
        first.INPUT_DESTINATION = path.join(first.GH_AW_SETUP_ROOT, staging);
      }
      const results = await Promise.all([invoke("index.js", first), invoke("setup.sh", second)]);
      for (const result of results) expect(result.status, result.output).toBe(0);
      expect(fs.readFileSync(first.GITHUB_OUTPUT, "utf8")).toMatch(/^trace-id=[0-9a-f]{32}$/m);
      Object.assign(first, readEnvironment(first));
      Object.assign(second, readEnvironment(second));
      expect(first.GH_AW_RUNTIME_ROOT).not.toBe(second.GH_AW_RUNTIME_ROOT);
      expect(first.GH_AW_SETUP_ROOT).not.toBe(second.GH_AW_SETUP_ROOT);
      for (const tree of [staging, "mcp-scripts", "safeoutputs"]) {
        for (const filename of fs.readdirSync(path.join(first.GH_AW_SETUP_ROOT, tree))) {
          if (filename.endsWith(".cjs")) {
            expect(() => new vm.Script(fs.readFileSync(path.join(first.GH_AW_SETUP_ROOT, tree, filename), "utf8"), { filename })).not.toThrow();
          }
        }
      }
      const resolveRoot = env => path.join(temporary, path.basename(env.GH_AW_RUNTIME_ROOT));
      const cache = path.join(resolveRoot(first), "cache-memory");
      fs.mkdirSync(cache);
      fs.writeFileSync(path.join(cache, "entry"), "cached data");
      fs.writeFileSync(path.join(resolveRoot(second), "sibling"), "second");
      const staged = fs.readFileSync(path.join(first.GH_AW_SETUP_ROOT, "safeoutputs", "safe_outputs_config.cjs"), "utf8");
      expect(staged).toContain("${process.env.GH_AW_SETUP_ROOT}/safeoutputs/config.json");
      expect(fs.readFileSync(path.join(first.GH_AW_SETUP_ROOT, staging, "start_mcp_gateway.cjs"), "utf8")).toContain(`awmg-mcpg-${first.GH_AW_RESOURCE_SUFFIX}`);
      const stagedRedaction = require(path.join(first.GH_AW_SETUP_ROOT, staging, "redact_secrets.cjs"));
      const sourceRedaction = require("./redact_secrets.cjs");
      expect(stagedRedaction.MCP_GATEWAY_CONFIG_PATHS).toContain(`${first.GH_AW_RUNTIME_ROOT}/mcp-config/gateway-output.json`);
      const synthetic = "credential=owned-fixture-secret";
      expect(stagedRedaction.redactArtifactContent(synthetic, ["owned-fixture-secret"], [])).toEqual(sourceRedaction.redactArtifactContent(synthetic, ["owned-fixture-secret"], []));
      const clean = await invoke("clean.sh", first);
      expect(clean.status, clean.output).toBe(0);
      expect(first.GH_AW_SETUP_ACTION_COMPLETE).toBe("true");
      expect(fs.readFileSync(path.join(cache, "entry"), "utf8")).toBe("cached data");
      expect(fs.existsSync(path.join(first.GH_AW_SETUP_ROOT, staging, "action_conclusion_otlp.cjs"))).toBe(true);
      // Cache post actions run before setup's post action (reverse registration).
      fs.copyFileSync(path.join(cache, "entry"), path.join(directory, "saved-cache"));
      const savedState = Object.fromEntries(
        fs
          .readFileSync(first.GITHUB_STATE, "utf8")
          .trim()
          .split("\n")
          .map(line => {
            const index = line.indexOf("=");
            return [`STATE_${line.slice(0, index)}`, line.slice(index + 1)];
          })
      );
      expect(savedState.STATE_GH_AW_SETUP_DESTINATION).toBe(staging);
      const post = await invoke("post.js", { ...first, ...savedState, INPUT_DESTINATION: path.join(first.GH_AW_SETUP_ROOT, "not-staged") });
      expect(post.status, post.output).toBe(0);
      expect(post.output).toContain("[otlp] GH_AW_OTLP_ENDPOINTS not set, skipping OTLP export");
      expect(fs.existsSync(resolveRoot(first))).toBe(false);
      expect(fs.readFileSync(path.join(directory, "saved-cache"), "utf8")).toBe("cached data");
      expect(fs.readFileSync(path.join(resolveRoot(second), "sibling"), "utf8")).toBe("second");
      const failure = environment("failure");
      failure.INPUT_DESTINATION = path.join(directory, "outside");
      expect((await invoke("setup.sh", failure)).status).not.toBe(0);
      Object.assign(failure, readEnvironment(failure));
      expect(fs.existsSync(resolveRoot(failure))).toBe(false);
      expect(fs.existsSync(failure.GH_AW_SETUP_ROOT)).toBe(false);
      expect(fs.readFileSync(path.join(temporary, "gh-aw", "legacy"), "utf8")).toBe("never reclaim");
      expect((await invoke("clean.sh", second)).status).toBe(0);
      expect(fs.readFileSync(calls, "utf8")).not.toMatch(/rm -f awmg-mcpg(?:\s|$)/);
    },
    90000
  );
});
