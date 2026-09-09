// @ts-check

/**
 * Pi Agent Core Driver (inner harness)
 *
 * Standalone program launched by the pi engine when engine.driver is set to
 * "pi_agent_core_driver.cjs".  Uses @earendil-works/pi-agent-core to run a
 * pi agent session directly from Node.js — no pi CLI required.
 *
 * The driver reads configuration from environment variables and outputs a
 * streaming JSONL log compatible with parse_pi_log.cjs so that gh-aw's
 * existing log parsing and step-summary pipeline works unchanged.
 *
 * Environment variables:
 *   GH_AW_PROMPT              — path to the prompt file
 *   GH_AW_PI_MODEL            — model string in "provider/model" format
 *                               (e.g. "copilot/claude-sonnet-4-20250514")
 *                               Preferred over PI_MODEL.
 *   PI_MODEL                  — legacy fallback model string
 *   PI_CODING_AGENT_DIR       — directory containing models.json for AWF
 *                               gateway routing (set by pi engine when
 *                               firewall is enabled)
 *   COPILOT_GITHUB_TOKEN      — Copilot / GitHub token
 *   GITHUB_TOKEN              — fallback token for Copilot provider
 *   ANTHROPIC_API_KEY         — Anthropic API key
 *   CODEX_API_KEY             — OpenAI/Codex API key
 *   OPENAI_API_KEY            — OpenAI API key (alias for CODEX_API_KEY)
 *
 * JSONL output format (compatible with parse_pi_log.cjs):
 *   { type: "init",        model, session_id }
 *   { type: "assistant",   content, delta }
 *   { type: "tool_use",    tool_name, tool_id, parameters }
 *   { type: "tool_result", tool_id, status, output }
 *   { type: "result",      stats: { input_tokens, output_tokens, duration_ms, turns } }
 */

"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");

// ---------------------------------------------------------------------------
// Logging helpers
// ---------------------------------------------------------------------------

/** @param {string} msg */
function log(msg) {
  process.stderr.write(`[pi-agent-core-driver] ${msg}\n`);
}

/** @param {unknown} obj */
function emitJsonl(obj) {
  process.stdout.write(JSON.stringify(obj) + "\n");
}

// ---------------------------------------------------------------------------
// models.json parsing
// ---------------------------------------------------------------------------

/**
 * Provider config parsed from a models.json "providers" entry.
 * @typedef {{
 *   api: string,
 *   baseUrl: string,
 *   apiKey: string,
 *   modelId: string,
 *   providerName: string,
 * }} GatewayProviderConfig
 */

/**
 * Read and parse the AWF gateway models.json written by the pi engine.
 * Returns null when the file is absent or unparseable.
 *
 * @param {string} agentDir
 * @returns {GatewayProviderConfig|null}
 */
function readGatewayConfig(agentDir) {
  if (!agentDir) return null;

  const modelsPath = path.join(agentDir, "models.json");
  let raw;
  try {
    raw = fs.readFileSync(modelsPath, "utf8");
  } catch {
    return null;
  }

  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    log(`warning: failed to parse ${modelsPath}: ${getErrorMessage(err)}`);
    return null;
  }

  // models.json schema: { providers: { <name>: { api, baseUrl, apiKey, models: [{id}] } } }
  const providers = parsed && typeof parsed === "object" ? parsed.providers : null;
  if (!providers || typeof providers !== "object") {
    return null;
  }

  // Use the first provider entry (gh-aw generates exactly one: "aw-gateway").
  const providerName = Object.keys(providers)[0];
  if (!providerName) return null;

  const config = providers[providerName];
  if (!config || typeof config !== "object") return null;

  const api = typeof config.api === "string" ? config.api : "openai-completions";
  const baseUrl = typeof config.baseUrl === "string" ? config.baseUrl : "";
  const apiKey = typeof config.apiKey === "string" ? config.apiKey : "";
  const modelId = Array.isArray(config.models) && config.models.length > 0 && typeof config.models[0].id === "string" ? config.models[0].id : "";

  if (!baseUrl || !modelId) {
    log(`warning: models.json at ${modelsPath} is missing baseUrl or model id`);
    return null;
  }

  return { api, baseUrl, apiKey, modelId, providerName };
}

// ---------------------------------------------------------------------------
// API key resolution
// ---------------------------------------------------------------------------

/**
 * Build a getApiKey callback for the Agent.
 *
 * @param {GatewayProviderConfig|null} gatewayConfig
 * @returns {(provider: string) => string|undefined}
 */
