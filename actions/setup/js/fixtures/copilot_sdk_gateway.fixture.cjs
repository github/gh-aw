"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const http = require("node:http");
const net = require("node:net");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { randomUUID } = require("node:crypto");

const GATEWAY_OUTPUT_LIMIT = 64 * 1024;

function requireGatewayBinary(value) {
  assert.ok(typeof value === "string" && value.trim() && path.isAbsolute(value), "GH_AW_TEST_MCP_GATEWAY_BINARY must be an absolute path to the gateway executable");
  assert.ok(fs.statSync(value).isFile(), "GH_AW_TEST_MCP_GATEWAY_BINARY must identify a regular file");
  fs.accessSync(value, fs.constants.X_OK);
  return value;
}

class FixtureTimeoutError extends Error {}

async function withTimeout(operation, timeoutMs, label) {
  assert.ok(Number.isSafeInteger(timeoutMs) && timeoutMs > 0 && timeoutMs <= 2_147_483_647, "Fixture timeout must be a positive, representable millisecond timeout");
  let timer;
  try {
    return await Promise.race([
      Promise.resolve().then(operation),
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new FixtureTimeoutError(`${label} exceeded ${timeoutMs}ms`)), timeoutMs);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

function createGatewayOutput(routes) {
  const tails = { stdout: Buffer.alloc(0), stderr: Buffer.alloc(0) };
  const routesUsed = new Set();
  const methods = new Set();
  const rpcErrorCodes = new Set();
  function observe(text) {
    // WithSDKLogging runs after the real gateway's authentication middleware.
    for (const match of text.matchAll(/>>> SDK Request \[routed:([A-Za-z0-9_.-]+)\][^\r\n]*method=POST path=(\/mcp\/[A-Za-z0-9_.-]+)(?=\s)/g)) {
      if (match[2] === `/mcp/${match[1]}` && routes.includes(match[2])) routesUsed.add(match[2]);
    }
    for (const method of ["server/discover", "initialize", "notifications/initialized", "tools/list", "tools/call"]) {
      if (text.includes(`JSON-RPC Request: method=${method} id=`)) methods.add(method);
    }
    for (const match of text.matchAll(/JSON-RPC Error: code=(-?\d+) message=/g)) {
      if (rpcErrorCodes.size < 16) rpcErrorCodes.add(Number(match[1]));
    }
  }
  return {
    append(stream, chunk) {
      assert.ok(Object.hasOwn(tails, stream), "Unknown gateway output stream");
      const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
      for (let offset = 0; offset < bytes.length; offset += GATEWAY_OUTPUT_LIMIT) {
        const buffer = Buffer.concat([tails[stream], bytes.subarray(offset, offset + GATEWAY_OUTPUT_LIMIT)]);
        observe(buffer.toString("utf8"));
        tails[stream] = Buffer.from(buffer.subarray(-GATEWAY_OUTPUT_LIMIT));
      }
    },
    evidence: () => ({ routesUsed: [...routesUsed].sort(), methods: [...methods].sort(), rpcErrorCodes: [...rpcErrorCodes].sort((left, right) => left - right) }),
    diagnostics: () => `Gateway stdout (last ${GATEWAY_OUTPUT_LIMIT} bytes):\n${tails.stdout.toString("utf8")}\nGateway stderr (last ${GATEWAY_OUTPUT_LIMIT} bytes):\n${tails.stderr.toString("utf8")}`,
  };
}

async function reservePort() {
  const listener = net.createServer();
  await new Promise((resolve, reject) => {
    listener.once("error", reject);
    listener.listen(0, "127.0.0.1", resolve);
  });
  const { port } = listener.address();
  await new Promise((resolve, reject) => listener.close(error => (error ? reject(error) : resolve())));
  return port;
}

async function requestGateway(url, headers, timeoutMs) {
  let timer;
  try {
    return await new Promise((resolve, reject) => {
      const request = http.get(url, { headers, agent: false }, response => {
        const chunks = [];
        let bytes = 0;
        response.on("data", chunk => {
          bytes += chunk.length;
          if (bytes > 32 * 1024) {
            const error = new Error("Gateway probe response exceeds 32 KiB");
            reject(error);
            request.destroy(error);
          } else {
            chunks.push(chunk);
          }
        });
        response.once("error", reject);
        response.once("end", () => resolve({ status: response.statusCode, body: Buffer.concat(chunks).toString("utf8") }));
      });
      request.once("error", reject);
      timer = setTimeout(() => {
        const error = new Error("Gateway probe timed out");
        error.code = "ETIMEDOUT";
        request.destroy(error);
      }, timeoutMs);
    });
  } finally {
    clearTimeout(timer);
  }
}

async function startMCPGateway({ binary, scratch, mcpServers, env, startupTimeoutMs = 30_000, shutdownTimeoutMs = 3_000, spawnImpl = spawn }) {
  requireGatewayBinary(binary);
  assert.ok(Number.isInteger(startupTimeoutMs) && startupTimeoutMs > 0 && startupTimeoutMs <= 30_000, "Gateway startup timeout must be between 1 and 30000ms");
  assert.ok(Number.isInteger(shutdownTimeoutMs) && shutdownTimeoutMs > 0 && shutdownTimeoutMs <= 3_000, "Gateway shutdown timeout must be between 1 and 3000ms");
  const routes = Object.keys(mcpServers).map(name => `/mcp/${name}`);
  assert.ok(routes.length > 0);
  for (const [name, server] of Object.entries(mcpServers)) {
    assert.match(name, /^[A-Za-z0-9_][A-Za-z0-9_.-]*$/);
    assert.ok(
      Object.keys(server).every(key => ["type", "url", "headers"].includes(key)),
      "Gateway fixture accepts only HTTP upstream configuration, without aliases or tool filters"
    );
    const url = new URL(server.url);
    assert.ok(server.type === "http" && url.protocol === "http:" && url.hostname === "127.0.0.1" && !url.username && !url.password, "Gateway fixture upstreams must be loopback HTTP servers");
  }
  const directory = fs.mkdtempSync(path.join(scratch, "gateway-"));
  const output = createGatewayOutput(routes);
  const token = `fixture-gateway-${randomUUID()}`;
  const headers = Object.freeze({ Authorization: token, "X-Agent-ID": token });
  const authRejections = {};
  let child;
  let closed = false;
  let exited = false;
  let processError;
  let closedPromise;
  let stopping;
  let forcedStop = false;
  let health;
  function assertRunning() {
    if (processError) throw new Error(`Gateway process failed: ${processError.message}`, { cause: processError });
    if (exited || closed) throw new Error(`Gateway exited before fixture completion (code=${child.exitCode}, signal=${child.signalCode})`);
  }
  function stop() {
    if (!stopping) {
      stopping = (async () => {
        if (child && !closed) {
          // ChildProcess.kill targets only this owned PID, including on Windows.
          if (child.pid !== undefined && !exited) child.kill("SIGTERM");
          try {
            await withTimeout(() => closedPromise, shutdownTimeoutMs, "Gateway shutdown");
          } catch (error) {
            if (!(error instanceof FixtureTimeoutError)) throw error;
            if (exited) throw new Error("Gateway exited but its output pipes did not close", { cause: error });
            forcedStop = true;
            child.kill("SIGKILL");
            await withTimeout(() => closedPromise, shutdownTimeoutMs, "Gateway forced shutdown");
          }
        }
        fs.rmSync(directory, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
      })();
    }
    return stopping;
  }
  try {
    const deadline = Date.now() + startupTimeoutMs;
    let lastHealth = "no health response";
    function remaining() {
      const milliseconds = deadline - Date.now();
      if (milliseconds <= 0) throw new Error(`Gateway startup exceeded ${startupTimeoutMs}ms: ${lastHealth}`);
      return milliseconds;
    }
    const port = await reservePort();
    const address = `http://127.0.0.1:${port}`;
    const directories = Object.fromEntries(["logs", "payloads", "wasm-cache", "tmp"].map(name => [name, path.join(directory, name)]));
    for (const filename of Object.values(directories)) fs.mkdirSync(filename);
    remaining();
    child = spawnImpl(
      binary,
      ["--config-stdin", "--routed", "--listen", `127.0.0.1:${port}`, "--log-dir", directories.logs, "--payload-dir", directories.payloads, "--wasm-cache-dir", directories["wasm-cache"], "--shutdown-timeout", "2s"],
      {
        cwd: directory,
        env: { ...env, TEMP: directories.tmp, TMP: directories.tmp, TMPDIR: directories.tmp, DEBUG: "server:sdk-frontend,server:auth", DEBUG_COLORS: "0" },
        stdio: ["pipe", "pipe", "pipe"],
        shell: false,
        detached: false,
        windowsHide: true,
      }
    );
    const recordError = error => {
      processError ??= error;
    };
    child.on("error", recordError);
    child.once("exit", () => {
      exited = true;
    });
    closedPromise = new Promise(resolve => {
      child.once("close", () => {
        closed = true;
        resolve();
      });
    });
    for (const stream of ["stdout", "stderr"]) {
      child[stream].on("data", chunk => output.append(stream, chunk));
      child[stream].on("error", recordError);
    }
    child.stdin.on("error", recordError);
    child.stdin.end(JSON.stringify({ mcpServers, gateway: { port, domain: "localhost", agentId: token } }));
    while (true) {
      assertRunning();
      try {
        const response = await requestGateway(`${address}/health`, {}, Math.min(1_000, remaining()));
        lastHealth = `HTTP ${response.status}: ${response.body}`;
        if (response.status === 200) {
          const candidate = JSON.parse(response.body);
          if (candidate.status === "healthy" && typeof candidate.gatewayVersion === "string" && Object.keys(mcpServers).every(name => Object.hasOwn(candidate.servers ?? {}, name))) {
            health = candidate;
            break;
          }
        }
      } catch (error) {
        if (!["ECONNREFUSED", "ECONNRESET", "ETIMEDOUT"].includes(error.code)) throw error;
        lastHealth = error.message;
      }
      assertRunning();
      await new Promise(resolve => setTimeout(resolve, Math.min(50, remaining())));
    }
    // Probe only rejected requests: discovery and initialization must come entirely from the native SDK.
    for (const route of routes) {
      authRejections[route] = {};
      for (const [name, authorization] of [
        ["missing", undefined],
        ["invalid", `${token}-invalid`],
        ["bearer", `Bearer ${token}`],
      ]) {
        const probeHeaders = { "X-Agent-ID": token };
        if (authorization !== undefined) probeHeaders.Authorization = authorization;
        const response = await requestGateway(`${address}${route}`, probeHeaders, Math.min(1_000, remaining()));
        assert.equal(response.status, 401, `${route} must reject ${name} Authorization`);
        authRejections[route][name] = response.status;
      }
    }
    assertRunning();
    return {
      address,
      headers,
      directory,
      assertRunning,
      stop,
      diagnostics: output.diagnostics,
      evidence: () => ({ binary, version: health.gatewayVersion, ...output.evidence(), authRejections, stopped: closed, forcedStop }),
    };
  } catch (error) {
    try {
      await stop();
    } catch (cleanupError) {
      throw new AggregateError([error, cleanupError], `${error.message}; gateway cleanup failed: ${cleanupError.message}\n${output.diagnostics()}`);
    }
    throw new Error(`${error.message}\n${output.diagnostics()}`, { cause: error });
  }
}

module.exports = { GATEWAY_OUTPUT_LIMIT, requireGatewayBinary, withTimeout, createGatewayOutput, startMCPGateway };
