#!/usr/bin/env bash
set +o histexpand

# Keep allocation and reclamation shared by action mode and script mode.
allocate_gh_aw_runtime() {
  local runtime_env
  runtime_env="$(node <<'NODE'
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const net = require("node:net");
let root;
let persisted = false;
const reservations = [];
(async () => {
  const windows = process.platform === "win32";
  const canonical = value => windows ? value.replaceAll("\\", "/") : value;
  const parent = canonical(fs.realpathSync(process.env.RUNNER_TEMP));
  if (!(windows ? /^[a-zA-Z]:\/[a-zA-Z0-9_./-]+$/ : /^\/[a-zA-Z0-9_./-]+$/).test(parent)) {
    throw new Error("RUNNER_TEMP must resolve to a safe absolute path containing only a drive prefix (Windows), letters, digits, underscore, dot, slash and hyphen");
  }
  root = canonical(fs.mkdtempSync(path.join(parent, "gh-aw-job-")));
  if (windows) {
    const { spawnSync } = require("node:child_process");
    const options = { encoding: "utf8", windowsHide: true, timeout: 10000, maxBuffer: 16384 };
    const identity = spawnSync("powershell.exe", [
      "-NoProfile", "-NonInteractive", "-Command",
      "[System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value",
    ], options);
    const sid = identity.stdout?.trim() || "";
    if (identity.error || identity.status !== 0 ||
        !/^S-1-[0-9]{1,15}(?:-[0-9]{1,10}){1,15}$/.test(sid)) {
      throw new Error("Unable to identify the Windows private runtime owner");
    }
    const permissions = spawnSync("icacls.exe", [
      root, "/inheritance:r", "/grant:r", `*${sid}:(OI)(CI)F`,
    ], options);
    if (permissions.error || permissions.status !== 0) {
      throw new Error("Unable to restrict the Windows private runtime ACL");
    }
  } else {
    fs.chmodSync(root, 0o700);
  }
  const stat = fs.lstatSync(root, { bigint: true });
  const record = {
    GH_AW_RUNTIME_DIR: root,
    GH_AW_RUNTIME_ID: path.basename(root),
    GH_AW_RUNTIME_DEV: String(stat.dev),
    GH_AW_RUNTIME_INO: String(stat.ino),
    GH_AW_RUNTIME_UID: String(stat.uid),
    GH_AW_RUNTIME_OWNER: crypto.randomBytes(32).toString("hex"),
  };
  // Hold all reservations together so this invocation never selects one port twice.
  for (const key of ["GH_AW_CLI_PROXY_PORT", "GH_AW_MCP_GATEWAY_PORT", "GH_AW_MCP_CONTROL_PORT", "GH_AW_MCP_SCRIPTS_PORT", "GH_AW_SAFE_OUTPUTS_PORT", "GH_AW_COPILOT_SDK_PORT"]) {
    const server = net.createServer();
    reservations.push(server);
    await new Promise((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.1", resolve);
    });
    record[key] = String(server.address().port);
  }
  fs.writeFileSync(path.join(root, ".gh-aw-owner"), record.GH_AW_RUNTIME_OWNER, { flag: "wx", mode: 0o600 });
  const lines = Object.entries(record).map(([key, value]) => {
    if (/[\r\n]/.test(value)) throw new Error("Runtime state contains a newline");
    return `${key}=${value}\n`;
  }).join("");
  // Persist before setup copies anything: the action post step also runs after setup fails.
  if (!process.env.GITHUB_ENV) throw new Error("GITHUB_ENV is required to persist runtime ownership");
  if (process.env.GITHUB_STATE) {
    fs.appendFileSync(process.env.GITHUB_STATE, lines);
    persisted = true;
  }
  fs.appendFileSync(process.env.GITHUB_ENV, lines);
  persisted = true;
  if (process.env.GITHUB_OUTPUT) {
    const ownership = ["DIR", "ID", "DEV", "INO", "UID", "OWNER"];
    fs.appendFileSync(process.env.GITHUB_OUTPUT, ownership.map(suffix => {
      const key = `GH_AW_RUNTIME_${suffix}`;
      return `${key}=${record[key]}\n`;
    }).join(""));
  }
  process.stdout.write(lines);
})().catch(error => {
  if (root && !persisted) fs.rmSync(root, { recursive: true, force: true });
  console.error(`::error::Unable to allocate private gh-aw runtime: ${error.message}`);
  process.exitCode = 1;
}).finally(() => {
  for (const server of reservations) server.close();
});
NODE
)" || return 1
  while IFS='=' read -r key value; do
    export "$key=$value"
  done <<< "$runtime_env"
}

