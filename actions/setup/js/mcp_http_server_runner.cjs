// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Shared HTTP MCP Server Runner
 *
 * This module provides the common HTTP server lifecycle implementation shared
 * between mcp_scripts_mcp_server_http.cjs and safe_outputs_mcp_server_http.cjs.
 *
 * It handles:
 * - CORS headers and OPTIONS preflight
 * - GET /health endpoint
 * - API key authentication and bounded request body reads
 * - POST body parsing and JSON-RPC request routing
 * - Per-request error handling
 * - Bind error handling (EADDRINUSE, EACCES)
 * - Graceful shutdown on SIGINT / SIGTERM
 *
 * @module mcp_http_server_runner
 */

const http = require("http");
const { timingSafeEqual } = require("crypto");
const { createLogger } = require("./mcp_logger.cjs");

const MAX_REQUEST_BODY_BYTES = 1024 * 1024;
const REQUEST_BODY_TIMEOUT_MS = 30_000;

/**
 * @param {unknown} apiKey
 * @param {string} credentialName
 * @returns {string}
 */
function requireHttpApiKey(apiKey, credentialName) {
  if (typeof apiKey !== "string" || !apiKey.trim()) {
    throw new Error(`${credentialName} must be set to a non-empty API key for HTTP MCP requests`);
  }
  return apiKey;
}

class RequestBodyError extends Error {
  /** @param {number} statusCode @param {string} message */
  constructor(statusCode, message) {
    super(message);
    this.statusCode = statusCode;
  }
}

/**
 * Bound the complete upload, not tool execution or idle gateway connections.
 * @param {http.IncomingMessage} req
 * @returns {Promise<Buffer>}
 */
function readRequestBody(req) {
  return new Promise((resolve, reject) => {
    /** @type {Buffer[]} */
    const chunks = [];
    let bytes = 0;
    const timer = setTimeout(() => fail(new RequestBodyError(408, "Request body timeout")), REQUEST_BODY_TIMEOUT_MS);
    function cleanup() {
      clearTimeout(timer);
      req.removeListener("data", onData);
      req.removeListener("end", onEnd);
      req.removeListener("error", onError);
      req.removeListener("aborted", onAborted);
    }
    /** @param {RequestBodyError} error */
    function fail(error) {
      cleanup();
      chunks.length = 0;
      req.pause();
      reject(error);
    }
    /** @param {Buffer} chunk */
    function onData(chunk) {
      bytes += chunk.length;
      if (bytes > MAX_REQUEST_BODY_BYTES) {
        fail(new RequestBodyError(413, "Request body too large"));
        return;
      }
      chunks.push(chunk);
    }
    function onEnd() {
      cleanup();
      resolve(Buffer.concat(chunks, bytes));
    }
    function onError() {
      fail(new RequestBodyError(400, "Failed to read request body"));
    }
    function onAborted() {
      fail(new RequestBodyError(400, "Request body aborted"));
    }
    req.on("data", onData);
    req.once("end", onEnd);
    req.once("error", onError);
    req.once("aborted", onAborted);
  });
}

/**
 * Start an HTTP server that routes MCP protocol requests to the given transport.
 *
 * @param {Object} options - Runner options
 * @param {any} options.transport - Connected MCPHTTPTransport instance
 * @param {number} options.port - Port to listen on
 * @param {Function} options.getHealthPayload - Zero-argument function that returns the JSON object for GET /health responses
 * @param {Object} options.logger - Logger instance (from mcp_logger.cjs createLogger)
 * @param {string} options.serverLabel - Human-readable label used in log messages (e.g. "MCP Scripts")
 * @param {string} options.apiKey - Required API key, sent verbatim in the Authorization header by the gateway
 * @param {Function} [options.configureServer] - Optional callback called with the http.Server instance before listen, useful for setting timeouts
 * @returns {Promise<http.Server>} The started HTTP server
 */
