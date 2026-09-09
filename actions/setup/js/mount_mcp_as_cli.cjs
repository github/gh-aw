// @ts-check
/// <reference types="@actions/github-script" />

/**
 * mount_mcp_as_cli.cjs
 *
 * @safe-outputs-exempt SEC-004: "body" references are JSON-RPC transport payloads, not user-authored comment bodies
 *
 * Mounts MCP servers as local CLI tools by reading the manifest written by
 * start_mcp_gateway.cjs, querying each server for its tool list, and generating
 * a standalone bash wrapper script per server in ${RUNNER_TEMP}/gh-aw/mcp-cli/bin/.
 *
 * The bin directory is locked (chmod 555) so the agent cannot modify or inject
 * scripts. The directory is added to PATH via core.addPath().
 *
 * Scripts are placed under ${RUNNER_TEMP}/gh-aw/ (not /tmp/gh-aw/) so they are
 * accessible inside the AWF sandbox, which mounts ${RUNNER_TEMP}/gh-aw read-only.
 *
 * Generated CLI wrapper usage:
 *   <server> --help                         Show all available commands
 *   <server> <command> --help               Show help for a specific command
 *   <server> <command> [--param value ...]  Execute a command
 */

const fs = require("fs");
const http = require("http");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { renderSafeOutputsPromptDocs } = require("./mcp_cli_schema_docs.cjs");

const MANIFEST_FILE = path.join(process.env.RUNNER_TEMP || "/home/runner/work/_temp", "gh-aw/mcp-cli/manifest.json");
// Use RUNNER_TEMP so the bin and tools directories are inside the AWF sandbox mount
// (AWF mounts ${RUNNER_TEMP}/gh-aw read-only; /tmp/gh-aw is not accessible inside AWF)
const RUNNER_TEMP = process.env.RUNNER_TEMP || "/home/runner/work/_temp";
const CLI_BIN_DIR = `${RUNNER_TEMP}/gh-aw/mcp-cli/bin`;
const TOOLS_DIR = `${RUNNER_TEMP}/gh-aw/mcp-cli/tools`;
const AWF_GATEWAY_IP = "172.30.0.1";
const SAFEOUTPUTS_SERVER_NAME = "safeoutputs";

/** Default timeout (ms) for HTTP calls to the local MCP gateway */
const DEFAULT_HTTP_TIMEOUT_MS = 15000;

/**
 * Maximum number of times to retry tools/list when a server returns 0 tools.
 * The gateway may report a backend as "running" before the backend has finished
 * building its tool schema (a race condition more likely with large configs).
 */
const TOOLS_EMPTY_MAX_RETRIES = 5;

/**
 * Milliseconds to wait between tools/list retry attempts when the result is empty.
 */
const TOOLS_EMPTY_RETRY_DELAY_MS = 1000;

/**
 * Parse a tools JSON file and return a validated tools array.
 *
 * @param {string} toolsPath
 * @param {typeof import("@actions/core")} core
 * @returns {Array<{name: string, description?: string, inputSchema?: unknown}>}
 */
function loadToolsFromJSONFile(toolsPath, core) {
  try {
    if (!fs.existsSync(toolsPath)) {
      return [];
    }
    const parsed = JSON.parse(fs.readFileSync(toolsPath, "utf8"));
    return Array.isArray(parsed) ? parsed : [];
  } catch (err) {
    core.warning(`  Failed to read tools file ${toolsPath}: ${getErrorMessage(err)}`);
    return [];
  }
}

/**
 * Return the path where the safeoutputs gateway-empty flag file is written.
 * The path is computed at call time (not module load time) so that tests can
 * control the location by setting process.env.RUNNER_TEMP.
 *
 * @returns {string}
 */
function getSafeOutputsGatewayEmptyFlagPath() {
  const runnerTemp = process.env.RUNNER_TEMP || "/home/runner/work/_temp";
  return path.join(runnerTemp, "gh-aw", "safeoutputs", "gateway_empty.flag");
}

