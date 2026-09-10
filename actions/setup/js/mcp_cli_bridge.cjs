// @ts-check
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * mcp_cli_bridge.cjs
 *
 * @safe-outputs-exempt SEC-004: "body" references are transport payloads/responses, not user-authored comment bodies
 *
 * Node.js bridge that handles MCP session protocol for CLI wrapper scripts.
 * Each CLI wrapper is a thin bash script that invokes this bridge with the
 * server configuration and user-provided command + arguments.
 *
 * Protocol flow: initialize → notifications/initialized → (periodic ping) → tools/call
 *
 * Operation metadata is logged via core.* (shim.cjs) and appended as JSONL
 * entries to /tmp/gh-aw/mcp-cli-audit/<server>.jsonl for auditing. Audit logs
 * omit payloads and are removed after 24 hours.
 *
 * Usage (called by generated CLI wrappers):
 *   node mcp_cli_bridge.cjs \
 *     --server-name <name> --server-url <url> \
 *     --tools-file <path> --api-key <key> \
 *     [<command> [--param value ...]]
 *   node mcp_cli_bridge.cjs \
 *     --server-name <name> --server-url <url> \
 *     --tools-file <path> --api-key <key> \
 *     --help
 *   node mcp_cli_bridge.cjs \
 *     --server-name <name> --server-url <url> \
 *     --tools-file <path> --api-key <key> \
 *     <command> --help
 */

require("./shim.cjs");

const fs = require("fs");
const path = require("path");
const http = require("http");
const { renderToolRecommendedExample, renderToolSignature, summarizeHelpText } = require("./mcp_cli_schema_docs.cjs");

/** Directory for JSONL audit logs (writable inside AWF sandbox via /tmp mount) */
const AUDIT_LOG_DIR = "/tmp/gh-aw/mcp-cli-audit";
const AUDIT_LOG_RETENTION_MS = 24 * 60 * 60 * 1000;
const SAFE_AUDIT_FIELDS = new Set(["event", "tool", "statusCode", "hasSession", "elapsedMs", "totalElapsedMs", "pingId", "intervalMs", "pid", "toolCount", "argumentBytes", "responseBytes"]);

/** Default timeout (ms) for HTTP calls to the local MCP gateway */
const DEFAULT_HTTP_TIMEOUT_MS = 15000;

/** Timeout (ms) for tool invocation calls (may be long-running) */
const TOOL_CALL_TIMEOUT_MS = 120000;
const TOOLS_LIST_REQUEST_ID = 3;
/** Default run count for logs MCP calls when count is not provided (mirrors server default) */
const LOGS_TOOL_DEFAULT_COUNT = 100;
/** Number of runs per timeout minute for logs auto-scaling (mirrors server: ceil(count/40)) */
const LOGS_TOOL_RUNS_PER_TIMEOUT_MINUTE = 40;
/** Minimum fallback timeout (minutes) for unfiltered logs calls (no workflow_name) */
const LOGS_TOOL_MIN_TIMEOUT_MINUTES_NO_FILTER = 5;
/** Maximum allowed explicit timeout (minutes) for logs calls to prevent bridge-resource exhaustion */
const LOGS_TOOL_MAX_EXPLICIT_TIMEOUT_MINUTES = 60;
/** Extra time (ms) to allow response marshalling/transport after tool execution */
const TOOL_CALL_TIMEOUT_BUFFER_MS = 15000;
const ENCLAVE_BIT_BUDGET_HINT = "Hint: the enclave response schema exceeded the finite-disclosure bit budget. Retry only with a lower-cardinality response schema.";

/** Timeout (ms) for the notifications/initialized handshake step */
const NOTIFY_TIMEOUT_MS = 10000;

/** Interval (ms) for MCP keepalive pings during long-running tool calls */
const KEEPALIVE_PING_INTERVAL_MS = 10000;

/** Starting JSON-RPC ID for keepalive ping requests */
const KEEPALIVE_PING_ID_START = 1000;

/** Preferred max lines for generated CLI help output */
const TOP_HELP_MAX_LINES = 20;
const TOOL_HELP_MAX_LINES = 30;
const TOOL_DESC_MAX_LEN = 90;
const COMPACT_NAME_LINE_TARGET_WIDTH = 110;
const SAFEOUTPUTS_SERVER_NAME = "safeoutputs";
const AWF_ENCLAVE_SERVER_NAME = "awf-enclave";
const DEFERRED_SERVERS_ENV = "GH_AW_MCP_DEFERRED_SERVERS";
const DEFERRED_TOOLS_LIST_MAX_ATTEMPTS = 5;
const DEFERRED_TOOLS_LIST_RETRY_DELAY_MS = 1000;
/** @type {Set<string>} */
const deferredToolsRefreshExhaustedServers = new Set();

// ---------------------------------------------------------------------------
// Audit logging
// ---------------------------------------------------------------------------

/**
 * Ensure the JSONL audit log directory exists and remove expired records.
 * @param {string} [auditDir]
 */
function ensureAuditDir(auditDir = AUDIT_LOG_DIR) {
  try {
    fs.mkdirSync(auditDir, { recursive: true, mode: 0o700 });
    fs.chmodSync(auditDir, 0o700);
    const cutoff = Date.now() - AUDIT_LOG_RETENTION_MS;
    for (const filename of fs.readdirSync(auditDir)) {
      const logPath = path.join(auditDir, filename);
      const stat = fs.lstatSync(logPath);
      if (stat.isFile() && filename.endsWith(".jsonl") && stat.mtimeMs < cutoff) {
        fs.rmSync(logPath);
      }
    }
  } catch (err) {
    const core = global.core;
    core.warning(`Failed to prepare audit log directory ${auditDir}: ${getErrorMessage(err)}`);
  }
}

/**
 * Retain only non-payload fields that are safe and useful for diagnostics.
 * @param {Record<string, unknown>} entry
 * @returns {Record<string, string | number | boolean>}
 */
function sanitizeAuditEntry(entry) {
  /** @type {Record<string, string | number | boolean>} */
  const safeEntry = {};
  for (const [key, value] of Object.entries(entry)) {
    if (SAFE_AUDIT_FIELDS.has(key) && (typeof value === "string" || typeof value === "number" || typeof value === "boolean")) {
      safeEntry[key] = value;
    }
  }
  return safeEntry;
}

/**
 * Return the serialized UTF-8 size of a value without exposing its content.
 * @param {unknown} value
 * @returns {number}
 */
function serializedSize(value) {
  try {
    return Buffer.byteLength(JSON.stringify(value) ?? "");
  } catch {
    return 0;
  }
}

/**
 * Append a JSONL entry to the audit log for a given server.
 *
 * @param {string} serverName - Server name (used as filename prefix)
 * @param {Record<string, unknown>} entry - Log entry object
 * @param {string} [auditDir]
 */
function auditLog(serverName, entry, auditDir = AUDIT_LOG_DIR) {
  let fd;
  try {
    const safeServerName = serverName.replace(/[^a-zA-Z0-9._-]/g, "_");
    const logPath = path.join(auditDir, `${safeServerName}.jsonl`);
    const record = {
      timestamp: new Date().toISOString(),
      server: serverName,
      ...sanitizeAuditEntry(entry),
    };
    fd = fs.openSync(logPath, fs.constants.O_APPEND | fs.constants.O_CREAT | fs.constants.O_WRONLY | fs.constants.O_NOFOLLOW, 0o600);
    fs.fchmodSync(fd, 0o600);
    fs.writeSync(fd, JSON.stringify(record) + "\n");
  } catch (err) {
    const core = global.core;
    core.warning(`Failed to write audit log for ${serverName}: ${getErrorMessage(err)}`);
  } finally {
    if (fd !== undefined) fs.closeSync(fd);
  }
}

// ---------------------------------------------------------------------------
// I/O helpers
// ---------------------------------------------------------------------------

/**
 * Write data to process.stdout and return a Promise that resolves only after
 * the data has been fully flushed to the OS.
 *
 * When stdout is a pipe and the payload exceeds the OS pipe buffer (~64 KiB on
 * Linux), `process.stdout.write()` returns `false` — the first chunk is written
 * to the OS immediately but the remainder is queued in Node.js's internal
 * buffer.  Any synchronous write to process.stderr that follows (e.g. a
 * `core.info` call) will reach the OS *before* the buffered stdout tail is
 * flushed, which corrupts the output when the caller captures both streams
 * together (e.g. `2>&1`).
 *
 * Awaiting the `drain` event ensures stdout is fully drained before any
 * subsequent diagnostic logging.
 *
 * @param {string} data
 * @returns {Promise<void>}
 */
