// @ts-check
/// <reference types="@actions/github-script" />

const { createLogger } = require("./mcp_logger.cjs");
const moduleLogger = createLogger("mcp_http_transport");

// Log immediately at module load time
moduleLogger.debug("Module is being loaded");

/**
 * MCP HTTP Transport Implementation
 *
 * This module provides the HTTP transport layer for the MCP (Model Context Protocol),
 * removing the dependency on @modelcontextprotocol/sdk.
 *
 * Features:
 * - HTTP request/response handling
 * - Session management (stateful and stateless modes)
 * - JSON-RPC 2.0 compatible
 *
 * References:
 * - MCP Specification: https://spec.modelcontextprotocol.io
 * - JSON-RPC 2.0: https://www.jsonrpc.org/specification
 */

const http = require("http");
const { randomUUID } = require("crypto");
const { createServer, registerTool, handleRequest } = require("./mcp_server_core.cjs");
const { ERR_SYSTEM } = require("./error_codes.cjs");

/**
 * Simple MCP Server wrapper that provides a class-like interface
 * compatible with the HTTP transport, backed by mcp_server_core functions.
 */
class MCPServer {
  /**
   * @param {Object} serverInfo - Server metadata
   * @param {string} serverInfo.name - Server name
   * @param {string} serverInfo.version - Server version
   * @param {Object} [options] - Server options
   * @param {Object} [options.capabilities] - Server capabilities
   * @param {string} [options.logDir] - Log directory path
   * @param {(toolName: string, args: any, tool?: any) => any} [options.normalizeArguments] - Optional tool argument normalizer
   * @param {(message: string) => string} [options.redactLogMessage] - Optional diagnostic payload redactor
   */
  constructor(serverInfo, options = {}) {
    // Extract logDir for createServer, keep capabilities for this class
    const { capabilities, logDir, normalizeArguments, redactLogMessage } = options;
    this._coreServer = createServer(serverInfo, { logDir, normalizeArguments, redactLogMessage });
    this.serverInfo = serverInfo;
    this.capabilities = capabilities || { tools: {} };
    this.tools = new Map();
    this.transport = null;
    this.initialized = false;
  }

  /**
   * Register a tool with the server
   * @param {string} name - Tool name
   * @param {string} description - Tool description
   * @param {Object} inputSchema - JSON Schema for tool input
   * @param {Function} handler - Async function that handles tool calls
   */
  tool(name, description, inputSchema, handler) {
    this.tools.set(name, {
      name,
      description,
      inputSchema,
      handler,
    });
    // Also register with the core server
    registerTool(this._coreServer, {
      name,
      description,
      inputSchema,
      handler,
    });
  }

  /**
   * Connect to a transport
   * @param {any} transport - Transport instance (must have setServer and start methods)
   */
  async connect(transport) {
    const logger = createLogger("MCPServer");
    logger.debug("Starting connect...");
    this.transport = transport;
    logger.debug("Set transport");
    transport.setServer(this);
    logger.debug("Called setServer");
    await transport.start();
    logger.debug("Transport.start() completed");
  }

  /**
   * Handle an incoming JSON-RPC request
   * @param {Object} request - JSON-RPC request
   * @returns {Promise<Object|null>} JSON-RPC response or null for notifications
   */
  async handleRequest(request) {
    // Track initialization state
    if (request.method === "initialize") {
      this.initialized = true;
    }
    // Delegate to core server's handleRequest function
    return handleRequest(this._coreServer, request);
  }
}

/**
 * MCP HTTP Transport implementation
 * Handles HTTP requests and converts them to MCP protocol messages
 */