/**
 * Write a flag file that signals the safeoutputs MCP gateway registered 0 tools.
 * collect_ndjson_output.cjs reads this flag and fails the conclusion job with a
 * clear infra error instead of silently treating the missing outputs.jsonl as a
 * graceful no-op.
 *
 * Failures are non-fatal — the flag is best-effort. A warning is emitted if the
 * write fails so that the issue is still surfaced in the step log.
 *
 * @param {typeof import("@actions/core")} core
 */
function writeSafeOutputsGatewayEmptyFlag(core) {
  const flagPath = getSafeOutputsGatewayEmptyFlagPath();
  try {
    fs.mkdirSync(path.dirname(flagPath), { recursive: true });
    fs.writeFileSync(flagPath, "", { flag: "w" });
  } catch (err) {
    core.warning(`Failed to write safeoutputs gateway-empty flag at ${flagPath}: ${getErrorMessage(err)}`);
  }
}

/**
 * Validate the safeoutputs tool list and fail fast when the live gateway is empty.
 *
 * When the live gateway returns 0 tools, a flag file is written so that the
 * conclusion job (collect_ndjson_output.cjs) can detect the outage and fail with
 * a clear infra error instead of treating the missing outputs.jsonl as a graceful
 * no-op.
 *
 * @param {Array<{name: string, description?: string, inputSchema?: unknown}>} tools
 * @param {typeof import("@actions/core")} core
 * @returns {Array<{name: string, description?: string, inputSchema?: unknown}>}
 */
function recoverSafeOutputsToolsIfNeeded(tools, core) {
  if (tools.length > 0) {
    return tools;
  }

  // The live MCP gateway returned 0 tools for safeoutputs.  Write a flag file so
  // that collect_ndjson_output.cjs can surface this as a hard failure instead of
  // silently concluding "graceful no-op" when outputs.jsonl is never written.
  writeSafeOutputsGatewayEmptyFlag(core);

  throw new Error(`safeoutputs tools/list returned 0 tools. ` + `Failing fast — the live MCP gateway has no tools registered. ` + `Check the MCP gateway startup logs for ECONNRESET errors or delayed backend registration.`);
}

/**
 * Per-server post-fetch validator registry.
 * Each entry receives the fetched tool list and the @actions/core instance, and returns
 * a (possibly replaced) tool list. Throwing here aborts the mount step for that server.
 * Add an entry here when a server needs special validation after tools/list.
 *
 * @type {Record<string, (tools: Array<{name: string, description?: string, inputSchema?: unknown}>, core: typeof import("@actions/core")) => Array<{name: string, description?: string, inputSchema?: unknown}>>}
 */
const SERVER_VALIDATORS = {
  [SAFEOUTPUTS_SERVER_NAME]: (tools, core) => recoverSafeOutputsToolsIfNeeded(tools, core),
};

/**
 * Build prompt documentation for mounted MCP CLI servers.
 *
 * @param {Array<{name: string, tools: Array<{name: string, description?: string, inputSchema?: unknown}>}>} mountedServerTools
 * @returns {string}
 */
function buildMCPCLIServersPromptList(mountedServerTools) {
  return mountedServerTools
    .map(server => {
      if (server.name !== SAFEOUTPUTS_SERVER_NAME) {
        return `- \`${server.name}\` — run \`${server.name} --help\` to see available tools`;
      }
      return renderSafeOutputsPromptDocs(server.name, server.tools);
    })
    .join("\n");
}

/**
 * Validate that a server name is safe to use as a filename and in shell scripts.
 * Prevents path traversal, shell metacharacter injection, and other abuse.
 *
 * @param {string} name - Server name from the manifest
 * @returns {boolean} true if the name is safe
 */
function isValidServerName(name) {
  // Only allow alphanumeric, hyphen, underscore — no dots, slashes, spaces, etc.
  return /^[a-zA-Z0-9_-]+$/.test(name) && name.length > 0 && name.length <= 64;
}

/**
 * Escape a string for safe embedding inside double-quoted shell strings.
 * Handles the characters that are special inside double quotes: $ ` \ " !
 * Also strips newlines and carriage returns to prevent line injection.
 *
 * @param {string} str - Raw string
 * @returns {string} Escaped string safe for use inside double-quoted shell strings
 */
