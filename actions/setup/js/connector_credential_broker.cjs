// @ts-check
// @safe-outputs-exempt USE-001 — standalone credential transport with no dependency on safe-output handlers.
const fs = require("node:fs");
const net = require("node:net");
const path = require("node:path");
const { fork } = require("node:child_process");
const { createHash } = require("node:crypto");
const { createLogger, debugEnabled } = require("./mcp_logger.cjs");

const limit = 64 * 1024;
const timeout = 11000;
const failure = () => new Error("Connector credential broker unavailable");
const validBearer = value => typeof value === "string" && value.length > 0 && value.length <= limit && /^[A-Za-z0-9._~+/-]+=*$/u.test(value) && !/\s/u.test(value);
const log = createLogger("connector:credential-broker");
const events = new Set([
  "broker.ready",
  "broker.request",
  "broker.request.failed",
  "token.cache.hit",
  "token.renew.start",
  "token.renew.join",
  "token.renew.success",
  "token.renew.failed",
  "azure.assertion.start",
  "azure.assertion.success",
  "azure.exchange.start",
  "azure.exchange.success",
  "azure.exchange.failed",
]);
let debugCount = 0;

function brokerDebug(event) {
  if (process.env.GH_AW_CONNECTOR_BROKER_DEBUG !== "1" || !events.has(event) || debugCount++ >= 256) return;
  try {
    fs.writeSync(3, event + "\n");
  } catch {
    // Diagnostics must not affect credential delivery.
  }
}

function renewableToken(exchange, now = Date.now, debug = brokerDebug) {
  let cached;
  let pending;
  async function renew() {
    debug("token.renew.start");
    try {
      const { value, expiresAt } = await exchange();
      const remaining = expiresAt - now();
      if (!validBearer(value) || !Number.isSafeInteger(expiresAt) || remaining <= 0) throw failure();
      cached = { value, refreshAt: expiresAt - Math.min(60000, remaining / 10) };
      debug("token.renew.success");
      return value;
    } catch (error) {
      debug("token.renew.failed");
      throw error;
    }
  }
  return async () => {
    if (cached && now() < cached.refreshAt) {
      debug("token.cache.hit");
      return cached.value;
    }
    cached = undefined;
    if (pending) debug("token.renew.join");
    pending ??= renew().finally(() => {
      pending = undefined;
    });
    return pending;
  };
}

function serve(socketPath, getToken) {
  const server = net.createServer(socket => {
    brokerDebug("broker.request");
    socket.on("error", () => socket.destroy());
    socket.setTimeout(timeout, () => socket.destroy());
    // Connecting requests exactly one credential. Client data is never read.
    getToken().then(
      value => (validBearer(value) ? socket.end(value) : socket.destroy()),
      () => {
        brokerDebug("broker.request.failed");
        socket.destroy();
      }
    );
  });
  server.maxConnections = 32;
  return server.listen(socketPath);
}

function readToken(socketPath) {
  if (typeof socketPath !== "string" || !path.isAbsolute(socketPath)) throw failure();
  return new Promise((resolve, reject) => {
    const socket = net.createConnection({ path: socketPath });
    const chunks = [];
    let size = 0;
    const timer = setTimeout(() => socket.destroy(failure()), timeout);
    socket.on("data", chunk => {
      size += chunk.length;
      if (size > limit) socket.destroy(failure());
      else chunks.push(chunk);
    });
    socket.on("close", () => clearTimeout(timer));
    socket.on("error", () => reject(failure()));
    socket.on("end", () => {
      const value = Buffer.concat(chunks).toString("utf8");
      if (size <= limit && validBearer(value)) resolve(value);
      else reject(failure());
    });
  });
}

function jobPrefix(trackingID) {
  if (typeof trackingID !== "string" || trackingID.length < 1 || trackingID.length > 256 || /[^\x21-\x7e]/u.test(trackingID)) throw failure();
  return "aw-" + createHash("sha256").update(trackingID).digest("hex").slice(0, 16) + "-";
}

function jobRoot(env) {
  if (!env.RUNNER_TEMP || !path.isAbsolute(env.RUNNER_TEMP) || /[\r\n]/u.test(env.RUNNER_TEMP)) throw failure();
  return fs.realpathSync(env.RUNNER_TEMP);
}

