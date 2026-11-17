// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { spawn, spawnSync } = require("node:child_process");
const { constants: osConstants, homedir } = require("node:os");

const AWF_CONTAINERS = new Set(["awf-agent", "awf-squid", "awf-iptables-init", "awf-api-proxy"]);
/** @type {NodeJS.Signals[]} */
const SIGNALS = ["SIGINT", "SIGTERM", "SIGHUP"];
const LOCK_SCRIPT = 'flock --exclusive 3 && exec "$@"';

/**
 * AWF v0.29.14 has fixed Docker names. Serialize its complete lifecycle on a
 * shared Linux/WSL host and daemon until upstream supports namespacing. Setup
 * and gh-aw-owned resources remain concurrent; unrelated hosts sharing a remote
 * daemon are not coordinated by this host-local compatibility guard.
 */
function lockPath(daemonId) {
  const digest = crypto.createHash("sha256").update(daemonId).digest("hex");
  // Deliberately outside invocation-private TMPDIR: persistent inert metadata.
  return `/tmp/gh-aw-awf-${digest}.lock`;
}

function assertLockStat(stat) {
  if (!stat.isFile() || stat.nlink !== 1 || stat.size !== 0 || (stat.mode & 0o022) !== 0) {
    throw new Error("AWF coordination lock is unsafe; refusing to run");
  }
}

function assertLockIdentity(fd, file, io = fs) {
  const held = io.fstatSync(fd);
  const entry = io.lstatSync(file);
  assertLockStat(held);
  assertLockStat(entry);
  if (held.dev !== entry.dev || held.ino !== entry.ino) {
    throw new Error("AWF coordination lock changed; refusing to run");
  }
}

function openLock(file, io = fs, uid = process.geteuid ? process.geteuid() : -1) {
  if (!io.constants.O_NOFOLLOW) {
    throw new Error("AWF isolation requires Linux/WSL no-follow file support");
  }
  const flags = io.constants.O_RDONLY | io.constants.O_NOFOLLOW | io.constants.O_NONBLOCK;
  let fd;
  try {
    for (let attempt = 0; attempt < 3 && fd === undefined; attempt++) {
      try {
        fd = io.openSync(file, flags);
      } catch (error) {
        if (error.code !== "ENOENT") throw error;
        try {
          fd = io.openSync(file, flags | io.constants.O_CREAT | io.constants.O_EXCL, 0o444);
        } catch (createError) {
          if (createError.code === "EEXIST") continue;
          throw createError;
        }
        if (io.fstatSync(fd).uid !== uid) {
          throw new Error("AWF coordination lock has unexpected ownership");
        }
        io.fchmodSync(fd, 0o444);
      }
    }
    if (fd === undefined) throw new Error("AWF coordination lock changed repeatedly");
    assertLockIdentity(fd, file, io);
    return fd;
  } catch {
    if (fd !== undefined) io.closeSync(fd);
    // Do not expose paths, Docker stderr, command arguments, or environment.
    throw new Error("Unable to safely open AWF coordination lock; refusing to run");
  }
}

function dockerQuery(args, run = spawnSync, env = process.env) {
  const result = run("docker", args, {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    timeout: 30000,
    maxBuffer: 1024 * 1024,
    env,
  });
  if (result.error || result.signal || result.status !== 0 || typeof result.stdout !== "string") {
    throw new Error("AWF Docker inspection failed; refusing to run");
  }
  return result.stdout.trim();
}

/**
 * Pin the effective endpoint before sudo -E can change HOME/current context.
 * Preserve Docker configuration and explicit TLS credentials; refuse context-
 * managed TLS and SSH rather than silently selecting different credentials.
 */
function connectionEnvironment(query = dockerQuery, env = process.env, home = homedir()) {
  let context;
  try {
    context = JSON.parse(query(["context", "inspect", "--format", "{{json .}}"]));
  } catch {
    throw new Error("AWF Docker context inspection failed; refusing to run");
  }
  const endpoint = context?.Endpoints?.docker;
  const tlsMaterial = context?.TLSMaterial;
  const explicitHost = !env.DOCKER_CONTEXT && Boolean(env.DOCKER_HOST);
  const effectiveHost = explicitHost ? env.DOCKER_HOST : endpoint?.Host;
  if (
    typeof context?.Name !== "string" ||
    !endpoint ||
    typeof effectiveHost !== "string" ||
    !/^(unix:\/\/\/|tcp:\/\/)[^\s\u0000]+$/.test(effectiveHost) ||
    endpoint.SkipTLSVerify !== false ||
    !tlsMaterial ||
    typeof tlsMaterial !== "object" ||
    Array.isArray(tlsMaterial) ||
    Object.keys(tlsMaterial).length !== 0 ||
    (!explicitHost && context.Name !== "default" && (env.DOCKER_TLS || env.DOCKER_TLS_VERIFY || env.DOCKER_CERT_PATH))
  ) {
    throw new Error("AWF requires a Unix/TCP Docker endpoint without context-managed TLS; refusing to run");
  }
  const config = path.resolve(env.DOCKER_CONFIG || path.join(home, ".docker"));
  /** @type {NodeJS.ProcessEnv} */
  const bound = {
    ...env,
    DOCKER_HOST: effectiveHost,
    DOCKER_CONFIG: config,
  };
  delete bound.DOCKER_CONTEXT;
  if (env.DOCKER_TLS || env.DOCKER_TLS_VERIFY || env.DOCKER_CERT_PATH) {
    bound.DOCKER_CERT_PATH = path.resolve(env.DOCKER_CERT_PATH || config);
  }
  return bound;
}