function writeStdoutAndFlush(data) {
  return new Promise((resolve, reject) => {
    const flushed = process.stdout.write(data);
    if (flushed) {
      resolve();
    } else {
      const onDrain = () => {
        process.stdout.removeListener("error", onError);
        resolve();
      };
      const onError = err => {
        process.stdout.removeListener("drain", onDrain);
        reject(err);
      };
      process.stdout.once("drain", onDrain);
      process.stdout.once("error", onError);
    }
  });
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

/**
 * Make an HTTP POST request with a JSON body and return the parsed response.
 *
 * @param {string} urlStr - Full URL to POST to
 * @param {Record<string, string>} headers - Request headers
 * @param {unknown} body - Request body (will be JSON-serialized)
 * @param {number} [timeoutMs] - Request timeout in milliseconds
 * @returns {Promise<{statusCode: number, body: unknown, headers: Record<string, string | string[] | undefined>}>}
 */
function httpPostJSON(urlStr, headers, body, timeoutMs = DEFAULT_HTTP_TIMEOUT_MS) {
  return new Promise((resolve, reject) => {
    let parsedUrl;
    try {
      parsedUrl = new URL(urlStr);
    } catch {
      reject(new Error(`Invalid URL: ${urlStr}`));
      return;
    }
    const bodyStr = JSON.stringify(body);

    const options = {
      hostname: parsedUrl.hostname,
      port: parsedUrl.port || 80,
      path: parsedUrl.pathname + parsedUrl.search,
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json, text/event-stream",
        "Content-Length": Buffer.byteLength(bodyStr),
        ...headers,
      },
    };

    const req = http.request(options, res => {
      let data = "";
      res.on("error", reject);
      res.on("data", chunk => {
        data += chunk;
      });
      res.on("end", () => {
        let parsed;
        try {
          parsed = JSON.parse(data);
        } catch {
          parsed = data;
        }
        resolve({
          statusCode: res.statusCode || 0,
          body: parsed,
          headers: /** @type {Record<string, string | string[] | undefined>} */ res.headers,
        });
      });
    });

    req.on("error", err => reject(err));

    req.setTimeout(timeoutMs, () => {
      req.destroy();
      reject(new Error(`HTTP request timed out after ${timeoutMs}ms`));
    });

    req.write(bodyStr);
    req.end();
  });
}

// ---------------------------------------------------------------------------
// MCP session protocol
// ---------------------------------------------------------------------------

/**
 * Perform the MCP initialize handshake and return the session ID (if any).
 *
 * @param {string} serverUrl - HTTP URL of the MCP server endpoint
 * @param {string} apiKey - Bearer token for gateway authentication
 * @param {string} serverName - Server name (for logging/auditing)
 * @returns {Promise<string>} Session ID or empty string
 */
async function mcpInitialize(serverUrl, apiKey, serverName) {
  const core = global.core;
  const startMs = Date.now();
  core.info(`[${serverName}] MCP initialize: POST ${serverUrl}`);

  auditLog(serverName, { event: "initialize_start", url: serverUrl });

  try {
    const resp = await httpPostJSON(
      serverUrl,
      { Authorization: apiKey },
      {
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: {
          capabilities: {},
          clientInfo: { name: "mcp-cli-bridge", version: "1.0.0" },
          protocolVersion: "2024-11-05",
        },
      },
      DEFAULT_HTTP_TIMEOUT_MS
    );

    const sessionId = typeof resp.headers["mcp-session-id"] === "string" ? resp.headers["mcp-session-id"] : "";
    const elapsedMs = Date.now() - startMs;

    core.info(`[${serverName}] MCP initialize: status=${resp.statusCode}, sessionId=${sessionId ? sessionId.slice(0, 8) + "..." : "(none)"}, elapsed=${elapsedMs}ms`);

    auditLog(serverName, {
      event: "initialize_done",
      statusCode: resp.statusCode,
      hasSession: !!sessionId,
      elapsedMs,
    });

    return sessionId;
  } catch (err) {
    const elapsedMs = Date.now() - startMs;
    const message = getErrorMessage(err);
    core.warning(`[${serverName}] MCP initialize failed (${elapsedMs}ms): ${message}`);
    auditLog(serverName, { event: "initialize_error", error: message, elapsedMs });
    return "";
  }
}

/**
 * Send the notifications/initialized message to complete the MCP handshake.
 *
 * @param {string} serverUrl - HTTP URL of the MCP server endpoint
 * @param {string} apiKey - Bearer token for gateway authentication
 * @param {string} sessionId - Session ID from initialize (may be empty)
 * @param {string} serverName - Server name (for logging/auditing)
 * @returns {Promise<void>}
 */
async function mcpNotifyInitialized(serverUrl, apiKey, sessionId, serverName) {
  const core = global.core;
  const startMs = Date.now();
  core.info(`[${serverName}] MCP notifications/initialized`);

  auditLog(serverName, { event: "notify_initialized_start" });

  /** @type {Record<string, string>} */
  const headers = { Authorization: apiKey };
  if (sessionId) {
    headers["Mcp-Session-Id"] = sessionId;
  }

  try {
    await httpPostJSON(serverUrl, headers, { jsonrpc: "2.0", method: "notifications/initialized", params: {} }, NOTIFY_TIMEOUT_MS);
    const elapsedMs = Date.now() - startMs;
    core.info(`[${serverName}] MCP notifications/initialized: done (${elapsedMs}ms)`);
    auditLog(serverName, { event: "notify_initialized_done", elapsedMs });
  } catch (err) {
    const elapsedMs = Date.now() - startMs;
    const message = getErrorMessage(err);
    core.warning(`[${serverName}] MCP notifications/initialized failed (${elapsedMs}ms): ${message}`);
    auditLog(serverName, { event: "notify_initialized_error", error: message, elapsedMs });
  }
}

/**
 * Call a tool via the MCP tools/call method.
 *
 * @param {string} serverUrl - HTTP URL of the MCP server endpoint
 * @param {string} apiKey - Bearer token for gateway authentication
 * @param {string} sessionId - Session ID from initialize (may be empty)
 * @param {string} toolName - Name of the tool to call
 * @param {Record<string, unknown>} toolArgs - Tool arguments
 * @param {string} serverName - Server name (for logging/auditing)
 * @returns {Promise<{statusCode: number, body: unknown}>}
 */
async function mcpToolsCall(serverUrl, apiKey, sessionId, toolName, toolArgs, serverName) {
  const core = global.core;
  const startMs = Date.now();
  const argumentBytes = serializedSize(toolArgs);
  core.info(`[${serverName}] MCP tools/call: tool=${toolName}, argumentBytes=${argumentBytes}`);

  auditLog(serverName, {
    event: "tools_call_start",
    tool: toolName,
    argumentBytes,
  });

  /** @type {Record<string, string>} */
  const headers = { Authorization: apiKey };
  if (sessionId) {
    headers["Mcp-Session-Id"] = sessionId;
  }

  const callTimeoutMs = getToolCallTimeoutMs(toolName, toolArgs);
  const resp = await httpPostJSON(
    serverUrl,
    headers,
    {
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: { name: toolName, arguments: toolArgs },
    },
    callTimeoutMs
  );

  const elapsedMs = Date.now() - startMs;
  core.info(`[${serverName}] MCP tools/call: status=${resp.statusCode}, elapsed=${elapsedMs}ms`);

  auditLog(serverName, {
    event: "tools_call_done",
    tool: toolName,
    statusCode: resp.statusCode,
    elapsedMs,
    responseBytes: serializedSize(resp.body),
  });

  return resp;
}

/**
 * Resolve MCP bridge timeout for a tool call.
 *
 * The logs tool may legitimately run for multiple minutes. The bridge always
 * computes a count-derived floor that mirrors the server's own auto-scaling
 * (`ceil(count / 40)` minutes, with a 5-minute minimum when no `workflow_name`
 * filter is provided, or when an `engine` filter is provided — matching
 * `effectiveMCPLogsToolTimeoutMinutes` in
 * `mcp_tools_privileged.go`). This floor applies to both the implicit path and
 * any explicit `timeout` argument, so callers that pass a small positive value
 * still receive a bridge deadline that is at least as long as the server's own
 * expected runtime. Explicit timeouts are capped at
 * `LOGS_TOOL_MAX_EXPLICIT_TIMEOUT_MINUTES` to prevent bridge-resource
 * exhaustion from adversarial or misconfigured calls. The result is always
 * at least `TOOL_CALL_TIMEOUT_MS` (120 s).
 *
 * @param {string} toolName
 * @param {Record<string, unknown>} toolArgs
 * @returns {number} Timeout in ms. Always at least TOOL_CALL_TIMEOUT_MS (120 s).
 */