class MCPHTTPTransport {
  /**
   * @param {Object} options - Transport options
   * @param {Function} [options.sessionIdGenerator] - Function that generates session IDs (undefined for stateless)
   * @param {boolean} [options.enableJsonResponse] - Enable JSON responses instead of SSE (default: true for simplicity)
   * @param {boolean} [options.enableDnsRebindingProtection] - Enable DNS rebinding protection (default: false)
   * @param {(message: string) => string} [options.redactLogMessage] - Optional diagnostic payload redactor
   */
  constructor(options = {}) {
    this.sessionIdGenerator = options.sessionIdGenerator;
    this.enableJsonResponse = options.enableJsonResponse !== false; // Default to true
    this.enableDnsRebindingProtection = options.enableDnsRebindingProtection || false;
    this.server = null;
    this.sessionId = null;
    this.started = false;
    this.logger = createLogger("MCPHTTPTransport", options.redactLogMessage);
  }

  /**
   * Set the MCP server instance
   * @param {MCPServer} server - MCP server instance
   */
  setServer(server) {
    this.server = server;
  }

  /**
   * Start the transport
   */
  async start() {
    const logger = createLogger("MCPHTTPTransport");
    logger.debug(`Called, started=${this.started}`);
    if (this.started) {
      throw new Error(`${ERR_SYSTEM}: Transport already started`);
    }
    this.started = true;
    logger.debug("Set started=true");
  }

  /**
   * Handle an incoming HTTP request
   * @param {http.IncomingMessage} req - HTTP request
   * @param {http.ServerResponse} res - HTTP response
   * @param {Object} [parsedBody] - Pre-parsed request body
   */
  async handleRequest(req, res, parsedBody) {
    // Only handle POST requests for MCP protocol
    if (req.method !== "POST") {
      res.writeHead(405, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "Method not allowed" }));
      return;
    }

    try {
      // Parse request body if not already parsed
      let body = parsedBody;
      if (!body) {
        const chunks = [];
        for await (const chunk of req) {
          chunks.push(chunk);
        }
        const bodyStr = Buffer.concat(chunks).toString();
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
      }

      if (!body) {
        res.writeHead(400, { "Content-Type": "application/json" });
        res.end(
          JSON.stringify({
            jsonrpc: "2.0",
            error: {
              code: -32600,
              message: "Invalid Request: Empty request body",
            },
            id: null,
          })
        );
        return;
      }

      // Validate JSON-RPC structure
      if (!body.jsonrpc || body.jsonrpc !== "2.0") {
        res.writeHead(400, { "Content-Type": "application/json" });
        res.end(
          JSON.stringify({
            jsonrpc: "2.0",
            error: {
              code: -32600,
              message: "Invalid Request: jsonrpc must be '2.0'",
            },
            id: body.id || null,
          })
        );
        return;
      }

      // Handle session management for stateful mode
      if (this.sessionIdGenerator) {
        // For initialize, generate a new session ID
        if (body.method === "initialize") {
          this.sessionId = this.sessionIdGenerator();
        } else {
          // For other methods, validate session ID
          const requestSessionId = req.headers["mcp-session-id"];
          if (!requestSessionId) {
            res.writeHead(400, { "Content-Type": "application/json" });
            res.end(
              JSON.stringify({
                jsonrpc: "2.0",
                error: {
                  code: -32600,
                  message: "Invalid Request: Missing Mcp-Session-Id header",
                },
                id: body.id || null,
              })
            );
            return;
          }

          if (requestSessionId !== this.sessionId) {
            res.writeHead(404, { "Content-Type": "application/json" });
            res.end(
              JSON.stringify({
                jsonrpc: "2.0",
                error: {
                  code: -32001,
                  message: "Session not found",
                },
                id: body.id || null,
              })
            );
            return;
          }
        }
      }

      // Process the request through the MCP server
      const response = await this.server?.handleRequest(body);

      // Handle notifications (null response means no reply needed)
      if (response === null || response === undefined) {
        res.writeHead(204); // No Content
        res.end();
        return;
      }

      // Set response headers
      const headers = { "Content-Type": "application/json" };
      if (this.sessionId) {
        headers["mcp-session-id"] = this.sessionId;
      }

      res.writeHead(200, headers);
      res.end(JSON.stringify(response));
    } catch (error) {
      this.logger.debugError("MCP HTTP Transport error: ", error);

      if (!res.headersSent) {
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
  }
}

module.exports = {
  MCPServer,
  MCPHTTPTransport,
};
