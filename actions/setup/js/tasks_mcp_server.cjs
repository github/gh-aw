// @ts-check
"use strict";

const http = require("node:http");
const crypto = require("node:crypto");
const { isRecord } = require("./tasks_config.cjs");
const { runTaskProcess } = require("./tasks_process.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * @param {{
 * tasks: Record<string, import("./tasks_config.cjs").TaskDefinition>,
 * executables: Record<string, string>, cwd: string, env: NodeJS.ProcessEnv,
 * token: string, signal: AbortSignal,
 * log?: (event: Record<string, unknown>) => void,
 * onError?: (error: Error) => void,
 * }} options
 */
async function startTasksServer({ tasks, executables, cwd, env, token, signal, onError, log = event => process.stderr.write(`[tasks] ${JSON.stringify(event)}\n`) }) {
  if (!/^[a-f0-9]{64}$/.test(token)) throw new Error("Tasks server requires a private 256-bit token");
  /** @type {ReturnType<typeof runTaskProcess> | undefined} */
  let active;
  /** @type {Map<string | number | null, AbortController>} */
  const calls = new Map();
  const names = Object.keys(tasks).sort();
  const tool = {
    name: "run_task",
    description: "Run a fixed workflow task inside the agent sandbox. " + names.map(name => `${name}: ${tasks[name].description}`).join("; "),
    inputSchema: { type: "object", additionalProperties: false, required: ["name"], properties: { name: { type: "string", enum: names } } },
  };
  const server = http.createServer(async (request, response) => {
    const authorization = request.headers.authorization || "";
    const expected = `Bearer ${token}`;
    if (Buffer.byteLength(authorization) !== Buffer.byteLength(expected) || !crypto.timingSafeEqual(Buffer.from(authorization), Buffer.from(expected))) {
      response.writeHead(401).end();
      return;
    }
    if (request.url !== "/mcp" || request.method !== "POST") {
      response.writeHead(405).end();
      return;
    }
    /** @type {string | number | null} */
    let id = null;
    let errorCode = -32600;
    const controller = new AbortController();
    response.on("close", () => {
      if (!response.writableEnded) controller.abort(new Error("MCP client disconnected"));
    });
    try {
      /** @type {Buffer[]} */
      const chunks = [];
      let size = 0;
      for await (const chunk of request) {
        size += chunk.length;
        if (size > 16 * 1024) throw new Error("MCP request exceeds 16 KiB");
        chunks.push(chunk);
      }
      const message = JSON.parse(Buffer.concat(chunks).toString("utf8"));
      if (!isRecord(message) || message.jsonrpc !== "2.0" || typeof message.method !== "string") throw new Error("Invalid JSON-RPC request");
      const requestId = message.id ?? null;
      if (requestId !== null && typeof requestId !== "string" && typeof requestId !== "number") throw new Error("Invalid JSON-RPC request id");
      id = requestId;
      errorCode = -32602;
      if (!Object.hasOwn(message, "id")) {
        if (message.method === "notifications/cancelled" && isRecord(message.params)) {
          const cancelled = message.params.requestId;
          if (typeof cancelled === "string" || typeof cancelled === "number") calls.get(cancelled)?.abort(new Error("MCP request cancelled"));
        }
        response.writeHead(202).end();
        return;
      }
      let result;
      switch (message.method) {
        case "initialize":
          result = { protocolVersion: "2025-03-26", capabilities: { tools: {} }, serverInfo: { name: "gh-aw-tasks", version: "1.0.0" } };
          break;
        case "ping":
          result = {};
          break;
        case "tools/list":
          result = { tools: [tool] };
          break;
        case "tools/call": {
          const params = message.params;
          if (!isRecord(params) || params.name !== "run_task" || !isRecord(params.arguments) || Object.keys(params.arguments).length !== 1 || typeof params.arguments.name !== "string" || !Object.hasOwn(tasks, params.arguments.name)) {
            throw new Error("run_task requires exactly one configured task name; no execution overrides are accepted");
          }
          if (active) throw new Error("Another task is running; invoke tasks sequentially");
          const name = params.arguments.name;
          const taskSignal = AbortSignal.any([signal, controller.signal]);
          calls.set(id, controller);
          log({ event: "task.start", name });
          try {
            active = runTaskProcess(executables[name], tasks[name], { cwd, env, signal: taskSignal });
            const completed = await active;
            result = { content: [{ type: "text", text: JSON.stringify({ name, ...completed }) }], isError: completed.exitCode !== 0 };
            log({ event: "task.finish", name, exitCode: completed.exitCode, durationMs: completed.durationMs });
          } catch (error) {
            log({ event: "task.error", name, message: getErrorMessage(error) });
            if (error instanceof AggregateError) onError?.(error);
            result = { content: [{ type: "text", text: JSON.stringify({ name, error: getErrorMessage(error) }) }], isError: true };
          } finally {
            calls.delete(id);
            active = undefined;
          }
          break;
        }
        default:
          errorCode = -32601;
          throw new Error(`Unsupported MCP method: ${message.method}`);
      }
      response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ jsonrpc: "2.0", id, result }));
    } catch (error) {
      log({ event: "mcp.error", message: getErrorMessage(error) });
      response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ jsonrpc: "2.0", id, error: { code: error instanceof SyntaxError ? -32700 : errorCode, message: getErrorMessage(error) } }));
    }
  });
  server.requestTimeout = 0;
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve(undefined);
    });
    server.on("error", error => {
      log({ event: "server.error", message: error.message });
      onError?.(error);
    });
  });
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Tasks MCP server has no loopback address");
  /** @type {Promise<void> | undefined} */
  let closing;
  return {
    config: { type: "http", url: `http://127.0.0.1:${address.port}/mcp`, headers: { Authorization: `Bearer ${token}` }, tools: ["run_task"], timeout: 605_000 },
    close() {
      closing ??= (async () => {
        for (const controller of calls.values()) controller.abort(new Error("Tasks server stopped"));
        await Promise.allSettled(active ? [active] : []);
        server.closeAllConnections();
        await new Promise((resolve, reject) => server.close(error => (error ? reject(error) : resolve(undefined))));
      })();
      return closing;
    },
  };
}

module.exports = { startTasksServer };