function getToolCallTimeoutMs(toolName, toolArgs) {
  if (toolName !== "logs") {
    return TOOL_CALL_TIMEOUT_MS;
  }

  // Compute the count-derived floor that mirrors the server's auto-scaling.
  // Reject non-numeric types to avoid implicit coercion of booleans/strings.
  const countCandidate = typeof toolArgs?.count === "number" ? toolArgs.count : NaN;
  const effectiveCount = Number.isFinite(countCandidate) && countCandidate > 0 ? countCandidate : LOGS_TOOL_DEFAULT_COUNT;
  const baseMinutes = Math.ceil(effectiveCount / LOGS_TOOL_RUNS_PER_TIMEOUT_MINUTE);
  // Without a workflow filter, or when filtering by engine, the GitHub API scans
  // all runs and is substantially slower for large repositories — apply the same
  // 5-minute floor as the server.
  const floorMinutes = toolArgs?.workflow_name && !toolArgs?.engine ? baseMinutes : Math.max(LOGS_TOOL_MIN_TIMEOUT_MINUTES_NO_FILTER, baseMinutes);
  const floorMs = Math.ceil(floorMinutes * 60 * 1000) + TOOL_CALL_TIMEOUT_BUFFER_MS;

  // Honor an explicit timeout when present. Reject non-numeric types (e.g.
  // booleans, strings) so only real numeric values are accepted, and cap at
  // LOGS_TOOL_MAX_EXPLICIT_TIMEOUT_MINUTES to bound bridge-resource use.
  const timeoutCandidate = typeof toolArgs?.timeout === "number" ? toolArgs.timeout : NaN;
  if (Number.isFinite(timeoutCandidate) && timeoutCandidate > 0) {
    const clampedMinutes = Math.min(timeoutCandidate, LOGS_TOOL_MAX_EXPLICIT_TIMEOUT_MINUTES);
    const explicitMs = Math.ceil(clampedMinutes * 60 * 1000) + TOOL_CALL_TIMEOUT_BUFFER_MS;
    // Floor to the count-derived minimum so tiny explicit values do not produce
    // a misleadingly short bridge deadline; always at least TOOL_CALL_TIMEOUT_MS.
    return Math.max(TOOL_CALL_TIMEOUT_MS, floorMs, explicitMs);
  }

  // No valid explicit timeout: use the count-derived floor directly.
  return Math.max(TOOL_CALL_TIMEOUT_MS, floorMs);
}

/**
 * Start periodic MCP ping requests to keep a session alive while a tool call runs.
 *
 * @param {string} serverUrl - HTTP URL of the MCP server endpoint
 * @param {string} apiKey - Bearer token for gateway authentication
 * @param {string} sessionId - Session ID from initialize (required for keepalive)
 * @param {string} serverName - Server name (for logging/auditing)
 * @returns {() => void} Stop function to clear the ping timer
 */
function startMcpKeepalivePings(serverUrl, apiKey, sessionId, serverName) {
  const core = global.core;

  if (!sessionId) {
    core.warning(`[${serverName}] MCP keepalive disabled: no sessionId`);
    return () => {};
  }

  /** @type {Record<string, string>} */
  const headers = {
    Authorization: apiKey,
    "Mcp-Session-Id": sessionId,
  };

  let stopped = false;
  let pingId = KEEPALIVE_PING_ID_START;
  /** @type {NodeJS.Timeout | null} */
  let nextTimer = null;

  const runPing = async () => {
    if (stopped) {
      return;
    }
    const startMs = Date.now();
    const currentPingId = pingId++;

    try {
      await httpPostJSON(
        serverUrl,
        headers,
        {
          jsonrpc: "2.0",
          id: currentPingId,
          method: "ping",
        },
        DEFAULT_HTTP_TIMEOUT_MS
      );

      const elapsedMs = Date.now() - startMs;
      core.info(`[${serverName}] MCP keepalive ping: id=${currentPingId}, elapsed=${elapsedMs}ms`);
      auditLog(serverName, { event: "keepalive_ping_done", pingId: currentPingId, elapsedMs });
    } catch (err) {
      const elapsedMs = Date.now() - startMs;
      const message = getErrorMessage(err);
      core.warning(`[${serverName}] MCP keepalive ping failed: ${message}`);
      auditLog(serverName, { event: "keepalive_ping_error", pingId: currentPingId, error: message, elapsedMs });
    }
    if (!stopped) {
      nextTimer = setTimeout(runPing, KEEPALIVE_PING_INTERVAL_MS);
    }
  };

  nextTimer = setTimeout(runPing, KEEPALIVE_PING_INTERVAL_MS);

  auditLog(serverName, { event: "keepalive_started", intervalMs: KEEPALIVE_PING_INTERVAL_MS });
  core.info(`[${serverName}] MCP keepalive started (interval=${KEEPALIVE_PING_INTERVAL_MS}ms)`);

  return () => {
    if (stopped) {
      return;
    }
    stopped = true;
    if (nextTimer) {
      clearTimeout(nextTimer);
      nextTimer = null;
    }
    auditLog(serverName, { event: "keepalive_stopped" });
    core.info(`[${serverName}] MCP keepalive stopped`);
  };
}

// ---------------------------------------------------------------------------
// CLI argument parsing
// ---------------------------------------------------------------------------

/**
 * Parse the bridge's own arguments from process.argv.
 * Bridge args (--server-name, --server-url, etc.) come before the user command.
 *
 * @param {string[]} argv - process.argv (includes node path and script path)
 * @returns {{serverName: string, serverUrl: string, toolsFile: string, apiKey: string, userArgs: string[]}}
 */
function parseBridgeArgs(argv) {
  // Skip first two entries (node binary + script path)
  const args = argv.slice(2);

  let serverName = "";
  let serverUrl = "";
  let toolsFile = "";
  let apiKey = "";
  let userArgsStart = -1;

  // Bridge args are always paired: --flag value
  // The first argument that doesn't match a known bridge flag marks the start of user args
  const bridgeFlags = new Set(["--server-name", "--server-url", "--tools-file", "--api-key"]);

  for (let i = 0; i < args.length; i++) {
    if (bridgeFlags.has(args[i]) && i + 1 < args.length) {
      switch (args[i]) {
        case "--server-name":
          serverName = args[++i];
          break;
        case "--server-url":
          serverUrl = args[++i];
          break;
        case "--tools-file":
          toolsFile = args[++i];
          break;
        case "--api-key":
          apiKey = args[++i];
          break;
      }
    } else {
      userArgsStart = i;
      break;
    }
  }

  const userArgs = userArgsStart >= 0 ? args.slice(userArgsStart) : [];
  return { serverName, serverUrl, toolsFile, apiKey, userArgs };
}

/**
 * Check whether stdin should be read for tool arguments.
 * Returns true when:
 * - The '.' sentinel is the only argument (JSON payload mode — full args from stdin), or
 * - No arguments are provided and stdin is not connected to a terminal (piped JSON payload), or
 * - Any '--key .' or '--key=.' pair is present (per-field stdin mode — raw text for that field).
 *
 * This enables agents to pipe content in multiple ways:
 *   printf '{"issue_number":42,"body":"hello"}' | safeoutputs add_comment .
 *   printf '{"issue_number":42,"body":"hello"}' | safeoutputs add_comment
 *   printf 'Long issue body...' | safeoutputs create_issue --title "Bug" --body .
 *
 * @param {string[]} args - User arguments after the tool name
 * @returns {boolean}
 */
function hasStdinJsonPayload(args) {
  if (args.length === 1 && args[0] === ".") return true;
  if (args.length === 0 && !process.stdin.isTTY) return true;
  // Per-field stdin marker: --key . (space-separated) or --key=. (equals-separated)
  for (let i = 0; i < args.length; i++) {
    if (args[i].startsWith("--")) {
      const raw = args[i].slice(2);
      const eqIdx = raw.indexOf("=");
      if (eqIdx >= 0 && raw.slice(eqIdx + 1) === ".") return true;
      if (eqIdx < 0 && i + 1 < args.length && args[i + 1] === ".") return true;
    }
  }
  return false;
}

/** Maximum bytes accepted from stdin to prevent memory exhaustion (10 MB) */
const STDIN_MAX_BYTES = 10 * 1024 * 1024;

