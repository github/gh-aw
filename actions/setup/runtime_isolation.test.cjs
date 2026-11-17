const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const crypto = require("node:crypto");
const { spawn, spawnSync } = require("node:child_process");
const { test } = require("node:test");

const lifecycle = path.join(__dirname, "sh/runtime_lifecycle.sh");
const setup = path.join(__dirname, "setup.sh");
const clean = path.join(__dirname, "clean.sh");
const ports = ["CLI_PROXY", "MCP_GATEWAY", "MCP_CONTROL", "MCP_SCRIPTS", "SAFE_OUTPUTS", "COPILOT_SDK"];

function sanitizedEnv() {
  const env = { ...process.env };
  for (const key of Object.keys(env)) {
    if (/^(?:OTEL_|GH_AW_OTLP_|GH_AW_OTEL_|GITHUB_AW_OTEL_|INPUT_)/.test(key)) delete env[key];
  }
  return env;
}

function fixture(t) {
  const root = fs.mkdtempSync(path.join(process.env.TMPDIR || os.tmpdir(), "runtime-isolation-"));
  const runner = path.join(root, "runner");
  const bin = path.join(root, "bin");
  const daemon = path.join(root, "daemon");
  for (const dir of [runner, bin, daemon]) fs.mkdirSync(dir);
  fs.writeFileSync(
    path.join(bin, "docker"),
    `#!${process.execPath}
const fs = require("node:fs");
const path = require("node:path");
const args = process.argv.slice(2);
const root = process.env.TEST_DOCKER_DAEMON;
fs.appendFileSync(path.join(root, "calls.jsonl"), JSON.stringify(args) + "\\n");
if (args[0] === "inspect") {
  if (process.env.TEST_DOCKER_UNAVAILABLE === "1") {
    process.stderr.write("Cannot connect to the Docker daemon");
    process.exit(1);
  }
  const file = path.join(root, args.at(-1) + ".json");
  if (!fs.existsSync(file)) {
    process.stderr.write("Error: No such container: " + args.at(-1));
    process.exit(1);
  }
  process.stdout.write(fs.readFileSync(file));
} else if (args[0] === "rm") {
  for (const name of fs.readdirSync(root).filter(name => name.endsWith(".json"))) {
    const file = path.join(root, name);
    if (JSON.parse(fs.readFileSync(file)).Id === args.at(-1)) fs.unlinkSync(file);
  }
} else process.exit(1);
`,
    { mode: 0o755 }
  );
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const invocation = () => {
    const name = crypto.randomUUID();
    const env = {
      ...sanitizedEnv(),
      PATH: `${bin}${path.delimiter}${process.env.PATH}`,
      RUNNER_TEMP: runner,
      GITHUB_ENV: path.join(root, `${name}.env`),
      GITHUB_STATE: path.join(root, `${name}.state`),
      GITHUB_OUTPUT: path.join(root, `${name}.output`),
      GH_AW_SKIP_SETUP_OTLP: "1",
      TEST_DOCKER_DAEMON: daemon,
    };
    for (const key of Object.keys(env)) if (key.startsWith("GH_AW_RUNTIME_") || key.startsWith("STATE_GH_AW_RUNTIME_")) delete env[key];
    for (const key of ["GITHUB_ENV", "GITHUB_STATE", "GITHUB_OUTPUT"]) fs.writeFileSync(env[key], "");
    return env;
  };
  const record = env =>
    Object.fromEntries(
      fs
        .readFileSync(env.GITHUB_ENV, "utf8")
        .trim()
        .split("\n")
        .filter(Boolean)
        .map(line => {
          const split = line.indexOf("=");
          return [line.slice(0, split), line.slice(split + 1)];
        })
    );
  const addContainer = (state, prefix = "awmg-mcpg", owner = state.GH_AW_RUNTIME_ID) => {
    const name = `${prefix}-${state.GH_AW_RUNTIME_ID}`;
    const container = { Name: `/${name}`, Id: crypto.randomBytes(32).toString("hex"), Config: { Labels: { "gh-aw.runtime-id": owner } } };
    const file = path.join(daemon, `${name}.json`);
    fs.writeFileSync(file, JSON.stringify(container));
    return { file, ...container };
  };
  return { root, runner, bin, daemon, invocation, record, addContainer };
}