function shellEscapeDoubleQuoted(str) {
  return str.replace(/[\r\n]/g, "").replace(/[\\"$`!]/g, "\\$&");
}

/**
 * Rewrite a raw gateway manifest URL to use the container-accessible domain.
 *
 * The manifest stores raw gateway-output URLs (e.g., http://0.0.0.0:8080/mcp/server)
 * that work from the host. Inside the AWF sandbox the gateway is reachable via
 * MCP_GATEWAY_DOMAIN:MCP_GATEWAY_PORT (typically host.docker.internal:8080).
 *
 * @param {string} rawUrl - URL from the manifest (host-accessible)
 * @returns {string} URL suitable for use inside AWF containers
 */
function toContainerUrl(rawUrl) {
  let domain = process.env.MCP_GATEWAY_DOMAIN;
  const port = process.env.MCP_GATEWAY_PORT;
  if (domain === "host.docker.internal") {
    // The CLI wrappers may run inside a chrooted host environment where
    // host.docker.internal is not resolvable. Use the AWF gateway IP instead.
    domain = AWF_GATEWAY_IP;
  }
  if (domain && port) {
    return rawUrl.replace(/^https?:\/\/[^/]+\/mcp\//, `http://${domain}:${port}/mcp/`);
  }
  return rawUrl;
}

/**
 * Make an HTTP POST request with a JSON body and return the parsed response.
 *
 * @param {string} urlStr - Full URL to POST to
 * @param {Record<string, string>} headers - Request headers
 * @param {unknown} body - Request body (will be JSON-serialized)
 * @param {number} [timeoutMs=DEFAULT_HTTP_TIMEOUT_MS] - Request timeout in milliseconds.
 *   `initialize` and `tools/list` calls are expected to be fast (local gateway),
 *   but tool invocations may take longer; callers can override as needed.
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

    req.on("error", err => {
      reject(err);
    });

    req.setTimeout(timeoutMs, () => {
      // req.destroy() is the correct modern API; req.abort() is deprecated since Node.js v14
      req.destroy();
      reject(new Error(`HTTP request timed out after ${timeoutMs}ms`));
    });

    req.write(bodyStr);
    req.end();
  });
}

/**
 * Parse an MCP response body that may be JSON or Server-Sent Events (SSE).
 *
 * Some MCP gateway responses are streamed as SSE and contain lines like:
 *   data: {"jsonrpc":"2.0","id":3,"result":{...}}
 *
 * @param {unknown} body - Parsed response body from httpPostJSON
 * @returns {unknown}
 */
function parseMCPResponseBody(body) {
  if (body && typeof body === "object" && !Array.isArray(body)) {
    return body;
  }
  if (typeof body !== "string") {
    return null;
  }

  const trimmed = body.trim();
  if (!trimmed) {
    return null;
  }

  try {
    return JSON.parse(trimmed);
  } catch {
    // Fall through to SSE parsing.
  }

  /** @type {unknown} */
  let lastDataMessage = null;
  for (const line of trimmed.split(/\r?\n/)) {
    if (!line.startsWith("data:")) {
      continue;
    }
    const payload = line.slice(5).trim();
    if (!payload || payload === "[DONE]") {
      continue;
    }
    try {
      lastDataMessage = JSON.parse(payload);
    } catch {
      // Ignore non-JSON SSE data lines.
    }
  }
  return lastDataMessage;
}

/**
 * Query the tools list from an MCP server via JSON-RPC.
 * Follows the standard MCP handshake: initialize → notifications/initialized → tools/list.
 *
 * @param {string} serverUrl - HTTP URL of the MCP server endpoint
 * @param {string} agentId - Bearer token for gateway authentication
 * @param {typeof import("@actions/core")} core - GitHub Actions core
 * @returns {Promise<{tools: Array<{name: string, description?: string, inputSchema?: unknown}>, emptyWasSuccessful: boolean}>}
 */
async function fetchMCPToolsResult(serverUrl, agentId, core) {
  const authHeaders = { Authorization: agentId };

  // Step 1: initialize – establish the session and capture Mcp-Session-Id if present
  /** @type {any} */
  let sessionHeader = {};
  try {
    const initResp = await httpPostJSON(
      serverUrl,
      authHeaders,
      {
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: {
          capabilities: {},
          clientInfo: { name: "mcp-cli-mount", version: "1.0.0" },
          protocolVersion: "2024-11-05",
        },
      },
      DEFAULT_HTTP_TIMEOUT_MS
    );
    const sessionId = initResp.headers["mcp-session-id"];
    if (sessionId && typeof sessionId === "string") {
      sessionHeader = { "Mcp-Session-Id": sessionId };
    }
  } catch (err) {
    core.warning(`  initialize failed for ${serverUrl}: ${getErrorMessage(err)}`);
    return { tools: [], emptyWasSuccessful: false };
  }

  // Step 2: notifications/initialized – required by MCP spec to complete the handshake.
  // The server responds with 204 No Content; errors here are non-fatal.
  try {
    await httpPostJSON(serverUrl, { ...authHeaders, ...sessionHeader }, { jsonrpc: "2.0", method: "notifications/initialized", params: {} }, 10000);
  } catch (err) {
    core.warning(`  notifications/initialized failed for ${serverUrl}: ${getErrorMessage(err)}`);
  }

  // Step 3: tools/list – get the available tool definitions
  try {
    const listResp = await httpPostJSON(serverUrl, { ...authHeaders, ...sessionHeader }, { jsonrpc: "2.0", id: 2, method: "tools/list" }, DEFAULT_HTTP_TIMEOUT_MS);
    const respBody = parseMCPResponseBody(listResp.body);
    if (respBody && typeof respBody === "object" && "result" in respBody && respBody.result && typeof respBody.result === "object") {
      const result = respBody.result;
      if ("tools" in result && Array.isArray(result.tools)) {
        return {
          tools: /** @type {Array<{name: string, description?: string, inputSchema?: unknown}>} */ result.tools,
          emptyWasSuccessful: true,
        };
      }
    }
    return { tools: [], emptyWasSuccessful: false };
  } catch (err) {
    core.warning(`  tools/list failed for ${serverUrl}: ${getErrorMessage(err)}`);
    return { tools: [], emptyWasSuccessful: false };
  }
}

/**
 * Query the tools list from an MCP server via JSON-RPC.
 *
 * @param {string} serverUrl - HTTP URL of the MCP server endpoint
 * @param {string} agentId - ****** for gateway authentication
 * @param {typeof import("@actions/core")} core - GitHub Actions core
 * @returns {Promise<Array<{name: string, description?: string, inputSchema?: unknown}>>}
 */
async function fetchMCPTools(serverUrl, agentId, core) {
  const result = await fetchMCPToolsResult(serverUrl, agentId, core);
  return result.tools;
}

/**
 * Fetch MCP tools with retry on empty result.
 *
 * The MCP gateway may report a backend as "running" before that backend has
 * finished building its internal tool schema (a race between process-level
 * readiness and schema construction). This is more likely with large
 * dispatch-workflow configs where building tool definitions takes long enough
 * that tools/list can still return 0 tools immediately after the health check
 * passes. Retrying a handful of times with a short delay bridges that gap, but
 * only for successful empty tools/list responses; transport/protocol failures
 * stop immediately so unavailable backends still fail fast.
 *
 * @param {string} serverUrl
 * @param {string} agentId
 * @param {string} serverName - Server name, used only for log messages
 * @param {typeof import("@actions/core")} core
 * @param {object} [options]
 * @param {(ms: number) => Promise<void>} [options.sleep] - Delay function (injectable for tests)
 * @param {(url: string, key: string, c: typeof import("@actions/core")) => Promise<Array<{name: string, description?: string, inputSchema?: unknown}> | {tools: Array<{name: string, description?: string, inputSchema?: unknown}>, emptyWasSuccessful: boolean}>} [options.fetchFn] - Fetch function (injectable for tests)
 * @returns {Promise<Array<{name: string, description?: string, inputSchema?: unknown}>>}
 */
async function fetchMCPToolsWithRetry(serverUrl, agentId, serverName, core, { sleep = undefined, fetchFn = undefined } = {}) {
  const doSleep = sleep ?? (ms => new Promise(resolve => setTimeout(resolve, ms)));
  const doFetchResult = async (url, key, c) => {
    if (!fetchFn) {
      return fetchMCPToolsResult(url, key, c);
    }
    const result = await fetchFn(url, key, c);
    if (Array.isArray(result)) {
      return { tools: result, emptyWasSuccessful: true };
    }
    return result;
  };
  let result = await doFetchResult(serverUrl, agentId, core);
  for (let attempt = 1; attempt <= TOOLS_EMPTY_MAX_RETRIES && result.emptyWasSuccessful && result.tools.length === 0; attempt++) {
    core.warning(`  tools/list returned 0 tools for '${serverName}', retrying in ${TOOLS_EMPTY_RETRY_DELAY_MS}ms (attempt ${attempt}/${TOOLS_EMPTY_MAX_RETRIES})...`);
    await doSleep(TOOLS_EMPTY_RETRY_DELAY_MS);
    result = await doFetchResult(serverUrl, agentId, core);
    if (!result.emptyWasSuccessful) {
      core.warning(`  stopping empty tools/list retries for '${serverName}' because tools/list did not complete successfully`);
      break;
    }
  }
  if (result.emptyWasSuccessful && result.tools.length === 0) {
    core.warning(`  tools/list still returned 0 tools for '${serverName}' after ${TOOLS_EMPTY_MAX_RETRIES} retries; continuing with empty tool list`);
  }
  return result.tools;
}

/**
 * Generate the bash wrapper script content for a given MCP server.
 * The generated script is a thin wrapper that delegates all work to the
 * mcp_cli_bridge.cjs Node.js script, which handles the full MCP session
 * protocol (initialize → notifications/initialized → tools/call), help
 * display, argument parsing, console logging, and JSONL audit logging.
 *
 * The gateway agent ID is baked directly into the generated script at
 * generation time because MCP_GATEWAY_AGENT_ID is excluded from the AWF
 * sandbox environment (--exclude-env MCP_GATEWAY_AGENT_ID) and would not
 * be accessible to the agent at runtime.
 *
 * @param {string} serverName - Name of the MCP server
 * @param {string} serverUrl - HTTP URL of the MCP server endpoint
 * @param {string} toolsFile - Path to the cached tools JSON file
 * @param {string} agentId - Gateway agent ID, baked into the script at generation time
 * @param {string} bridgeScript - Absolute path to mcp_cli_bridge.cjs
 * @returns {string} Content of the bash wrapper script
 */
function generateCLIWrapperScript(serverName, serverUrl, toolsFile, agentId, bridgeScript) {
  // Sanitize all values that are embedded in the shell script to prevent injection.
  // Server names are pre-validated by isValidServerName(), but we still escape all
  // values for defense-in-depth.
  const safeName = shellEscapeDoubleQuoted(serverName);
  const safeUrl = shellEscapeDoubleQuoted(serverUrl);
  const safeToolsFile = shellEscapeDoubleQuoted(toolsFile);
  const safeAgentId = shellEscapeDoubleQuoted(agentId);
  const safeBridge = shellEscapeDoubleQuoted(bridgeScript);

  return `#!/usr/bin/env bash
set +o histexpand

# MCP CLI wrapper for: ${safeName}
# Auto-generated by gh-aw. Do not modify.
#
# Usage:
#   ${safeName} --help                        Show all available commands
#   ${safeName} <command> --help              Show help for a specific command
#   ${safeName} <command> [--param value...]  Execute a command
#
# All calls are delegated to the mcp_cli_bridge.cjs Node.js bridge which
# handles the MCP session protocol, logging, and JSONL audit trail.

exec node "${safeBridge}" \\
  --server-name "${safeName}" \\
  --server-url "${safeUrl}" \\
  --tools-file "${safeToolsFile}" \\
  --api-key "${safeAgentId}" \\
  "\$@"
`;
}

/**
 * Mount MCP servers as CLI tools by reading the manifest and generating wrapper scripts.
 *
 * @returns {Promise<void>}
 */
async function main() {
  const core = global.core;

  core.info("Mounting MCP servers as CLI tools...");

  if (!fs.existsSync(MANIFEST_FILE)) {
    core.info("No MCP CLI manifest found, skipping CLI mounting");
    return;
  }

  /** @type {{servers: Array<{name: string, url: string}>}} */
  let manifest;
  try {
    manifest = JSON.parse(fs.readFileSync(MANIFEST_FILE, "utf8"));
  } catch (err) {
    core.warning(`Failed to read MCP CLI manifest: ${getErrorMessage(err)}`);
    return;
  }

  const servers = manifest.servers || [];

  if (servers.length === 0) {
    core.info("No MCP servers in manifest, skipping CLI mounting");
    return;
  }

  core.info(`Found ${servers.length} server(s) in manifest to mount as CLI tools`);

  try {
    fs.mkdirSync(CLI_BIN_DIR, { recursive: true });
    fs.mkdirSync(TOOLS_DIR, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create MCP CLI directories: ${getErrorMessage(err)}`, { cause: err });
  }

  // The bridge script lives alongside mount_mcp_as_cli.cjs in the setup actions directory.
  // It is accessible inside the AWF sandbox because ${RUNNER_TEMP}/gh-aw is mounted read-only.
  const bridgeScript = path.join(path.dirname(__filename), "mcp_cli_bridge.cjs");
  if (!fs.existsSync(bridgeScript)) {
    core.warning(`mcp_cli_bridge.cjs not found at ${bridgeScript}; CLI wrappers will not work`);
  } else {
    core.info(`Bridge script: ${bridgeScript}`);
  }

  const agentId = process.env.MCP_GATEWAY_AGENT_ID || "";
  if (!agentId) {
    core.warning("MCP_GATEWAY_AGENT_ID is not set; generated CLI wrappers will not be able to authenticate with the gateway");
  }

  const gatewayDomain = process.env.MCP_GATEWAY_DOMAIN || "";
  const gatewayPort = process.env.MCP_GATEWAY_PORT || "";
  if (!gatewayDomain || !gatewayPort) {
    core.warning("MCP_GATEWAY_DOMAIN or MCP_GATEWAY_PORT is not set; CLI wrappers will use raw manifest URLs which may not be reachable inside the AWF sandbox");
  }

  const mountedServers = [];
  /** @type {Array<{name: string, tools: Array<{name: string, description?: string, inputSchema?: unknown}>}>} */
  const mountedServerTools = [];
  const skippedServers = [];

  for (const server of servers) {
    const { name, url } = server;

    // Validate server name to prevent path traversal and shell injection.
    // Server names become filenames in CLI_BIN_DIR and are embedded in shell scripts.
    if (!isValidServerName(name)) {
      core.warning(`Skipping server '${name}': name contains invalid characters (only alphanumeric, hyphen, underscore allowed)`);
      skippedServers.push(name);
      continue;
    }

    // Validate URL format
    try {
      new URL(url);
    } catch {
      core.warning(`Skipping server '${name}': invalid URL '${url}'`);
      skippedServers.push(name);
      continue;
    }
    // The manifest URL is the host-accessible raw gateway address (e.g., http://0.0.0.0:8080/mcp/server).
    // Rewrite it to the container-accessible URL for the generated CLI wrapper scripts,
    // which run inside the AWF sandbox where the gateway is reached via MCP_GATEWAY_DOMAIN.
    const containerUrl = toContainerUrl(url);
    core.info(`Mounting MCP server '${name}' (host url: ${url}, container url: ${containerUrl})...`);

    const toolsFile = path.join(TOOLS_DIR, `${name}.json`);

    // Query tools from the server using the host-accessible URL (mount step runs on host).
    // Retries on empty to handle the race between gateway health-reporting and
    // the backend finishing internal tool-schema construction (common with large configs).
    let tools = await fetchMCPToolsWithRetry(url, agentId, name, core);
    const validate = SERVER_VALIDATORS[name];
    if (validate) {
      tools = validate(tools, core);
    }
    core.info(`  Found ${tools.length} tool(s)`);

    // Cache the tool list. This file only contains tool name/description/schema
    // metadata returned by the gateway; it does not contain the agent ID or any
    // other secret, so world-readable permissions (0o644) are acceptable here.
    try {
      fs.writeFileSync(toolsFile, JSON.stringify(tools, null, 2), { mode: 0o644 });
    } catch (err) {
      core.warning(`  Failed to write tools cache for ${name}: ${getErrorMessage(err)}`);
    }

    // Write the CLI wrapper script using the container-accessible URL
    const scriptPath = path.join(CLI_BIN_DIR, name);
    let scriptFd;
    try {
      // Owner-only permissions: the wrapper script embeds the plaintext gateway agent ID,
      // so it must not be world- or group-readable (matches chmod 600 used elsewhere for
      // this same credential, e.g. convert_gateway_config_copilot.sh).
      // Note: writeFileSync(mode) only applies when creating a new file; for existing files,
      // force mode with fchmodSync so prior permissive modes (e.g., 0o755) are corrected.
      scriptFd = fs.openSync(scriptPath, "w", 0o700);
      fs.fchmodSync(scriptFd, 0o700);
      fs.writeFileSync(scriptFd, generateCLIWrapperScript(name, containerUrl, toolsFile, agentId, bridgeScript), "utf8");
      fs.closeSync(scriptFd);
      scriptFd = undefined;
      mountedServers.push(name);
      mountedServerTools.push({ name, tools });
      core.info(`  ✓ Mounted as: ${scriptPath}`);
    } catch (err) {
      core.warning(`  Failed to write CLI wrapper for ${name}: ${getErrorMessage(err)}`);
    } finally {
      if (scriptFd !== undefined) {
        try {
          fs.closeSync(scriptFd);
        } catch {
          // Ignore close errors in cleanup path; main error already reported above.
        }
      }
    }
  }

  if (mountedServers.length === 0) {
    core.info("No MCP servers were successfully mounted as CLI tools");
    return;
  }

  // Lock the bin directory so the agent cannot modify or inject scripts
  try {
    fs.chmodSync(CLI_BIN_DIR, 0o555);
    core.info(`CLI bin directory locked (read-only): ${CLI_BIN_DIR}`);
  } catch (err) {
    core.warning(`Failed to lock CLI bin directory: ${getErrorMessage(err)}`);
  }

  // Add the bin directory to PATH for subsequent steps
  core.addPath(CLI_BIN_DIR);

  core.info("");
  core.info(`Successfully mounted ${mountedServers.length} MCP server(s) as CLI tools:`);
  for (const name of mountedServers) {
    core.info(`  - ${name}`);
  }
  if (skippedServers.length > 0) {
    core.warning(`Skipped ${skippedServers.length} server(s) due to validation errors: ${skippedServers.join(", ")}`);
  }
  core.info(`CLI bin directory added to PATH: ${CLI_BIN_DIR}`);
  core.setOutput("mounted-servers", mountedServers.join(","));
  const docs = buildMCPCLIServersPromptList(mountedServerTools);
  core.setOutput("mcp-cli-servers-list", docs);
}

module.exports = {
  AWF_GATEWAY_IP,
  main,
  fetchMCPTools,
  fetchMCPToolsWithRetry,
  generateCLIWrapperScript,
  isValidServerName,
  shellEscapeDoubleQuoted,
  parseMCPResponseBody,
  toContainerUrl,
  loadToolsFromJSONFile,
  recoverSafeOutputsToolsIfNeeded,
  getSafeOutputsGatewayEmptyFlagPath,
  writeSafeOutputsGatewayEmptyFlag,
  SERVER_VALIDATORS,
  buildMCPCLIServersPromptList,
  TOOLS_EMPTY_MAX_RETRIES,
  TOOLS_EMPTY_RETRY_DELAY_MS,
};