/**
 * Read all of stdin synchronously and return the content as a string.
 * Uses low-level fs.readSync on fd 0 so it works in both TTY and piped contexts.
 * Throws an error if stdin exceeds STDIN_MAX_BYTES or if a read error occurs
 * after bytes have already been collected (to prevent silently returning partial content).
 * Returns an empty string if stdin is empty or if an error occurs before any bytes are read.
 *
 * @returns {string}
 */
function readStdinSync() {
  const STDIN_FD = 0;
  /** @type {Buffer[]} */
  const chunks = [];
  const bufSize = 65536;
  let totalBytes = 0;
  while (true) {
    const buf = Buffer.alloc(bufSize);
    let bytesRead;
    try {
      bytesRead = fs.readSync(STDIN_FD, buf, 0, bufSize, null);
    } catch (err) {
      // If we have already read some bytes, rethrow so the caller doesn't
      // unknowingly use partial content. An error before any data is read
      // (e.g. stdin is not connected) is treated as empty input.
      if (totalBytes > 0) {
        throw err;
      }
      return "";
    }
    if (bytesRead === 0) break;
    totalBytes += bytesRead;
    if (totalBytes > STDIN_MAX_BYTES) {
      throw new Error(`stdin input exceeds maximum allowed size of ${STDIN_MAX_BYTES} bytes`);
    }
    chunks.push(buf.slice(0, bytesRead));
  }
  return Buffer.concat(chunks).toString("utf8");
}

/**
 * Try to extract a specific field value from a JSON object string.
 *
 * When per-field stdin mode (`--key .`) is used and stdin contains a JSON
 * object, this function checks whether that object has the target key.  If so,
 * it returns the field value, allowing agents to pipe a full JSON payload and
 * extract individual fields without the entire JSON string ending up as the
 * field value.
 *
 * Example: an agent may construct a full payload JSON and still pass individual
 * fields via `--key .`:
 *   printf '{"title":"Fix bug","body":"Details here"}' \
 *     | safeoutputs create_pull_request --title "Fix bug" --body .
 * Without this helper the body would be the raw JSON string.  With it, the
 * body is correctly extracted as "Details here".
 *
 * Returns `undefined` when stdin is not a JSON object, when the key is absent
 * from the object, or when JSON parsing fails — in all those cases the caller
 * falls back to using the raw stdin content.
 *
 * @param {string} trimmedStdin - Pre-trimmed stdin content.  Must have no
 *   leading whitespace — the function uses a `startsWith('{')` fast-path to
 *   avoid parsing non-JSON input, so leading whitespace will cause a JSON
 *   object to be treated as non-JSON and return `undefined`.  All callers
 *   must pass `stdinContent.trim()` before invoking this function.
 * @param {string} canonicalKey - Canonical schema key to look up
 * @param {Record<string, {type?: string|string[]}>} schemaProperties - Tool input schema properties
 * @param {Map<string, string>} normalizedSchemaKeyMap - Map from normalized key to canonical schema key
 * @param {Set<string>} ambiguousNormalizedSchemaKeys - Normalized keys that map to multiple schema keys
 * @returns {unknown} The extracted field value, or `undefined` if not found
 */
function tryExtractJsonFieldFromStdin(trimmedStdin, canonicalKey, schemaProperties, normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys) {
  if (!trimmedStdin.startsWith("{")) {
    return undefined;
  }
  try {
    const parsed = JSON.parse(trimmedStdin);
    if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
      return undefined;
    }
    // Try the canonical key first, then any schema-aliased form.
    if (Object.prototype.hasOwnProperty.call(parsed, canonicalKey)) {
      return parsed[canonicalKey];
    }
    // Search for a key in the JSON that resolves to the same canonical key.
    for (const jsonKey of Object.keys(parsed)) {
      const resolved = resolveSchemaPropertyKey(jsonKey, schemaProperties, normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys);
      if (resolved === canonicalKey) {
        return parsed[jsonKey];
      }
    }
    return undefined;
  } catch {
    return undefined;
  }
}

/**
 * Parse user-provided --key value pairs into a tool arguments object.
 * Supports both --key value and --key=value styles.
 * Boolean flags (--key without a value) are set to true.
 *
 * When `stdinContent` is provided and args is empty or `['.']`, the stdin
 * content is parsed as a JSON object and its properties are used as tool
 * arguments directly (JSON payload mode). This enables agents to pipe
 * complex multi-argument payloads without shell quoting issues:
 *   printf '{"issue_number":42,"body":"hello"}' | safeoutputs add_comment .
 *
 * When `stdinContent` is provided and non-empty, any '--key .' or '--key=.'
 * pair substitutes that field's value with stdin.  If stdin is a JSON object
 * that contains the target key, the matching field value is extracted from the
 * JSON instead of using the entire stdin string.  This prevents a common agent
 * mistake where the whole JSON payload ends up as a string field value:
 *   printf '{"title":"Fix bug","body":"Details"}' \
 *     | safeoutputs create_pull_request --title "Fix bug" --body .
 * When stdin is empty, the '.' is passed through as a literal value.  When
 * stdin is non-empty but the key is absent from the JSON (or stdin is not a
 * JSON object), the entire trimmed stdin string is used as the field value.
 *
 * @param {string[]} args - User arguments after the tool name
 * @param {Record<string, {type?: string|string[]}>} [schemaProperties] - Tool input schema properties
 * @param {string | null} [stdinContent] - Pre-read stdin content; used in JSON payload mode
 *   (args empty or `['.']`) and per-field stdin mode (`--key .`).
 * @returns {{args: Record<string, unknown>, json: boolean}}
 */
function parseToolArgs(args, schemaProperties = {}, stdinContent = null) {
  /** @type {Record<string, unknown>} */
  const result = {};
  let jsonOutput = false;
  const hasSchemaProperties = Object.keys(schemaProperties).length > 0;
  const { normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys } = buildNormalizedSchemaKeyMap(schemaProperties);
  // Trimmed stdin content used in both JSON payload mode and per-field stdin mode.
  const trimmedStdin = stdinContent !== null ? stdinContent.trim() : null;
  const isJsonPayloadMode = trimmedStdin !== null && (args.length === 0 || (args.length === 1 && args[0] === "."));
  const isExplicitJsonSentinel = args.length === 1 && args[0] === ".";

  // JSON payload mode: when args is empty or ['.'] and stdinContent is available,
  // parse stdin as a JSON object and use its properties directly as tool arguments.
  if (isJsonPayloadMode) {
    const hasRawStdinBytes = typeof stdinContent === "string" && stdinContent.length > 0;
    if (isExplicitJsonSentinel || hasRawStdinBytes) {
      let parsed;
      try {
        parsed = JSON.parse(trimmedStdin ?? "");
      } catch (err) {
        const parseError = getErrorMessage(err);
        throw new Error(`stdin is not valid JSON: ${parseError}. JSON payload mode was requested ${isExplicitJsonSentinel ? "with '.'" : "from piped stdin with no flags"}. Pass --key value flags instead, or correct the JSON.`);
      }
      if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
        for (const [key, value] of Object.entries(parsed)) {
          const canonicalKey = resolveSchemaPropertyKey(key, schemaProperties, normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys);
          result[canonicalKey] = value;
        }
        return { args: result, json: false };
      }
      throw new Error(`stdin JSON payload must be an object. JSON payload mode was requested ${isExplicitJsonSentinel ? "with '.'" : "from piped stdin with no flags"}. Pass --key value flags instead, or provide a JSON object.`);
    }
  }

  for (let i = 0; i < args.length; i++) {
    if (args[i].startsWith("--")) {
      const raw = args[i].slice(2);
      const eqIdx = raw.indexOf("=");
      if (eqIdx >= 0) {
        // --key=value style
        const key = raw.slice(0, eqIdx);
        if (key === "json") {
          jsonOutput = true;
        } else {
          const canonicalKey = resolveSchemaPropertyKey(key, schemaProperties, normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys);
          const rawValue = raw.slice(eqIdx + 1);
          if (rawValue === "." && trimmedStdin) {
            const extracted = tryExtractJsonFieldFromStdin(trimmedStdin, canonicalKey, schemaProperties, normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys);
            result[canonicalKey] = extracted !== undefined ? extracted : trimmedStdin;
          } else {
            result[canonicalKey] = coerceToolArgValue(canonicalKey, rawValue, schemaProperties[canonicalKey], result[canonicalKey], !hasSchemaProperties);
          }
        }
      } else if (raw === "json") {
        jsonOutput = true;
      } else if (i + 1 < args.length && !args[i + 1].startsWith("--")) {
        const canonicalKey = resolveSchemaPropertyKey(raw, schemaProperties, normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys);
        const rawValue = args[i + 1];
        if (rawValue === "." && trimmedStdin) {
          const extracted = tryExtractJsonFieldFromStdin(trimmedStdin, canonicalKey, schemaProperties, normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys);
          result[canonicalKey] = extracted !== undefined ? extracted : trimmedStdin;
        } else {
          result[canonicalKey] = coerceToolArgValue(canonicalKey, rawValue, schemaProperties[canonicalKey], result[canonicalKey], !hasSchemaProperties);
        }
        i++;
      } else {
        const canonicalKey = resolveSchemaPropertyKey(raw, schemaProperties, normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys);
        result[canonicalKey] = true;
      }
    }
    // Skip non-flag arguments
  }

  return { args: result, json: jsonOutput };
}