cleanup_gh_aw_runtime() {
  node <<'NODE'
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const state = name => process.env[name] || "";
const root = state("GH_AW_RUNTIME_DIR");
if (!root) process.exit(0);
const id = state("GH_AW_RUNTIME_ID");
const uid = state("GH_AW_RUNTIME_UID");
const windows = process.platform === "win32";
const canonical = value => windows ? value.replaceAll("\\", "/") : value;
function fail(message) { throw new Error(message); }
function validateRoot() {
  if (!path.isAbsolute(root) || canonical(path.normalize(root)) !== root ||
      path.basename(root) !== id || !/^gh-aw-job-[a-zA-Z0-9]+$/.test(id)) fail("invalid allocated runtime path");
  const stat = fs.lstatSync(root, { bigint: true });
  if (!stat.isDirectory() || stat.isSymbolicLink() || canonical(fs.realpathSync(root)) !== root ||
      String(stat.dev) !== state("GH_AW_RUNTIME_DEV") ||
      String(stat.ino) !== state("GH_AW_RUNTIME_INO") ||
      (!windows && (String(stat.uid) !== uid ||
        (typeof process.getuid === "function" && String(process.getuid()) !== uid) ||
        (stat.mode & 0o777n) !== 0o700n))) fail("runtime identity, owner or permissions changed");
  const marker = path.join(root, ".gh-aw-owner");
  const markerStat = fs.lstatSync(marker, { bigint: true });
  if (!markerStat.isFile() || markerStat.isSymbolicLink() || markerStat.nlink !== 1n ||
      (!windows && String(markerStat.uid) !== uid) || markerStat.dev !== stat.dev ||
      !/^[a-f0-9]{64}$/.test(state("GH_AW_RUNTIME_OWNER")) ||
      fs.readFileSync(marker, "utf8") !== state("GH_AW_RUNTIME_OWNER")) fail("runtime owner marker changed");
}
function validateChildren(directory) {
  for (const entry of fs.readdirSync(directory)) {
    const child = path.join(directory, entry);
    const stat = fs.lstatSync(child, { bigint: true });
    if ((!windows && String(stat.uid) !== uid) || String(stat.dev) !== state("GH_AW_RUNTIME_DEV")) {
      fail("runtime contains foreign-owned files or a mounted filesystem; refusing elevated cleanup");
    }
    if (stat.isDirectory() && !stat.isSymbolicLink()) validateChildren(child);
  }
}
function stopHostServices() {
  const file = path.join(root, ".gh-aw-processes.jsonl");
  let stat;
  try { stat = fs.lstatSync(file); } catch (error) { if (error.code === "ENOENT") return; throw error; }
  if (process.platform !== "linux" || !stat.isFile() || stat.isSymbolicLink() ||
      String(stat.uid) !== uid || stat.nlink !== 1 || stat.size > 1048576) fail("unsafe host service identity record");
  const processes = fs.readFileSync(file, "utf8").split("\n").filter(Boolean).map(line => JSON.parse(line));
  const matches = record => {
    if (!Number.isSafeInteger(record.pid) || record.pid <= 1 || record.pid === process.pid || record.pid === process.ppid) return false;
    try {
      const base = `/proc/${record.pid}`;
      const value = fs.readFileSync(`${base}/stat`, "utf8");
      const fields = value.slice(value.lastIndexOf(")") + 2).split(" ");
      if (fields[0] === "Z" || fields[19] !== record.start || String(record.uid) !== uid ||
          String(fs.statSync(base).uid) !== uid) return false;
      const env = fs.readFileSync(`${base}/environ`, "utf8").split("\0");
      return env.includes(`GH_AW_RUNTIME_ID=${id}`) &&
        env.includes(`GH_AW_RUNTIME_DIR=${root}`);
    } catch (error) {
      if (error.code === "ENOENT" || error.code === "ESRCH") return false;
      if (error.code === "EACCES") {
        try {
          const value = fs.readFileSync(`/proc/${record.pid}/stat`, "utf8");
          if (value.slice(value.lastIndexOf(")") + 2).split(" ")[0] === "Z") return false;
        } catch (retryError) {
          if (retryError.code === "ENOENT" || retryError.code === "ESRCH") return false;
        }
      }
      throw error;
    }
  };
  for (const record of processes) {
    if (!matches(record)) continue;
    try { process.kill(record.pid, "SIGTERM"); } catch (error) { if (error.code !== "ESRCH") throw error; }
    for (let attempt = 0; attempt < 20 && matches(record); attempt++) {
      Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 50);
    }
    if (matches(record)) process.kill(record.pid, "SIGKILL");
  }
}
try {
  try { fs.lstatSync(root); } catch (error) { if (error.code === "ENOENT") process.exit(0); throw error; }
  validateRoot();
  for (const prefix of ["awmg-mcpg", "awmg-cli-proxy", "awmg-proxy"]) {
    const name = `${prefix}-${id}`;
    const result = spawnSync("docker", ["inspect", "--type", "container", "--format", "{{json .}}", name], { encoding: "utf8", timeout: 10000 });
    if (result.error?.code === "ENOENT") break;
    if (result.error) fail(`could not inspect ${name}`);
    if (result.status !== 0) {
      if (/No such (?:container|object):/i.test(result.stderr || "")) continue;
      fail(`could not establish container ownership for ${name}`);
    }
    const container = JSON.parse(result.stdout);
    if (container.Name !== `/${name}` || container.Config?.Labels?.["gh-aw.runtime-id"] !== id ||
        !/^[a-f0-9]{64}$/.test(container.Id)) fail(`container ownership mismatch for ${name}`);
    const removed = spawnSync("docker", ["rm", "-f", container.Id], { encoding: "utf8", timeout: 30000 });
    if (removed.error || removed.status !== 0) fail(`could not remove owned container ${name}`);
  }
  // Never chown or sudo: rootless and interrupted runs must fail closed on foreign state.
  validateRoot();
  stopHostServices();
  validateChildren(root);
  validateRoot();
  fs.rmSync(root, { recursive: true, force: false });
} catch (error) {
  console.warn(`::warning::Leaving private gh-aw runtime untouched: ${error.message}`);
  process.exitCode = 1;
}
NODE
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  case "${1:-}" in
    allocate) allocate_gh_aw_runtime ;;
    cleanup) cleanup_gh_aw_runtime ;;
    *) echo "Usage: runtime_lifecycle.sh {allocate|cleanup}" >&2; exit 1 ;;
  esac
fi