function run(script, args, env, expected = 0) {
  const result = spawnSync("bash", [script, ...args], { env, encoding: "utf8" });
  assert.equal(result.status, expected, result.stdout + result.stderr);
  return result;
}

function runAsync(script, args, env) {
  return new Promise((resolve, reject) => {
    const child = spawn("bash", [script, ...args], { env });
    let output = "";
    child.stdout.on("data", data => {
      output += data;
    });
    child.stderr.on("data", data => {
      output += data;
    });
    child.on("error", reject);
    child.on("exit", code => {
      try {
        assert.equal(code, 0, output);
        resolve();
      } catch (error) {
        reject(error);
      }
    });
  });
}

test("checkout credential cleanup still works before runtime allocation", t => {
  const f = fixture(t);
  const workspace = path.join(f.root, "workspace");
  fs.mkdirSync(path.join(workspace, ".git"), { recursive: true });
  const config = path.join(workspace, ".git/config");
  fs.writeFileSync(config, '[credential]\n\thelper = store\n[http "https://example.invalid"]\n\textraheader = fixture\n');
  run(path.join(__dirname, "sh/clean_git_credentials_checkout.sh"), [], { ...f.invocation(), GITHUB_WORKSPACE: workspace });
  assert.doesNotMatch(fs.readFileSync(config, "utf8"), /helper|extraheader/);
});