/**
 * Normalize a CLI argument/schema key by removing separators and lowercasing.
 *
 * @param {string} key - Raw CLI or schema key
 * @example
 * normalizeSchemaKey("issue-number")
 * // => "issuenumber"
 * @example
 * normalizeSchemaKey("issue_number")
 * // => "issuenumber"
 * @returns {string}
 */
function normalizeSchemaKey(key) {
  return key.replace(/[-_]/g, "").toLowerCase();
}

/**
 * Build a map from normalized key -> canonical schema key.
 *
 * @param {Record<string, {type?: string|string[]}>} schemaProperties - Tool input schema properties
 * @returns {{
 *   normalizedSchemaKeyMap: Map<string, string>,
 *   ambiguousNormalizedSchemaKeys: Set<string>
 * }} Object containing resolvable normalized keys and ambiguous normalized keys
 */
function buildNormalizedSchemaKeyMap(schemaProperties) {
  const normalizedSchemaKeyMap = new Map();
  const ambiguousNormalizedSchemaKeys = new Set();
  for (const key of Object.keys(schemaProperties)) {
    const normalized = normalizeSchemaKey(key);
    if (ambiguousNormalizedSchemaKeys.has(normalized)) {
      continue;
    }
    const existing = normalizedSchemaKeyMap.get(normalized);
    if (existing === undefined) {
      normalizedSchemaKeyMap.set(normalized, key);
    } else if (existing !== key) {
      ambiguousNormalizedSchemaKeys.add(normalized);
      normalizedSchemaKeyMap.delete(normalized);
    }
  }
  return { normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys };
}

/**
 * Resolve a user-provided CLI key to the canonical schema key when possible.
 * Falls back to the original key when no schema match exists.
 *
 * @param {string} key - User-provided CLI argument key (without leading `--`)
 * @param {Record<string, {type?: string|string[]}>} schemaProperties - Tool input schema properties
 * @param {Map<string, string>} normalizedSchemaKeyMap - Map from normalized key to canonical schema key
 * @param {Set<string>} ambiguousNormalizedSchemaKeys - Normalized keys that map to multiple schema keys
 * @returns {string}
 */
function resolveSchemaPropertyKey(key, schemaProperties, normalizedSchemaKeyMap, ambiguousNormalizedSchemaKeys) {
  if (Object.prototype.hasOwnProperty.call(schemaProperties, key)) {
    return key;
  }
  const normalized = normalizeSchemaKey(key);
  if (ambiguousNormalizedSchemaKeys.has(normalized)) {
    return key;
  }
  return normalizedSchemaKeyMap.get(normalized) || key;
}

const CLI_UNESCAPED_TEXT_ARG_KEYS = new Set(["body", "draftbody"]);

/**
 * Unescape a conservative subset of JSON-style escape sequences in a CLI text argument.
 *
 * This is only applied to body-like text fields where authors commonly expect
 * `\n` and similar escapes to become literal formatting characters, matching
 * JSON stdin mode more closely without mutating unrelated string arguments
 * such as file paths or regex patterns.
 *
 * Supported sequences:
 *   `\n`      → newline
 *   `\t`      → tab
 *   `\r`      → carriage return
 *   `\b`      → backspace
 *   `\f`      → form feed
 *   `\\`      → single backslash
 *   `\uXXXX`  → Unicode code point
 *   Any other `\X` is left unchanged (the backslash is preserved).
 *
 * @param {string} str - Raw CLI string argument
 * @returns {string} String with supported escape sequences replaced by literal characters
 */
function unescapeCliStringArg(str) {
  return str.replace(/\\(?:u([0-9a-fA-F]{4})|([\s\S]))/g, (match, hex, char) => {
    if (hex) {
      return String.fromCharCode(Number.parseInt(hex, 16));
    }
    switch (char) {
      case "n":
        return "\n";
      case "t":
        return "\t";
      case "r":
        return "\r";
      case "b":
        return "\b";
      case "f":
        return "\f";
      case "\\":
        return "\\";
      default:
        return match;
    }
  });
}

/**
 * @param {string} key
 * @returns {boolean}
 */
function shouldUnescapeCliTextArg(key) {
  return CLI_UNESCAPED_TEXT_ARG_KEYS.has(normalizeSchemaKey(key));
}

/**
 * Parse and coerce a CLI argument value based on the MCP tool schema property type.
 *
 * @param {string} key - Argument key name
 * @param {string} rawValue - Raw CLI value
 * @param {{type?: string|string[]}|undefined} schemaProperty - JSON schema property
 * @param {unknown} existingValue - Existing value (for repeated flags)
 * @param {boolean} [allowNumericFallback=false] - Allow numeric parsing when schema is unavailable
 * @returns {unknown}
 */
function coerceToolArgValue(key, rawValue, schemaProperty, existingValue, allowNumericFallback = false) {
  /** @type {string[]} */
  const types = [];
  if (schemaProperty && typeof schemaProperty === "object" && "type" in schemaProperty && schemaProperty.type != null) {
    if (Array.isArray(schemaProperty.type)) {
      for (const t of schemaProperty.type) {
        if (typeof t === "string") {
          types.push(t);
        }
      }
    } else if (typeof schemaProperty.type === "string") {
      types.push(schemaProperty.type);
    }
  }

  if (types.includes("array")) {
    /** @type {unknown[]} */
    let values;
    const trimmed = rawValue.trim();
    if (trimmed.startsWith("[")) {
      try {
        const parsed = JSON.parse(trimmed);
        if (Array.isArray(parsed)) {
          values = parsed;
        } else {
          values = [rawValue];
        }
      } catch {
        values = [rawValue];
      }
    } else if (rawValue.includes(",")) {
      values = rawValue
        .split(",")
        .map(v => v.trim())
        .filter(v => v.length > 0);
    } else {
      values = [rawValue];
    }

    if (Array.isArray(existingValue)) {
      return [...existingValue, ...values];
    }
    return values;
  }

  if (types.includes("integer") && /^-?\d+$/.test(rawValue)) {
    const parsed = Number.parseInt(rawValue, 10);
    if (Number.isSafeInteger(parsed)) {
      return parsed;
    }
  }

  if (types.includes("number")) {
    const parsed = Number(rawValue);
    if (!Number.isNaN(parsed) && Number.isFinite(parsed)) {
      return parsed;
    }
  }

  if (types.includes("boolean")) {
    const normalized = rawValue.trim().toLowerCase();
    if (normalized === "true" || normalized === "1") {
      return true;
    }
    if (normalized === "false" || normalized === "0") {
      return false;
    }
  }

  // Only unescape body-like text fields. Shell argv is already decoded, so
  // applying a second escape pass to arbitrary string fields would corrupt
  // values like Windows paths and regex patterns.
  if (types.includes("string") && shouldUnescapeCliTextArg(key)) {
    return unescapeCliStringArg(rawValue);
  }

  // When schema metadata is unavailable (e.g. empty tools cache), apply
  // conservative numeric coercion fallback for CLI ergonomics.
  if (allowNumericFallback && types.length === 0) {
    const trimmedValue = rawValue.trim();

    if (/^-?\d+$/.test(trimmedValue)) {
      const parsedInt = Number.parseInt(trimmedValue, 10);
      if (!Number.isNaN(parsedInt) && Number.isSafeInteger(parsedInt)) {
        return parsedInt;
      }
    }

    if (/^-?(?:(?:\d+\.\d*|\.\d+)(?:[eE][+-]?\d+)?|\d+[eE][+-]?\d+)$/.test(trimmedValue)) {
      const parsedFloat = Number.parseFloat(trimmedValue);
      if (!Number.isNaN(parsedFloat) && Number.isFinite(parsedFloat)) {
        return parsedFloat;
      }
    }
  }

  return rawValue;
}

