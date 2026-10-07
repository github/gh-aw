// @ts-check
"use strict";

const http = require("node:http");
const { spawn } = require("node:child_process");
const { once } = require("node:events");
const { constants } = require("node:os");
const { GeminiProtocolError, toChatRequest, fromChatResponse } = require("./gemini_copilot_protocol.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const MAX_BODY_BYTES = 16 * 1024 * 1024;

/** @param {http.IncomingMessage} request */
async function readJSON(request) {
  const chunks = [];
  let length = 0;
  for await (const chunk of request) {
    length += chunk.length;
    if (length > MAX_BODY_BYTES) throw new GeminiProtocolError("Gemini request exceeds the 16 MiB bridge limit.");
    chunks.push(chunk);
  }
  try {
    return JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch (error) {
    throw new GeminiProtocolError(`Invalid Gemini JSON: ${getErrorMessage(error)}`);
  }
}

/**
 * Only the credential-isolated AWF Copilot sidecar receives inference requests.
 * The bridge never forwards the CLI's placeholder key or incoming auth headers.
 *
 * @param {{model: string, upstream?: string, fetchImpl?: typeof fetch}} options
 */
function createBridge({ model, upstream = "http://host.docker.internal:10002", fetchImpl = fetch }) {
  let warnedGenerationDefaults = false;
  return http.createServer(async (request, response) => {
    const match = /^\/v1(?:beta)?\/models\/[^/:]+:(generateContent|streamGenerateContent)(?:\?.*)?$/.exec(request.url || "");
    if (request.method !== "POST" || !match) {
      response.writeHead(501, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ error: { code: 501, message: "Gemini Copilot bridge supports generateContent and streamGenerateContent only." } }));
      return;
    }
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 120000);
    response.on("close", () => controller.abort());
    response.on("error", error => {
      controller.abort();
      process.stderr.write(`[gemini-copilot] Response failed: ${getErrorMessage(error)}\n`);
    });
    try {
      const geminiRequest = await readJSON(request);
      const body = toChatRequest(geminiRequest, model);
      if (!warnedGenerationDefaults && (geminiRequest.generationConfig?.topK !== undefined || geminiRequest.generationConfig?.thinkingConfig !== undefined)) {
        warnedGenerationDefaults = true;
        process.stderr.write("[gemini-copilot] Gemini topK and thinkingConfig use Copilot model defaults.\n");
      }
      const result = await fetchImpl(`${upstream}/chat/completions`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
        signal: controller.signal,
        redirect: "error",
      });
      if (!result.ok) {
        response.writeHead(result.status, { "Content-Type": "application/json", ...(result.headers.has("retry-after") ? { "Retry-After": result.headers.get("retry-after") || "" } : {}) });
        response.end(JSON.stringify({ error: { code: result.status, message: await result.text() } }));
        return;
      }
      let translated;
      try {
        translated = fromChatResponse(await result.json(), model);
      } catch (error) {
        throw new Error(`Invalid Copilot response: ${getErrorMessage(error)}`, { cause: error });
      }
      // Buffer one completion per turn; the Gemini SDK still receives its native SSE format.
      const streaming = match[1] === "streamGenerateContent";
      response.writeHead(200, { "Content-Type": streaming ? "text/event-stream" : "application/json" });
      response.end(streaming ? `data: ${JSON.stringify(translated)}\n\n` : JSON.stringify(translated));
    } catch (error) {
      const status = error instanceof GeminiProtocolError ? 400 : 502;
      const message = getErrorMessage(error);
      process.stderr.write(`[gemini-copilot] ${message}\n`);
      if (!response.destroyed) {
        response.writeHead(status, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ error: { code: status, message } }));
      }
    } finally {
      clearTimeout(timeout);
    }
  });
}

/** @param {string[]} args @param {NodeJS.ProcessEnv} [env] */
async function main(args, env = process.env) {
  const model = (env.GH_AW_GEMINI_COPILOT_MODEL || "").replace(/^copilot\//i, "").trim();
  if (!model.startsWith("gemini") || !args.length) throw new Error("Gemini Copilot routing requires a copilot/gemini* model and a CLI command.");
  const server = createBridge({ model });
  /** @type {import("node:child_process").ChildProcess | undefined} */
  let child;
  /** @param {NodeJS.Signals} signal */
  const stop = signal => child?.kill(signal);
  const interrupt = () => stop("SIGINT");
  const terminate = () => stop("SIGTERM");
  try {
    server.listen(0, "127.0.0.1");
    await once(server, "listening");
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("Gemini Copilot bridge did not bind a loopback port.");
    child = spawn(args[0], args.slice(1), {
      stdio: "inherit",
      env: {
        ...env,
        GEMINI_MODEL: model,
        GEMINI_API_KEY: "gh-aw-copilot-placeholder",
        GOOGLE_GEMINI_BASE_URL: `http://127.0.0.1:${address.port}`,
        GOOGLE_GENAI_USE_VERTEXAI: "false",
        GOOGLE_GENAI_USE_GCA: "false",
      },
    });
    process.on("SIGINT", interrupt);
    process.on("SIGTERM", terminate);
    /** @type {[number | null, NodeJS.Signals | null]} */
    const [code, signal] = await new Promise((resolve, reject) => {
      child?.once("error", reject);
      child?.once("exit", (code, signal) => resolve([code, signal]));
    });
    if (code === null && !signal) throw new Error("Gemini CLI exited without a status or signal.");
    return code === null && signal ? 128 + constants.signals[signal] : code;
  } finally {
    process.off("SIGINT", interrupt);
    process.off("SIGTERM", terminate);
    if (server.listening) {
      server.closeAllConnections();
      await new Promise((resolve, reject) => server.close(error => (error ? reject(error) : resolve(undefined))));
    }
  }
}

if (require.main === module) {
  main(process.argv.slice(2))
    .then(code => {
      process.exitCode = code;
    })
    .catch(error => {
      process.stderr.write(`[gemini-copilot] ${getErrorMessage(error)}\n`);
      process.exitCode = 1;
    });
}

module.exports = { createBridge, main };