function buildGetApiKey(gatewayConfig) {
  return provider => {
    // Gateway provider used in AWF firewall mode.
    if (gatewayConfig && provider === gatewayConfig.providerName) {
      // The models.json apiKey field contains the NAME of the env var that
      // holds the secret (Pi CLI's resolveConfigValue() semantics).
      const envVarName = gatewayConfig.apiKey;
      if (envVarName) {
        const value = process.env[envVarName];
        if (value) return value;
      }
    }

    // Built-in providers used in no-firewall mode.
    switch (provider) {
      case "github-copilot":
      case "copilot":
        return process.env.COPILOT_GITHUB_TOKEN || process.env.GITHUB_TOKEN;
      case "anthropic":
        return process.env.ANTHROPIC_API_KEY;
      case "openai":
      case "codex":
        return process.env.CODEX_API_KEY || process.env.OPENAI_API_KEY;
      default:
        return undefined;
    }
  };
}

// ---------------------------------------------------------------------------
// Model construction
// ---------------------------------------------------------------------------

/**
 * Resolve the pi-ai Model object for the configured model string.
 *
 * When an AWF gateway config is present (models.json), builds a custom model
 * that routes LLM traffic to the gateway sidecar.  Otherwise, maps the
 * "provider/model" prefix to the corresponding pi-ai built-in provider name
 * so the driver can call the real LLM API without going through a gateway.
 *
 * @param {GatewayProviderConfig|null} gatewayConfig
 * @param {string} modelStr  GH_AW_PI_MODEL value (e.g. "anthropic/claude-sonnet-4")
 * @returns {Record<string, unknown>}  pi-ai Model object (structural typing)
 */