// ---------------------------------------------------------------------------
// Tool information / help
// ---------------------------------------------------------------------------

/**
 * Load the cached tool list for a server.
 *
 * @param {string} toolsFile - Path to the JSON tools file
 * @returns {Array<{name: string, description?: string, inputSchema?: {properties?: Record<string, {description?: string, type?: string}>, required?: string[]}}>}
 */
function loadTools(toolsFile) {
  try {
    if (fs.existsSync(toolsFile)) {
      const parsed = JSON.parse(fs.readFileSync(toolsFile, "utf8"));
      return Array.isArray(parsed) ? parsed : [];
    }
  } catch {
    // Fall through to empty array
  }
  return [];
}

/**
 * Ensure safeoutputs CLI always has a non-empty tool schema available.
 *
 * @param {Array<{name: string, description?: string, inputSchema?: {properties?: Record<string, {description?: string, type?: string}>, required?: string[]}}>} tools
 * @param {string} serverName
 * @param {string} toolsFile
 * @returns {Array<{name: string, description?: string, inputSchema?: {properties?: Record<string, {description?: string, type?: string}>, required?: string[]}}>}
 */
function ensureSafeOutputsTools(tools, serverName, toolsFile) {
  if (serverName !== SAFEOUTPUTS_SERVER_NAME || tools.length > 0) {
    return tools;
  }
  const runnerTemp = process.env.RUNNER_TEMP || path.resolve(path.dirname(toolsFile), "../../..");
  const fallbackPath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH || path.join(runnerTemp, "gh-aw", "safeoutputs", "tools.json");
  if (fallbackPath !== toolsFile) {
    const fallbackTools = loadTools(fallbackPath);
    if (fallbackTools.length > 0) {
      global.core?.warning(`[${serverName}] tools cache ${toolsFile} is empty; recovered ${fallbackTools.length} tool(s) from ${fallbackPath}`);
      return fallbackTools;
    }
  }
  throw new Error(`[${serverName}] tool schema is empty (${toolsFile}). ` + "Failing fast to prevent runs where safe-output tools cannot be discovered.");
}

/**
 * @param {string} name
 * @param {string} list
 * @returns {boolean}
 */
function serverInCommaList(name, list) {
  return list
    .split(",")
    .map(item => item.trim())
    .filter(Boolean)
    .includes(name);
}

function isEnclaveBitBudgetExhausted(serverName, message) {
  return serverName === AWF_ENCLAVE_SERVER_NAME && /bit-budget-exhausted/i.test(message);
}

/**
 * Fetch the live tools/list result for a server and persist it over an empty
 * cache. The awf-enclave server is always treated as deferred, and any server in
 * GH_AW_MCP_DEFERRED_SERVERS is also treated as deferred. These servers may
 * register after the wrapper is mounted, so their startup-time cache can
 * legitimately be empty. The refresh retries bounded attempts before falling
 * back to the cached tools. If retries are exhausted, a process-local marker
 * avoids repeating the same bounded retry loop again in this invocation.
 *
 * @param {Array<{name: string, description?: string, inputSchema?: {properties?: Record<string, {description?: string, type?: string}>, required?: string[]}}>} tools
 * @param {string} serverName
 * @param {string} serverUrl
 * @param {string} apiKey
 * @param {string} toolsFile
 * @param {number} [maxAttempts]
 * @param {number} [retryDelayMs]
 * @returns {Promise<Array<{name: string, description?: string, inputSchema?: {properties?: Record<string, {description?: string, type?: string}>, required?: string[]}}>>}
 */
async function refreshDeferredToolsIfNeeded(tools, serverName, serverUrl, apiKey, toolsFile, maxAttempts = DEFERRED_TOOLS_LIST_MAX_ATTEMPTS, retryDelayMs = DEFERRED_TOOLS_LIST_RETRY_DELAY_MS) {
  const isDeferredServer = serverName === AWF_ENCLAVE_SERVER_NAME || serverInCommaList(serverName, process.env[DEFERRED_SERVERS_ENV] || "");
  if (tools.length > 0 || !isDeferredServer) {
    return tools;
  }
  if (deferredToolsRefreshExhaustedServers.has(serverName)) {
    global.core.warning(`[${serverName}] deferred tools/list refresh already exhausted in this process; using cached schema`);
    return tools;
  }
  const core = global.core;
  core.warning(`[${serverName}] cached tool schema is empty for deferred server; refreshing from live gateway`);
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      const sessionId = await mcpInitialize(serverUrl, apiKey, serverName);
      await mcpNotifyInitialized(serverUrl, apiKey, sessionId, serverName);
      /** @type {Record<string, string>} */
      const headers = { Authorization: apiKey };
      if (sessionId) {
        headers["Mcp-Session-Id"] = sessionId;
      }
      const resp = await httpPostJSON(serverUrl, headers, { jsonrpc: "2.0", id: TOOLS_LIST_REQUEST_ID, method: "tools/list" }, DEFAULT_HTTP_TIMEOUT_MS);
      const messages = extractJSONRPCMessages(resp.body);
      const resultMessage = messages.find(isResultMessage);
      const result = resultMessage && typeof resultMessage === "object" && "result" in resultMessage && resultMessage.result && typeof resultMessage.result === "object" ? resultMessage.result : null;
      const refreshed = result && "tools" in result && Array.isArray(result.tools) ? result.tools : [];
      if (refreshed.length > 0) {
        try {
          fs.writeFileSync(toolsFile, JSON.stringify(refreshed, null, 2), { mode: 0o644 });
        } catch (err) {
          core.warning(`[${serverName}] failed to update refreshed tools cache ${toolsFile}: ${getErrorMessage(err)}`);
        }
        deferredToolsRefreshExhaustedServers.delete(serverName);
        core.info(`[${serverName}] refreshed deferred tools cache with ${refreshed.length} tool(s)`);
        return refreshed;
      }
      core.warning(`[${serverName}] live tools/list attempt ${attempt}/${maxAttempts} returned 0 tools for deferred server`);
    } catch (err) {
      core.warning(`[${serverName}] deferred tools/list attempt ${attempt}/${maxAttempts} failed: ${getErrorMessage(err)}`);
    }
    if (attempt < maxAttempts) {
      await new Promise(resolve => setTimeout(resolve, retryDelayMs));
    }
  }
  deferredToolsRefreshExhaustedServers.add(serverName);
  core.warning(`[${serverName}] deferred tools/list refresh exhausted retries; using cached empty schema`);
  return tools;
}

/**
 * Show top-level help: list all available commands for a server.
 *
 * @param {string} serverName - Server name
 * @param {Array<{name: string, description?: string}>} tools - Tool list
 */
function showHelp(serverName, tools) {
  const lines = [`Usage: ${serverName} <command> [--param value ...]`, `Tip: ${serverName} <command> --help`, "", `Commands (${tools.length}):`];
  if (tools.length > 0) {
    const maxCommandLines = Math.max(1, TOP_HELP_MAX_LINES - lines.length);
    lines.push(
      ...formatCompactNameLines(
        tools.map(tool => tool.name),
        maxCommandLines
      )
    );
  } else {
    lines.push("  (tool list unavailable)");
  }
  process.stdout.write(lines.join("\n") + "\n");
}

/**
 * Show help for a specific tool.
 *
 * @param {string} serverName - Server name
 * @param {string} toolName - Tool name
 * @param {Array<{name: string, description?: string, inputSchema?: {properties?: Record<string, {description?: string, type?: string|string[]}>, required?: string[]}}>} tools
 */