async function runHttpServer(options) {
  const { transport, port, getHealthPayload, logger, serverLabel, configureServer } = options;
  const expectedKey = Buffer.from(requireHttpApiKey(options.apiKey, `${serverLabel} API key`));

  const httpServer = http.createServer(async (req, res) => {
    req.on("error", error => logger.debugError("HTTP request stream error: ", error));
    // Set CORS headers for development
    res.setHeader("Access-Control-Allow-Origin", "*");
    res.setHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
    res.setHeader("Access-Control-Allow-Headers", "Content-Type, Accept, Authorization");

    /** @param {number} statusCode @param {string} message */
    function rejectRequest(statusCode, message) {
      // Flush the error response, then close rather than draining an unbounded upload.
      req.pause();
      res.once("finish", () => req.destroy());
      res.writeHead(statusCode, { "Content-Type": "application/json", Connection: "close" });
      res.end(JSON.stringify({ error: message }));
    }

    // Handle OPTIONS preflight
    if (req.method === "OPTIONS") {
      res.writeHead(200);
      res.end();
      return;
    }

    // Handle GET /health endpoint for health checks
    if (req.method === "GET" && req.url === "/health") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(getHealthPayload()));
      return;
    }

    // Only handle POST requests for MCP protocol
    if (req.method !== "POST") {
      res.writeHead(405, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "Method not allowed" }));
      return;
    }

    const suppliedKey = Buffer.from(req.headers.authorization || "");
    if (suppliedKey.length !== expectedKey.length || !timingSafeEqual(suppliedKey, expectedKey)) {
      rejectRequest(401, "Unauthorized");
      return;
    }

    const contentLength = req.headers["content-length"];
    if (contentLength !== undefined) {
      if (!/^\d+$/.test(contentLength) || !Number.isSafeInteger(Number(contentLength))) {
        rejectRequest(400, "Invalid Content-Length");
        return;
      }
      if (Number(contentLength) > MAX_REQUEST_BODY_BYTES) {
        rejectRequest(413, "Request body too large");
        return;
      }
    }

    try {
      // Parse request body for POST requests
      /** @type {any} */
      let body = null;
      const bodyStr = (await readRequestBody(req)).toString();
      try {
        body = bodyStr ? JSON.parse(bodyStr) : null;
      } catch (parseError) {
        res.writeHead(400, { "Content-Type": "application/json" });
        res.end(
          JSON.stringify({
            jsonrpc: "2.0",
            error: {
              code: -32700,
              message: "Parse error: Invalid JSON in request body",
            },
            id: null,
          })
        );
        return;
      }

      // Let the transport handle the request
      await transport.handleRequest(req, res, body);
    } catch (error) {
      // Log the full error with stack trace on the server for debugging
      logger.debugError("Error handling request: ", error);

      if (error instanceof RequestBodyError) {
        if (!res.headersSent && !res.destroyed) {
          rejectRequest(error.statusCode, error.message);
        }
      } else if (!res.headersSent) {
        res.writeHead(500, { "Content-Type": "application/json" });
        res.end(
          JSON.stringify({
            jsonrpc: "2.0",
            error: {
              code: -32603,
              message: "Internal server error",
            },
            id: null,
          })
        );
      }
    }
  });

  // Allow callers to configure the server before binding (e.g. set timeouts)
  if (configureServer) {
    configureServer(httpServer);
  }
  // Callers may keep idle connections alive, but cannot disable upload limits.
  httpServer.headersTimeout = REQUEST_BODY_TIMEOUT_MS;
  httpServer.requestTimeout = REQUEST_BODY_TIMEOUT_MS;

  // Start listening
  logger.debug(`Attempting to bind to port ${port}...`);
  httpServer.listen(port, "127.0.0.1", () => {
    logger.debug(`=== ${serverLabel} HTTP Server Started Successfully ===`);
    logger.debug(`HTTP server listening on http://127.0.0.1:${port}`);
    logger.debug(`MCP endpoint: POST http://127.0.0.1:${port}/`);
    logger.debug(`Server is ready to accept requests`);
  });

  // Handle bind errors
  httpServer.on("error", error => {
    /** @type {NodeJS.ErrnoException} */
    const errnoError = error;
    if (errnoError.code === "EADDRINUSE") {
      logger.debugError(`ERROR: Port ${port} is already in use. `, error);
    } else if (errnoError.code === "EACCES") {
      logger.debugError(`ERROR: Permission denied to bind to port ${port}. `, error);
    } else {
      logger.debugError(`ERROR: Failed to start HTTP server: `, error);
    }
    process.exit(1);
  });

  // Handle shutdown gracefully
  function shutdown() {
    logger.debug("Received shutdown signal, shutting down...");
    httpServer.close(() => {
      logger.debug("HTTP server closed");
      process.exit(0);
    });
  }
  process.once("SIGINT", shutdown);
  process.once("SIGTERM", shutdown);
  httpServer.once("close", () => {
    process.removeListener("SIGINT", shutdown);
    process.removeListener("SIGTERM", shutdown);
  });

  return httpServer;
}

/**
 * Log detailed startup error information and re-throw.
 *
 * @param {unknown} error - The caught error
 * @param {string} namespace - Logger namespace for the error logger (e.g. "mcp-scripts-startup-error")
 * @param {Object} context - Key/value pairs to include in log output
 * @returns {never}
 */
function logStartupError(error, namespace, context) {
  const errorLogger = createLogger(namespace);
  errorLogger.debugError("FATAL ERROR: Failed to start HTTP Server: ", error);
  if (error && typeof error === "object") {
    if ("constructor" in error && /** @type {any} */ error.constructor) {
      errorLogger.debug(`Error type: ${/** @type {any} */ error.constructor.name}`);
    }
    if ("message" in error) {
      errorLogger.debug(`Error message: ${/** @type {any} */ error.message}`);
    }
    if ("stack" in error && /** @type {any} */ error.stack) {
      errorLogger.debug(`Stack trace:\n${/** @type {any} */ error.stack}`);
    }
    if ("code" in error && /** @type {any} */ error.code) {
      errorLogger.debug(`Error code: ${/** @type {any} */ error.code}`);
    }
  }
  for (const [key, value] of Object.entries(context)) {
    errorLogger.debug(`${key}: ${value}`);
  }
  throw error;
}

module.exports = {
  runHttpServer,
  logStartupError,
  requireHttpApiKey,
  MAX_REQUEST_BODY_BYTES,
  REQUEST_BODY_TIMEOUT_MS,
};