function buildModel(gatewayConfig, modelStr) {
  if (gatewayConfig) {
    // AWF firewall mode: route through the gateway sidecar.
    // The "apiKey" field is intentionally omitted here because getApiKey()
    // on the Agent constructor handles per-request key injection.
    return {
      id: gatewayConfig.modelId,
      name: gatewayConfig.modelId,
      api: gatewayConfig.api,
      provider: gatewayConfig.providerName,
      baseUrl: gatewayConfig.baseUrl,
      reasoning: false,
      input: ["text"],
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
      contextWindow: 200000,
      maxTokens: 8192,
    };
  }

  // No-firewall mode: use the pi-ai built-in provider resolved from the
  // provider prefix in GH_AW_PI_MODEL.  Strip the prefix for the model id.
  let provider = "github-copilot";
  let modelId = modelStr;
  const slashIdx = modelStr.indexOf("/");
  if (slashIdx > 0) {
    const prefix = modelStr.slice(0, slashIdx).toLowerCase();
    modelId = modelStr.slice(slashIdx + 1);
    switch (prefix) {
      case "anthropic":
        provider = "anthropic";
        break;
      case "openai":
      case "codex":
        provider = "openai";
        break;
      case "copilot":
      case "github-copilot":
      default:
        provider = "github-copilot";
        break;
    }
  }

  // Resolve the appropriate baseUrl for the provider.
  let baseUrl = "";
  switch (provider) {
    case "anthropic":
      baseUrl = process.env.ANTHROPIC_BASE_URL || "https://api.anthropic.com";
      break;
    case "openai":
      baseUrl = process.env.OPENAI_BASE_URL || "https://api.openai.com/v1";
      break;
    default:
      baseUrl = process.env.GITHUB_COPILOT_BASE_URL || "https://api.githubcopilot.com";
      break;
  }

  // Determine the pi-ai api type for the provider.
  // Real OpenAI models are only published under "openai-responses" in Pi's upstream
  // model catalog; OpenAI's Chat Completions endpoint rejects function tools whenever
  // reasoning_effort is anything other than "none" (see
  // https://developers.openai.com/api/docs/guides/responses-vs-chat-completions), so
  // routing the "openai" provider through /responses keeps tool calling working.
  // GitHub Copilot's gateway keeps its existing chat-completions-style protocol.
  let api = "openai-completions";
  if (provider === "anthropic") {
    api = "anthropic-messages";
  } else if (provider === "openai") {
    api = "openai-responses";
  }

  return {
    id: modelId,
    name: modelId,
    api,
    provider,
    baseUrl,
    reasoning: false,
    input: ["text"],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    contextWindow: 200000,
    maxTokens: 8192,
  };
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

async function main() {
  // --- Read configuration from environment ---

  const promptFile = process.env.GH_AW_PROMPT;
  if (!promptFile) {
    process.stderr.write("[pi-agent-core-driver] error: GH_AW_PROMPT is not set\n");
    process.exit(1);
  }

  let prompt;
  try {
    prompt = fs.readFileSync(promptFile, "utf8");
  } catch (err) {
    process.stderr.write(`[pi-agent-core-driver] error: failed to read prompt file ${promptFile}: ${getErrorMessage(err)}\n`);
    process.exit(1);
  }

  const modelStr = process.env.GH_AW_PI_MODEL || process.env.PI_MODEL || "";
  const agentDir = process.env.PI_CODING_AGENT_DIR || "";
  const gatewayConfig = readGatewayConfig(agentDir);

  if (gatewayConfig) {
    log(`gateway mode: provider=${gatewayConfig.providerName} baseUrl=${gatewayConfig.baseUrl} model=${gatewayConfig.modelId}`);
  } else {
    log(`native mode: model=${modelStr || "(not set)"}`);
  }

  // --- Dynamic import of ESM modules ---

  // @earendil-works/pi-agent-core and @earendil-works/pi-ai are ES modules;
  // use dynamic import() from a CommonJS entry point.
  // @ts-ignore — packages are auto-installed at runtime before this point
  const { Agent } = await import("@earendil-works/pi-agent-core");
  // Importing @earendil-works/pi-ai registers all built-in API providers
  // (openai-completions, anthropic-messages, etc.) so streamSimple works.
  // @ts-ignore — packages are auto-installed at runtime before this point
  await import("@earendil-works/pi-ai");

  // --- Set up agent ---

  const model = buildModel(gatewayConfig, modelStr);
  const getApiKey = buildGetApiKey(gatewayConfig);

  const sessionId = crypto.randomUUID();
  const startTimeMs = Date.now();
  let inputTokens = 0;
  let outputTokens = 0;
  let turns = 0;

  const agent = new Agent({
    initialState: {
      // systemPrompt is intentionally left empty; the gh-aw prompt contains
      // all task instructions.  Users can extend this driver to set a system
      // prompt if needed.
      systemPrompt: "",
      model,
    },
    getApiKey,
  });

  // --- Subscribe to events and emit JSONL ---

  agent.subscribe(event => {
    switch (event.type) {
      case "agent_start":
        emitJsonl({ type: "init", model: modelStr || model.id, session_id: sessionId });
        break;

      case "message_update": {
        const ae = event.assistantMessageEvent;
        if (ae && ae.type === "text_delta") {
          emitJsonl({ type: "assistant", content: ae.delta, delta: true });
        }
        break;
      }

      case "tool_execution_start":
        emitJsonl({
          type: "tool_use",
          tool_name: event.toolName,
          tool_id: event.toolCallId,
          parameters: event.args ?? {},
        });
        break;

      case "tool_execution_end": {
        const output = typeof event.result === "string" ? event.result : event.result !== null && event.result !== undefined ? JSON.stringify(event.result) : "";
        emitJsonl({
          type: "tool_result",
          tool_id: event.toolCallId,
          status: event.isError ? "error" : "success",
          output,
        });
        break;
      }

      case "turn_end": {
        turns++;
        // Accumulate token usage from the assistant message that ended the turn.
        const msg = event.message;
        if (msg && typeof msg === "object" && "usage" in msg && msg.usage && typeof msg.usage === "object") {
          const usage = /** @type {Record<string, number>} */ msg.usage;
          inputTokens += typeof usage.input === "number" ? usage.input : 0;
          outputTokens += typeof usage.output === "number" ? usage.output : 0;
        }
        break;
      }

      case "agent_end":
        emitJsonl({
          type: "result",
          stats: {
            input_tokens: inputTokens,
            output_tokens: outputTokens,
            duration_ms: Date.now() - startTimeMs,
            turns,
          },
        });
        break;

      default:
        break;
    }
  });

  // --- Run the agent ---

  log(`starting agent session: session_id=${sessionId}`);
  await agent.prompt(prompt);
  await agent.waitForIdle();
  log(`agent session complete: session_id=${sessionId} turns=${turns} input_tokens=${inputTokens} output_tokens=${outputTokens}`);
}

if (require.main === module) {
  main().catch(err => {
    process.stderr.write(`[pi-agent-core-driver] unhandled error: ${err instanceof Error && err.stack ? err.stack : getErrorMessage(err)}\n`);
    process.exit(1);
  });
}

module.exports = { buildGetApiKey, buildModel, readGatewayConfig };