function showToolHelp(serverName, toolName, tools) {
  const tool = tools.find(t => t.name === toolName);
  if (!tool) {
    process.stderr.write(`Error: Unknown command '${toolName}'\n`);
    process.stderr.write(`Run '${serverName} --help' to see available commands.\n`);
    process.exitCode = 1;
    return;
  }

  const lines = [
    `Command: ${toolName}`,
    `Description: ${summarizeHelpText(tool.description || "No description", TOOL_DESC_MAX_LEN)}`,
    "Usage:",
    ...renderToolSignature(serverName, tool).split("\n"),
    "Recommended:",
    ...renderToolRecommendedExample(serverName, tool).split("\n"),
    `JSON mode: printf '{"param":"value",...}' | ${serverName} ${toolName} .`,
  ];

  const props = tool.inputSchema?.properties;
  const required = new Set(tool.inputSchema?.required || []);
  if (props && Object.keys(props).length > 0) {
    lines.push("");
    lines.push(`Options (${Object.keys(props).length}):`);
    const optionEntries = Object.entries(props);
    const hasRequiredOptions = required.size > 0;
    const maxOptionLines = Math.max(1, TOOL_HELP_MAX_LINES - lines.length - (hasRequiredOptions ? 1 : 0));
    lines.push(
      ...formatCompactNameLines(
        optionEntries.map(([key]) => `--${key}${required.has(key) ? "*" : ""}`),
        maxOptionLines
      )
    );
    if (hasRequiredOptions) {
      lines.push("Required options are marked with *.");
    }
  }

  process.stdout.write(lines.join("\n") + "\n");
}

/**
 * Determine whether the bridge should show tool help instead of invoking the tool
 * with an empty arguments object.
 *
 * For safeoutputs tools, help is shown only when the tool schema declares at least
 * one required field.  Zero-argument tools (empty properties, no required array) and
 * optional-only tools are valid and should proceed to MCP tools/call.  A missing or
 * malformed cached schema is treated conservatively as no-required so the MCP server
 * remains authoritative; it can return the appropriate protocol error if needed.
 *
 * @param {string} serverName
 * @param {Record<string, unknown>} toolArgs
 * @param {{inputSchema?: {required?: string[]}} | null | undefined} matchedTool - resolved tool definition from the cached tool schema
 * @returns {boolean}
 */
function shouldShowToolHelpForEmptyArgs(serverName, toolArgs, matchedTool) {
  if (serverName !== SAFEOUTPUTS_SERVER_NAME || Object.keys(toolArgs).length !== 0) {
    return false;
  }
  // Show help only when the schema explicitly declares required fields.
  // Zero-argument and optional-only tools have no required array (or an empty one)
  // and must be allowed to call through to MCP so their output item is recorded.
  const required = matchedTool && matchedTool.inputSchema && Array.isArray(matchedTool.inputSchema.required) ? matchedTool.inputSchema.required : [];
  return required.length > 0;
}

/**
 * Render names as comma-separated compact lines and keep all names visible.
 * Width is a soft target; the final line may exceed it to avoid dropping names.
 *
 * @param {string[]} names
 * @param {number} maxLines - Preferred line budget; non-positive/invalid values force one compact line
 * @returns {string[]}
 */
function formatCompactNameLines(names, maxLines) {
  if (!Array.isArray(names) || names.length === 0) {
    // Callers spread the result into help lines, so empty input should contribute no lines.
    return [];
  }
  if (!Number.isFinite(maxLines) || maxLines <= 0) {
    return [`  ${names.join(", ")}`];
  }
  const lines = [];
  let current = "  ";
  for (const name of names) {
    const token = current.trim() ? `, ${name}` : name;
    // A single very long name may still exceed the width target; we keep it intact.
    const shouldStartNewLine = current.length + token.length > COMPACT_NAME_LINE_TARGET_WIDTH;
    if (shouldStartNewLine) {
      lines.push(current);
      current = `  ${name}`;
      continue;
    }
    current += token;
  }
  if (current.trim()) {
    lines.push(current);
  }
  if (lines.length > maxLines) {
    // Keep maxLines - 1 full lines and collapse the remaining names into the final allowed line.
    const compactTail = lines
      .slice(maxLines - 1)
      // Trim per-line indentation before rebuilding a single indented tail line.
      .map(line => line.trim())
      .join(", ");
    return [...lines.slice(0, maxLines - 1), `  ${compactTail}`];
  }
  return lines;
}

// ---------------------------------------------------------------------------
// Response formatting
// ---------------------------------------------------------------------------

/**
 * Extract JSON-RPC messages from a response body that may be:
 * - A JSON object
 * - A JSON string
 * - Server-Sent Events (SSE) payload containing multiple `data:` lines
 *
 * @param {unknown} responseBody
 * @returns {unknown[]}
 */
function extractJSONRPCMessages(responseBody) {
  if (responseBody == null) {
    return [];
  }

  if (Array.isArray(responseBody)) {
    return responseBody;
  }

  if (typeof responseBody === "object") {
    return [responseBody];
  }

  if (typeof responseBody !== "string") {
    return [];
  }

  const trimmed = responseBody.trim();
  if (!trimmed) {
    return [];
  }

  try {
    return [JSON.parse(trimmed)];
  } catch {
    // Fall through to SSE parsing.
  }

  /** @type {unknown[]} */
  const messages = [];
  for (const line of trimmed.split(/\r?\n/)) {
    if (!line.startsWith("data:")) {
      continue;
    }
    const payload = line.slice(5).trim();
    if (!payload || payload === "[DONE]") {
      continue;
    }
    try {
      messages.push(JSON.parse(payload));
    } catch {
      // Ignore non-JSON SSE data lines.
    }
  }

  return messages;
}

/**
 * Render MCP progress notifications to stderr.
 *
 * @param {unknown[]} messages - Parsed JSON-RPC message stream
 */
function renderProgressMessages(messages) {
  for (const message of messages) {
    if (!message || typeof message !== "object" || !("method" in message) || message.method !== "notifications/progress") {
      continue;
    }

    const params = "params" in message && message.params && typeof message.params === "object" ? message.params : null;
    if (!params) {
      continue;
    }

    const progressText = "message" in params && params.message ? String(params.message) : "";
    const progress = "progress" in params && typeof params.progress === "number" ? params.progress : null;
    const total = "total" in params && typeof params.total === "number" ? params.total : null;

    if (progressText) {
      process.stderr.write(progressText + "\n");
      continue;
    }

    if (progress != null && total != null) {
      process.stderr.write(`Progress: ${progress}/${total}\n`);
      continue;
    }

    if (progress != null) {
      process.stderr.write(`Progress: ${progress}\n`);
      continue;
    }

    process.stderr.write(`Progress: ${JSON.stringify(params)}\n`);
  }
}

/**
 * @param {unknown} message
 * @returns {boolean}
 */
function isErrorMessage(message) {
  return !!(message && typeof message === "object" && "error" in message);
}

/**
 * @param {unknown} message
 * @returns {boolean}
 */
function isResultMessage(message) {
  return !!(message && typeof message === "object" && "result" in message);
}

/**
 * Format and display the MCP tool call response.
 *
 * @param {unknown} responseBody - Parsed JSON-RPC response body
 * @param {string} serverName - Server name (for logging)
 * @param {string} [toolName] - Tool name, when known
 * @returns {Promise<void>}
 */