// Unlink only this job's private Unix socket, never signal a PID from disk.
// The broker watches its socket directory and exits when the endpoint is removed.
function stopBroker(env) {
  const dir = env.GH_AW_CONNECTOR_BROKER_DIRECTORY;
  const prefix = jobPrefix(env.RUNNER_TRACKING_ID);
  if (typeof dir !== "string" || dir !== path.resolve(dir) || path.dirname(dir) !== jobRoot(env) || !new RegExp(`^${prefix}[a-zA-Z0-9]{6}$`, "u").test(path.basename(dir))) throw failure();
  const stat = fs.lstatSync(dir, { throwIfNoEntry: false });
  if (!stat) return; // A prior stop or deadline already retired the capability.
  if (!stat.isDirectory() || (stat.mode & 0o077) !== 0 || stat.uid !== process.getuid?.()) throw failure();
  const debugFile = path.join(dir, "debug.log");
  try {
    const fd = fs.openSync(debugFile, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    try {
      const file = fs.fstatSync(fd);
      if (file.isFile() && file.uid === stat.uid) {
        const buffer = Buffer.alloc(16384);
        const length = fs.readSync(fd, buffer, 0, buffer.length, 0);
        for (const event of buffer.subarray(0, length).toString("utf8").split("\n")) {
          if (events.has(event)) log.diagnostic(event);
        }
      }
    } finally {
      fs.closeSync(fd);
    }
  } catch {
    // The optional debug file is untrusted and may be absent.
  }
  try {
    fs.rmSync(debugFile, { force: true });
  } catch {
    // Ignore an invalid debug file and still retire the socket.
  }
  const file = path.join(dir, "token.sock");
  const socket = fs.lstatSync(file, { throwIfNoEntry: false });
  if (socket) {
    if (!socket.isSocket() || socket.uid !== stat.uid || (socket.mode & 0o077) !== 0) throw failure();
    fs.unlinkSync(file);
  } else {
    try {
      fs.rmdirSync(dir);
    } catch {
      // The broker or runner cleanup may already have removed the directory.
    }
  }
}

async function startBroker(moduleFile, env, keys) {
  const prefix = jobPrefix(env.RUNNER_TRACKING_ID);
  const minutes = env.GH_AW_CONNECTOR_BROKER_TIMEOUT_MINUTES;
  if (!env.GITHUB_OUTPUT || !/^[1-9][0-9]{0,3}$/u.test(minutes) || Number(minutes) > 7200) throw failure();
  const deadline = Date.now() + Number(minutes) * 60000;
  const dir = fs.mkdtempSync(path.join(jobRoot(env), prefix));
  const socketPath = path.join(dir, "token.sock");
  let child;
  let debugFD;
  try {
    if (Buffer.byteLength(socketPath) > 103) throw failure();
    const debug = debugEnabled("connector:credential-broker");
    if (debug) debugFD = fs.openSync(path.join(dir, "debug.log"), "wx", 0o600);
    // Preserve runner job-end process tracking, but never inherit unrelated
    // secrets, Node preload flags, or step file-command handles.
    child = fork(moduleFile, ["serve", socketPath, String(deadline)], {
      env: { ...Object.fromEntries([...keys, "RUNNER_TRACKING_ID", "NODE_EXTRA_CA_CERTS"].map(key => [key, env[key]])), ...(debug ? { GH_AW_CONNECTOR_BROKER_DEBUG: "1" } : {}) },
      execArgv: [],
      detached: true,
      stdio: ["ignore", "ignore", "ignore", debugFD ?? "ignore", "ipc"],
    });
    if (debugFD !== undefined) {
      fs.closeSync(debugFD);
      debugFD = undefined;
    }
    const proc = child;
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(failure()), timeout);
      proc.once("error", () => {
        clearTimeout(timer);
        reject(failure());
      });
      proc.once("exit", () => {
        clearTimeout(timer);
        reject(failure());
      });
      proc.once("message", ready => {
        clearTimeout(timer);
        if (ready === "ready") resolve(true);
        else reject(failure());
      });
    });
    fs.appendFileSync(env.GITHUB_OUTPUT, `directory=${dir}\n`);
    log.debug("broker.started");
    proc.disconnect();
    proc.unref();
    return proc;
  } catch {
    if (debugFD !== undefined) fs.closeSync(debugFD);
    child?.kill();
    try {
      fs.rmSync(socketPath, { force: true });
      fs.rmSync(path.join(dir, "debug.log"), { force: true });
      fs.rmdirSync(dir);
    } catch {
      /* Child cleanup may have already run. */
    }
    log.debug("broker.start.failed");
    throw failure();
  }
}

function runBroker(socketPath, getToken, deadline) {
  const remaining = deadline - Date.now();
  if (!Number.isSafeInteger(deadline) || remaining <= 0 || remaining > 7200 * 60000 || !path.basename(path.dirname(socketPath)).startsWith(jobPrefix(process.env.RUNNER_TRACKING_ID))) throw failure();
  // This timer is armed once, before listening, and is never reset on renewal.
  setTimeout(() => process.exit(0), remaining);
  process.umask(0o077);
  const server = serve(socketPath, async () => {
    if (Date.now() >= deadline) throw failure();
    const value = await getToken();
    if (Date.now() >= deadline) throw failure();
    return value;
  });
  server.once("listening", () => {
    brokerDebug("broker.ready");
    fs.watch(path.dirname(socketPath), () => {
      if (!fs.existsSync(socketPath)) process.exit(0);
    }).on("error", () => process.exit(1));
    process.once("SIGTERM", () => process.exit(0));
    process.once("SIGINT", () => process.exit(0));
    process.once("exit", () => {
      try {
        fs.rmSync(socketPath, { force: true });
        fs.rmdirSync(path.dirname(socketPath));
      } catch {
        // Runner cleanup also removes RUNNER_TEMP after the job.
      }
    });
    process.send?.("ready");
  });
}

module.exports = { validBearer, renewableToken, readToken, serve, startBroker, runBroker, stopBroker, brokerDebug };
