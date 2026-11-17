// @ts-check

const path = require("node:path");
const fs = require("node:fs");

/**
 * Runtime state is separate from the trusted setup assets in RUNNER_TEMP/gh-aw.
 * There is intentionally no shared-directory fallback.
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {string}
 */
function runtimeRoot(env = process.env) {
  const root = env.GH_AW_RUNTIME_DIR;
  if (!root || !path.isAbsolute(root) || path.normalize(root) === path.parse(root).root || /[\0\r\n]/.test(root)) {
    throw new Error("GH_AW_RUNTIME_DIR must be an absolute, private runtime directory");
  }
  return path.normalize(root);
}

/**
 * @param {...string} segments
 * @returns {string}
 */
function runtimePath(...segments) {
  const root = runtimeRoot();
  const result = path.resolve(root, ...segments);
  if (result !== root && !result.startsWith(root + path.sep)) {
    throw new Error("Runtime paths must remain inside GH_AW_RUNTIME_DIR");
  }
  return result;
}

/** @param {NodeJS.ProcessEnv} [env] @returns {string} */
function setupRoot(env = process.env) {
  const temp = env.RUNNER_TEMP;
  if (!temp || !path.isAbsolute(temp) || /[\0\r\n]/.test(temp)) {
    throw new Error("RUNNER_TEMP must be an absolute setup directory");
  }
  return path.join(temp, "gh-aw");
}

/** @param {NodeJS.ProcessEnv} [env] */
function runtimeId(env = process.env) {
  const id = env.GH_AW_RUNTIME_ID;
  if (!id || !/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$/.test(id)) {
    throw new Error("GH_AW_RUNTIME_ID must be a Docker-safe runtime identifier");
  }
  return id;
}

/** @param {string} prefix @param {NodeJS.ProcessEnv} [env] */
function runtimeContainerName(prefix, env = process.env) {
  if (!/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(prefix)) {
    throw new Error("Runtime container prefix must be Docker-safe");
  }
  return `${prefix}-${runtimeId(env)}`;
}

/**
 * @param {string} name
 * @param {number} fallback
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {number}
 */
function runtimePort(name, fallback, env = process.env) {
  const raw = env[name];
  if (raw === undefined || raw === "") return fallback;
  if (!/^[0-9]+$/.test(raw) || !Number.isInteger(Number(raw)) || Number(raw) < 1 || Number(raw) > 65535) {
    throw new Error(`${name} must be a valid port number`);
  }
  return Number(raw);
}

/**
 * Record Linux process identity, never just a reusable PID, for post cleanup.
 * @param {number} pid
 * @param {string} role
 * @param {NodeJS.ProcessEnv} [env]
 */
function registerRuntimeProcess(pid, role, env = process.env) {
  if (process.platform !== "linux") return;
  if (!Number.isSafeInteger(pid) || pid <= 1) throw new Error("Runtime process PID must be a positive process identifier");
  runtimeId(env);
  const proc = `/proc/${pid}`;
  const stat = fs.readFileSync(`${proc}/stat`, "utf8");
  const start = stat
    .slice(stat.lastIndexOf(")") + 2)
    .trim()
    .split(/\s+/)[19];
  if (!start || !/^[0-9]+$/.test(start)) throw new Error(`Could not read runtime process identity for PID ${pid}`);
  const uid = fs.statSync(proc).uid;
  const record = JSON.stringify({ pid, start, uid, role }) + "\n";
  const file = path.join(runtimeRoot(env), ".gh-aw-processes.jsonl");
  const descriptor = fs.openSync(file, fs.constants.O_WRONLY | fs.constants.O_APPEND | fs.constants.O_CREAT | fs.constants.O_NOFOLLOW, 0o600);
  try {
    fs.writeFileSync(descriptor, record);
  } finally {
    fs.closeSync(descriptor);
  }
}

module.exports = { runtimeRoot, runtimePath, setupRoot, runtimeId, runtimeContainerName, runtimePort, registerRuntimeProcess };