async function formatResponse(responseBody, serverName, toolName = "") {
  const core = global.core;
  const messages = extractJSONRPCMessages(responseBody);
  renderProgressMessages(messages);

  const resp = messages.find(isErrorMessage) || messages.find(isResultMessage) || responseBody;

  // Check for JSON-RPC error
  if (resp && typeof resp === "object" && "error" in resp && resp.error && typeof resp.error === "object") {
    const errRecord = resp.error;
    const message = "message" in errRecord ? String(errRecord.message || "Unknown error") : "Unknown error";
    const code = "code" in errRecord && errRecord.code != null ? String(errRecord.code) : "";
    const isSafeOutputsEmptyArgs = serverName === SAFEOUTPUTS_SERVER_NAME && code === "-32602" && /Empty arguments are not allowed/i.test(message);
    const isEnclaveBitBudget = isEnclaveBitBudgetExhausted(serverName, message);
    const hint =
      isSafeOutputsEmptyArgs && toolName
        ? `Hint: do not retry '${serverName} ${toolName}' with empty arguments. Run '${serverName} ${toolName} --help' to inspect the required options, or call 'noop' with a message if no action is needed.`
        : isEnclaveBitBudget
          ? ENCLAVE_BIT_BUDGET_HINT
          : "";
    const errText = code ? `Error [${code}]: ${message}` : `Error: ${message}`;
    process.stderr.write(errText + "\n");
    auditLog(serverName, { event: "tool_error", error: errText });
    if (hint) process.stderr.write(hint + "\n");
    core.setFailed(`[${serverName}] Tool call error: ${errText}`);
    return;
  }

  // Extract result content
  if (resp && typeof resp === "object" && "result" in resp && resp.result && typeof resp.result === "object") {
    const result = resp.result;
    const isErrorResult = "isError" in result && result.isError === true;
    if ("content" in result && Array.isArray(result.content)) {
      const outputParts = [];
      for (const item of result.content) {
        const entry = /** @type {Record<string, unknown>} */ item;
        if (entry.type === "text") {
          outputParts.push(String(entry.text));
        } else if (entry.type === "image") {
          outputParts.push(`[image data - ${String(entry.mimeType || "unknown")}]`);
        } else {
          outputParts.push(JSON.stringify(entry));
        }
      }
      const output = outputParts.join("\n");
      if (isErrorResult) {
        process.stderr.write(output + "\n");
        if (isEnclaveBitBudgetExhausted(serverName, output)) {
          process.stderr.write(ENCLAVE_BIT_BUDGET_HINT + "\n");
        }
        auditLog(serverName, { event: "tool_error", error: output });
        core.setFailed(`[${serverName}] Tool returned isError=true: ${output.length} chars`);
        return;
      } else {
        await writeStdoutAndFlush(output + "\n");
        core.info(`[${serverName}] Tool output: ${output.length} chars`);
      }
      return;
    }
    // Fallback: print raw result
    const resultStr = typeof result === "string" ? result : JSON.stringify(result);
    if (isErrorResult) {
      process.stderr.write(resultStr + "\n");
      if (isEnclaveBitBudgetExhausted(serverName, resultStr)) {
        process.stderr.write(ENCLAVE_BIT_BUDGET_HINT + "\n");
      }
      auditLog(serverName, { event: "tool_error", error: resultStr });
      core.setFailed(`[${serverName}] Tool returned isError=true`);
      return;
    } else {
      await writeStdoutAndFlush(resultStr + "\n");
    }
    return;
  }

  // Fallback: print raw response
  const rawStr = typeof resp === "string" ? resp : JSON.stringify(resp);
  await writeStdoutAndFlush(rawStr + "\n");
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

async function main() {
  const core = global.core;
  const { serverName, serverUrl, toolsFile, apiKey, userArgs } = parseBridgeArgs(process.argv);

  if (!serverName || !serverUrl) {
    core.setFailed("mcp_cli_bridge: --server-name and --server-url are required");
    return;
  }

  ensureAuditDir();

  core.info(`[${serverName}] Bridge invoked: argumentCount=${Math.max(0, userArgs.length - 1)}`);
  auditLog(serverName, {
    event: "bridge_invoked",
    pid: process.pid,
  });

  // Load cached tools for help display
  let tools = await refreshDeferredToolsIfNeeded(loadTools(toolsFile), serverName, serverUrl, apiKey, toolsFile);
  tools = ensureSafeOutputsTools(tools, serverName, toolsFile);

  // Route: --help or no args → show top-level help
  if (userArgs.length === 0 || userArgs[0] === "--help" || userArgs[0] === "-h") {
    core.info(`[${serverName}] Showing top-level help (${tools.length} tools)`);
    auditLog(serverName, { event: "show_help", toolCount: tools.length });
    showHelp(serverName, tools);
    return;
  }

  const toolName = userArgs[0];
  const toolUserArgs = userArgs.slice(1);
  const shouldReadStdin = hasStdinJsonPayload(toolUserArgs);

  // Route: <command> --help → show command-specific help
  if (toolUserArgs.length > 0 && (toolUserArgs[0] === "--help" || toolUserArgs[0] === "-h")) {
    core.info(`[${serverName}] Showing help for tool '${toolName}'`);
    auditLog(serverName, { event: "show_tool_help", tool: toolName });
    showToolHelp(serverName, toolName, tools);
    return;
  }

  // Route: <command> [--param value ...] → call tool via MCP
  const matchedTool = tools.find(tool => tool && typeof tool === "object" && tool.name === toolName);
  const schemaProperties = matchedTool && matchedTool.inputSchema && matchedTool.inputSchema.properties ? matchedTool.inputSchema.properties : {};

  // Pre-read stdin when JSON payload mode is triggered ('.' sentinel or no args with piped stdin).
  const stdinContent = shouldReadStdin ? readStdinSync() : null;
  if (shouldReadStdin) {
    const stdinBytes = stdinContent !== null ? Buffer.byteLength(stdinContent, "utf8") : 0;
    core.info(`[${serverName}] Stdin captured: ${stdinBytes} bytes`);
  }
  /** @type {{args: Record<string, unknown>, json: boolean}} */
  let parsedArgs;
  try {
    parsedArgs = parseToolArgs(toolUserArgs, schemaProperties, stdinContent);
  } catch (err) {
    const message = getErrorMessage(err);
    auditLog(serverName, { event: "parse_args_error", tool: toolName, error: message });
    process.stderr.write(`Error: ${message}\n`);
    core.setFailed(`[${serverName}] Argument parsing failed: ${message}`);
    return;
  }
  const { args: toolArgs, json: jsonOutput } = parsedArgs;

  if (shouldShowToolHelpForEmptyArgs(serverName, toolArgs, matchedTool)) {
    core.warning(`[${serverName}] No arguments provided for '${toolName}'; showing command help instead of calling the tool`);
    auditLog(serverName, { event: "show_tool_help_empty_args", tool: toolName });
    showToolHelp(serverName, toolName, tools);
    return;
  }

  const argumentBytes = serializedSize(toolArgs);
  core.info(`[${serverName}] Calling tool '${toolName}' (${argumentBytes} argument bytes${jsonOutput ? ", JSON output" : ""})`);
  auditLog(serverName, { event: "call_start", tool: toolName, argumentBytes });

  const callStartMs = Date.now();
  /** @type {(() => void) | null} */
  let stopKeepalive = null;

  try {
    // MCP session protocol: initialize → notifications/initialized → tools/call
    const sessionId = await mcpInitialize(serverUrl, apiKey, serverName);
    await mcpNotifyInitialized(serverUrl, apiKey, sessionId, serverName);
    stopKeepalive = startMcpKeepalivePings(serverUrl, apiKey, sessionId, serverName);
    const resp = await mcpToolsCall(serverUrl, apiKey, sessionId, toolName, toolArgs, serverName);

    // Stop keepalive BEFORE writing any output.  When stdout is a pipe and the
    // response payload exceeds the OS pipe buffer (~64 KiB on Linux),
    // process.stdout.write() buffers the overflow and flushes it later via the
    // event loop.  If the keepalive timer fires in that window its core.info()
    // call writes to stderr; callers that capture both streams (e.g. 2>&1) see
    // the [info] line interleaved inside the JSON, corrupting it.  Stopping the
    // timer here ensures no further log lines reach stderr during the write.
    stopKeepalive?.();
    stopKeepalive = null;

    const totalMs = Date.now() - callStartMs;
    core.info(`[${serverName}] Tool call complete: total=${totalMs}ms`);
    auditLog(serverName, { event: "call_complete", tool: toolName, totalElapsedMs: totalMs });

    if (jsonOutput) {
      // --json: print the raw JSON-RPC response body
      await writeStdoutAndFlush(JSON.stringify(resp.body, null, 2) + "\n");
    } else {
      await formatResponse(resp.body, serverName, toolName);
    }
  } catch (err) {
    const totalMs = Date.now() - callStartMs;
    const message = getErrorMessage(err);
    auditLog(serverName, {
      event: "call_error",
      tool: toolName,
      error: message,
      totalElapsedMs: totalMs,
    });
    process.stderr.write(`Error: ${message}\n`);
    core.setFailed(`[${serverName}] Tool call failed (${totalMs}ms): ${message}`);
  } finally {
    stopKeepalive?.();
  }
}

if (require.main === module) {
  main().catch(err => {
    const core = global.core;
    const message = err instanceof Error ? err.stack || err.message : String(err);
    if (core && typeof core.setFailed === "function") {
      core.setFailed(`mcp_cli_bridge fatal: ${message}`);
      return;
    }
    process.exitCode = 1;
    process.stderr.write(`mcp_cli_bridge fatal: ${message}\n`);
  });
}

module.exports = {
  parseToolArgs,
  coerceToolArgValue,
  unescapeCliStringArg,
  tryExtractJsonFieldFromStdin,
  extractJSONRPCMessages,
  renderProgressMessages,
  formatResponse,
  writeStdoutAndFlush,
  showHelp,
  showToolHelp,
  shouldShowToolHelpForEmptyArgs,
  hasStdinJsonPayload,
  readStdinSync,
  ensureSafeOutputsTools,
  refreshDeferredToolsIfNeeded,
  serverInCommaList,
  getToolCallTimeoutMs,
  auditLog,
  ensureAuditDir,
  sanitizeAuditEntry,
  serializedSize,
  main,
};