function daemonIdentity(query = dockerQuery) {
  const id = query(["info", "--format", "{{.ID}}"]);
  if (!/^[a-zA-Z0-9:_-]+$/.test(id)) {
    throw new Error("AWF Docker daemon identity is unavailable; refusing to run");
  }
  return id;
}

function namesFromList(value) {
  if (!value) return [];
  const names = value.split("\n");
  if (names.some(name => !/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(name))) {
    throw new Error("AWF Docker resource listing is invalid; refusing to run");
  }
  return names;
}

function preflight(expectedDaemon, query = dockerQuery) {
  if (daemonIdentity(query) !== expectedDaemon) {
    throw new Error("AWF Docker daemon changed while waiting; refusing to run");
  }
  const containers = namesFromList(query(["container", "ls", "--all", "--format", "{{.Names}}"]));
  if (containers.some(name => AWF_CONTAINERS.has(name))) {
    throw new Error("Existing AWF containers require operator inspection; refusing to run without removing them");
  }
  const networks = namesFromList(query(["network", "ls", "--format", "{{.Name}}"]));
  if (!networks.includes("awf-net")) return;
  let endpoints;
  try {
    endpoints = JSON.parse(query(["network", "inspect", "awf-net", "--format", "{{json .Containers}}"]));
  } catch {
    throw new Error("AWF network inspection failed; refusing to run");
  }
  if (!endpoints || typeof endpoints !== "object" || Array.isArray(endpoints) || Object.keys(endpoints).length !== 0) {
    throw new Error("Existing AWF network is occupied or invalid; refusing to run without removing it");
  }
}

function exitStatus(code, signal) {
  if (signal && osConstants.signals[signal]) return 128 + osConstants.signals[signal];
  return Number.isInteger(code) && code >= 0 ? code : 1;
}

/**
 * Keep the supervising Node process (and its lock descriptor) alive until the
 * child exits. Each child has its own process group so forwarding happens once.
 * @param {string} command
 * @param {string[]} args
 * @param {number} fd
 * @param {typeof spawn} [launcher]
 * @param {{on: (event: string, handler: () => void) => unknown, removeListener: (event: string, handler: () => void) => unknown}} [signals]
 * @param {typeof process.kill} [kill]
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {Promise<number>}
 */
function supervise(command, args, fd, launcher = spawn, signals = process, kill = process.kill, env = undefined) {
  return new Promise(resolve => {
    /** @type {import("node:child_process").ChildProcess | undefined} */
    let child;
    /** @type {NodeJS.Signals | undefined} */
    let receivedSignal;
    /** @type {Map<NodeJS.Signals, () => void>} */
    const handlers = new Map(
      SIGNALS.map(signal => [
        signal,
        () => {
          receivedSignal ||= signal;
          if (child?.pid) {
            try {
              kill(-child.pid, signal);
            } catch {
              // A concurrently exiting child needs no further signaling.
            }
          }
        },
      ])
    );
    for (const [signal, handler] of handlers) signals.on(signal, handler);
    const finish = (code, signal) => {
      for (const [name, handler] of handlers) signals.removeListener(name, handler);
      resolve(exitStatus(code, receivedSignal || signal));
    };
    try {
      child = launcher(command, args, { stdio: ["inherit", "inherit", "inherit", fd], detached: true, ...(env ? { env } : {}) });
    } catch {
      finish(1, null);
      return;
    }
    child.once("error", () => finish(1, null));
    child.once("close", finish);
  });
}

async function main(argv = process.argv.slice(2)) {
  const locked = argv[0] === "--locked";
  const daemonId = locked ? argv[1] : undefined;
  const command = locked ? argv.slice(3) : argv.slice(1);
  if ((locked ? argv[2] : argv[0]) !== "--" || !command.length || !command[0]) {
    throw new Error("Usage: node run_awf_isolated.cjs -- command [arguments...]");
  }
  if (process.platform !== "linux") {
    throw new Error("AWF shared-resource isolation requires Linux or WSL with flock");
  }
  if (locked) {
    if (!daemonId || !/^[a-zA-Z0-9:_-]+$/.test(daemonId)) throw new Error("Invalid AWF lock identity");
    assertLockIdentity(3, lockPath(daemonId));
    preflight(daemonId);
    return supervise(command[0], command.slice(1), 3);
  }
  const originalId = daemonIdentity();
  const env = connectionEnvironment();
  const id = daemonIdentity(args => dockerQuery(args, spawnSync, env));
  if (id !== originalId) {
    throw new Error("AWF Docker endpoint binding changed daemon; refusing to run");
  }
  const fd = openLock(lockPath(id));
  try {
    return await supervise("bash", ["-c", LOCK_SCRIPT, "gh-aw-awf-lock", process.execPath, path.resolve(__filename), "--locked", id, "--", ...command], fd, spawn, process, process.kill, env);
  } finally {
    fs.closeSync(fd);
  }
}

if (require.main === module) {
  main().then(
    status => {
      process.exitCode = status;
    },
    () => {
      console.error("AWF isolation guard failed; shared resources were not removed. Check Docker access, flock, and stale AWF resources.");
      process.exitCode = 1;
    }
  );
}

module.exports = {
  AWF_CONTAINERS,
  LOCK_SCRIPT,
  lockPath,
  assertLockStat,
  assertLockIdentity,
  openLock,
  dockerQuery,
  connectionEnvironment,
  daemonIdentity,
  preflight,
  exitStatus,
  supervise,
  main,
};