test("two simultaneous setups isolate files, ports and daemon teardown under a shared temp parent", async t => {
  const f = fixture(t);
  const envs = [f.invocation(), f.invocation()];
  const stale = path.join(f.runner, "gh-aw-job-stale");
  fs.mkdirSync(stale);
  fs.writeFileSync(path.join(stale, "sentinel"), "leave stale state");
  const setupResults = await Promise.allSettled(envs.map(env => runAsync(setup, [], env)));
  for (const result of setupResults) if (result.status === "rejected") throw result.reason;
  const states = envs.map(f.record);
  assert.notEqual(states[0].GH_AW_RUNTIME_DIR, states[1].GH_AW_RUNTIME_DIR);
  const allPorts = states.flatMap(state => ports.map(port => state[`GH_AW_${port}_PORT`]));
  assert.equal(new Set(allPorts).size, allPorts.length);
  for (const state of states) {
    assert.equal(path.dirname(state.GH_AW_RUNTIME_DIR), fs.realpathSync(f.runner));
    assert.equal(fs.statSync(state.GH_AW_RUNTIME_DIR).mode & 0o777, 0o700);
    assert.notEqual(state.GH_AW_RUNTIME_DIR, path.join(f.runner, "gh-aw"));
    fs.writeFileSync(path.join(state.GH_AW_RUNTIME_DIR, "sentinel"), state.GH_AW_RUNTIME_ID);
  }
  const containers = states.map(state => ["awmg-mcpg", "awmg-cli-proxy", "awmg-proxy"].map(prefix => f.addContainer(state, prefix)));
  const other = f.addContainer({ GH_AW_RUNTIME_ID: "gh-aw-job-unrelated" });
  run(clean, [], { ...envs[0], ...states[0] });
  assert.equal(fs.existsSync(states[0].GH_AW_RUNTIME_DIR), false);
  for (const container of containers[0]) assert.equal(fs.existsSync(container.file), false);
  for (const container of containers[1]) assert.equal(fs.existsSync(container.file), true);
  assert.equal(fs.readFileSync(path.join(states[1].GH_AW_RUNTIME_DIR, "sentinel"), "utf8"), states[1].GH_AW_RUNTIME_ID);
  run(path.join(f.runner, "gh-aw/actions/clean.sh"), [], { ...envs[1], ...states[1] });
  assert.equal(fs.existsSync(states[1].GH_AW_RUNTIME_DIR), false);
  assert.equal(fs.existsSync(other.file), true);
  assert.equal(fs.readFileSync(path.join(stale, "sentinel"), "utf8"), "leave stale state");
  const calls = fs.readFileSync(path.join(f.daemon, "calls.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
  for (const args of calls.filter(args => args[0] === "rm")) assert.match(args.at(-1), /^[a-f0-9]{64}$/);
});

test("failed setup records post-step cleanup ownership before the first copy", t => {
  const f = fixture(t);
  const env = f.invocation();
  const destination = path.join(f.root, "not-a-directory");
  fs.writeFileSync(destination, "sentinel");
  const result = spawnSync("bash", [setup], { env: { ...env, INPUT_DESTINATION: destination }, encoding: "utf8" });
  assert.notEqual(result.status, 0);
  const state = f.record(env);
  assert.ok(state.GH_AW_RUNTIME_DIR);
  assert.equal(fs.readFileSync(env.GITHUB_STATE, "utf8"), fs.readFileSync(env.GITHUB_ENV, "utf8"));
  const stateEnv = Object.fromEntries(Object.entries(state).map(([key, value]) => [`STATE_${key}`, value]));
  const foreignScratch = path.join(f.root, "awf-tmp-foreign");
  fs.mkdirSync(foreignScratch);
  fs.writeFileSync(path.join(foreignScratch, "sentinel"), "unrelated invocation");
  const post = spawnSync(process.execPath, [path.join(__dirname, "post.js")], {
    env: { ...env, ...stateEnv, GH_AW_RUNTIME_DIR: destination, GH_AW_RUNTIME_TMP_DIR: foreignScratch },
    encoding: "utf8",
  });
  assert.equal(post.status, 0, post.stderr);
  assert.equal(fs.existsSync(state.GH_AW_RUNTIME_DIR), false);
  assert.equal(fs.readFileSync(destination, "utf8"), "sentinel");
  assert.equal(fs.readFileSync(path.join(foreignScratch, "sentinel"), "utf8"), "unrelated invocation");
});

test("script-mode cleanup uses per-setup ownership outputs instead of a later setup's ambient environment", t => {
  const f = fixture(t);
  const envs = [f.invocation(), f.invocation()];
  for (const env of envs) run(lifecycle, ["allocate"], env);
  const states = envs.map(f.record);
  const firstOutputs = f.record({ GITHUB_ENV: envs[0].GITHUB_OUTPUT });
  assert.deepEqual(Object.keys(firstOutputs).sort(), ["DIR", "ID", "DEV", "INO", "UID", "OWNER"].map(suffix => `GH_AW_RUNTIME_${suffix}`).sort());
  for (const [key, value] of Object.entries(firstOutputs)) assert.equal(value, states[0][key]);
  // Generated cleanup steps explicitly overlay their own setup outputs.
  run(clean, [], { ...envs[0], ...states[1], ...firstOutputs });
  assert.equal(fs.existsSync(states[0].GH_AW_RUNTIME_DIR), false);
  assert.equal(fs.existsSync(states[1].GH_AW_RUNTIME_DIR), true);
  run(clean, [], { ...envs[1], ...states[1] });
  assert.equal(fs.existsSync(states[1].GH_AW_RUNTIME_DIR), false);
});

test("interrupted invocation leaves a recorded allocation that a later clean can reclaim", async t => {
  const f = fixture(t);
  const env = f.invocation();
  const child = spawn("bash", ["-c", 'source "$1"; allocate_gh_aw_runtime; exec "$2" -e \'process.stdout.write("ready\\\\n"); setInterval(() => {}, 1000)\'', "interrupt-test", lifecycle, process.execPath], { env });
  await new Promise((resolve, reject) => {
    child.stdout.once("data", resolve);
    child.once("error", reject);
    child.once("exit", () => reject(new Error("invocation exited before interruption")));
  });
  const exited = new Promise(resolve => child.once("exit", resolve));
  child.kill("SIGTERM");
  await exited;
  const state = f.record(env);
  assert.equal(fs.existsSync(state.GH_AW_RUNTIME_DIR), true);
  run(clean, [], { ...env, ...state });
  assert.equal(fs.existsSync(state.GH_AW_RUNTIME_DIR), false);
});

test("cleanup refuses replaced inode, symlink, marker, foreign child and Docker ownership", t => {
  const f = fixture(t);
  for (const scenario of ["inode", "symlink", "marker", "docker", "uid", "daemon-unavailable"]) {
    const env = f.invocation();
    run(lifecycle, ["allocate"], env);
    const state = f.record(env);
    const root = state.GH_AW_RUNTIME_DIR;
    if (scenario === "inode") state.GH_AW_RUNTIME_INO = String(BigInt(state.GH_AW_RUNTIME_INO) + 1n);
    if (scenario === "uid") state.GH_AW_RUNTIME_UID = String(Number(state.GH_AW_RUNTIME_UID) + 1);
    if (scenario === "marker") fs.writeFileSync(path.join(root, ".gh-aw-owner"), "foreign");
    if (scenario === "docker") f.addContainer(state, "awmg-mcpg", "another-invocation");
    if (scenario === "symlink") {
      fs.renameSync(root, `${root}-original`);
      fs.symlinkSync(`${root}-original`, root);
    }
    run(lifecycle, ["cleanup"], { ...env, ...state, TEST_DOCKER_UNAVAILABLE: scenario === "daemon-unavailable" ? "1" : "0" }, 1);
    assert.equal(fs.existsSync(root), true, scenario);
  }
  if (process.getuid?.() === 0) {
    const env = f.invocation();
    run(lifecycle, ["allocate"], env);
    const state = f.record(env);
    const child = path.join(state.GH_AW_RUNTIME_DIR, "foreign");
    fs.writeFileSync(child, "foreign");
    fs.chownSync(child, 65534, 65534);
    run(lifecycle, ["cleanup"], { ...env, ...state }, 1);
    assert.equal(fs.readFileSync(child, "utf8"), "foreign");
  }
});

test("allocator rejects shell/JSON-unsafe canonical temp paths before allocation", t => {
  const f = fixture(t);
  for (const name of ["space dir", "quote'dir", 'double"quote', "back\\slash", "line\nbreak"]) {
    const parent = path.join(f.root, name);
    fs.mkdirSync(parent);
    const env = { ...f.invocation(), RUNNER_TEMP: parent };
    run(lifecycle, ["allocate"], env, 1);
    assert.deepEqual(fs.readdirSync(parent), []);
    assert.equal(fs.readFileSync(env.GITHUB_ENV, "utf8"), "");
  }
});

test(
  "cleanup stops only host services with matching PID start-time, UID and invocation environment",
  {
    skip: process.platform !== "linux",
    timeout: 10000,
  },
  async t => {
    const f = fixture(t);
    const env = f.invocation();
    run(lifecycle, ["allocate"], env);
    const state = f.record(env);
    const start = childEnv => {
      const child = spawn(process.execPath, ["-e", 'process.stdout.write("ready"); setInterval(() => {}, 1000)'], { env: childEnv });
      t.after(() => child.kill("SIGKILL"));
      return new Promise((resolve, reject) => {
        child.once("error", reject);
        child.stdout.once("data", () => resolve(child));
      });
    };
    const owned = await start({ ...env, ...state });
    const foreign = await start(env);
    const register = spawnSync("bash", ["-c", 'source "$1"; register_gh_aw_process "$2" mcp-scripts', "register-test", path.join(__dirname, "sh/runtime_processes.sh"), String(owned.pid)], { env: { ...env, ...state }, encoding: "utf8" });
    assert.equal(register.status, 0, register.stderr);
    const procStat = fs.readFileSync(`/proc/${foreign.pid}/stat`, "utf8");
    fs.appendFileSync(
      path.join(state.GH_AW_RUNTIME_DIR, ".gh-aw-processes.jsonl"),
      JSON.stringify({
        pid: foreign.pid,
        start: procStat.slice(procStat.lastIndexOf(")") + 2).split(" ")[19],
        uid: process.getuid(),
        role: "foreign",
      }) + "\n"
    );
    const exited = new Promise(resolve => owned.once("exit", resolve));
    run(lifecycle, ["cleanup"], { ...env, ...state });
    await exited;
    assert.equal(foreign.kill(0), true);
  }
);

test(
  "real Docker daemon isolates concurrent runtime containers and refuses root-owned residue",
  {
    skip: process.env.GH_AW_TEST_REAL_DOCKER !== "1" ? "set GH_AW_TEST_REAL_DOCKER=1 to use a local cached image" : false,
  },
  async t => {
    const f = fixture(t);
    const dockerConfig = path.join(f.root, "docker-config");
    fs.mkdirSync(dockerConfig);
    const pinLocalDaemon = env => {
      const pinned = { ...env, DOCKER_HOST: "unix:///var/run/docker.sock", DOCKER_CONFIG: dockerConfig };
      for (const key of ["DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"]) delete pinned[key];
      return pinned;
    };
    const dockerEnv = pinLocalDaemon(sanitizedEnv());
    const docker = args => spawnSync("docker", args, { env: dockerEnv, encoding: "utf8" });
    assert.equal(docker(["version", "--format", "{{.Server.Version}}"]).status, 0, "Docker daemon must be available");
    const image = process.env.GH_AW_TEST_DOCKER_IMAGE || "";
    assert.match(image, /^(?:sha256:[a-f0-9]{64}|[^@\s]+@sha256:[a-f0-9]{64})$/, "test requires an explicitly reviewed immutable cached image");
    assert.equal(docker(["image", "inspect", image]).status, 0, "test requires a cached image and never pulls");
    const envs = [f.invocation(), f.invocation()];
    const setupResults = await Promise.allSettled(envs.map(env => runAsync(setup, [], env)));
    for (const result of setupResults) if (result.status === "rejected") throw result.reason;
    const states = envs.map(f.record);
    const ids = [];
    t.after(() => {
      for (const id of ids) docker(["rm", "-f", id]);
    });
    for (const [index, state] of states.entries()) {
      const result = docker([
        "run",
        "--pull=never",
        "--network",
        "none",
        "-d",
        "--name",
        `awmg-mcpg-${state.GH_AW_RUNTIME_ID}`,
        "--label",
        `gh-aw.runtime-id=${state.GH_AW_RUNTIME_ID}`,
        "--user",
        `${process.getuid()}:${process.getgid()}`,
        "-v",
        `${state.GH_AW_RUNTIME_DIR}:/runtime`,
        "--entrypoint",
        "/bin/sh",
        image,
        "-c",
        `printf '${index}' > /runtime/ready; exec sleep 120`,
      ]);
      assert.equal(result.status, 0, result.stderr);
      ids.push(result.stdout.trim());
      assert.match(ids.at(-1), /^[a-f0-9]{64}$/);
      assert.equal(docker(["exec", ids.at(-1), "/bin/sh", "-c", "for attempt in 1 2 3 4 5; do test -f /runtime/ready && exit 0; sleep 1; done; exit 1"]).status, 0);
    }
    run(clean, [], pinLocalDaemon({ ...envs[0], ...states[0], PATH: process.env.PATH }));
    assert.notEqual(docker(["inspect", ids[0]]).status, 0);
    assert.equal(docker(["inspect", ids[1]]).status, 0);
    assert.equal(fs.readFileSync(path.join(states[1].GH_AW_RUNTIME_DIR, "ready"), "utf8"), "1");
    if (process.getuid() !== 0) {
      assert.equal(docker(["exec", "--user", "0:0", ids[1], "/bin/sh", "-c", "printf residue > /runtime/root-owned"]).status, 0);
      run(lifecycle, ["cleanup"], pinLocalDaemon({ ...envs[1], ...states[1], PATH: process.env.PATH }), 1);
      const residue = path.join(states[1].GH_AW_RUNTIME_DIR, "root-owned");
      assert.equal(fs.statSync(residue).uid, 0);
      assert.equal(fs.readFileSync(residue, "utf8"), "residue");
    } else {
      run(clean, [], pinLocalDaemon({ ...envs[1], ...states[1], PATH: process.env.PATH }));
    }
  }
);
