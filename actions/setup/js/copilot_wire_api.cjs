// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { getCatalogModelEntry } = require("./awf_reflect.cjs");
const { getAWFRoutingModel, getAWFRoutingModelAmbiguityError } = require("./awf_model_routing.cjs");
const { redactDiagnosticText } = require("./diagnostic_sanitization.cjs");

const WIRE_ENDPOINTS = { responses: "/responses", completions: "/chat/completions" };
const MODEL_ENDPOINT_FIX = "Pin a compatible model, remove the COPILOT_PROVIDER_WIRE_API override, or upgrade gh-aw.";

function getSupportedEndpoints(reflectData, model) {
  model = typeof model === "string" ? model.split("?")[0] : "";
  if (!model || getAWFRoutingModelAmbiguityError(reflectData, model)) return null;
  const entry = getAWFRoutingModel(reflectData, model);
  return entry?.candidate_metadata_complete === true && Array.isArray(entry.supported_endpoints) && entry.supported_endpoints.every(endpoint => typeof endpoint === "string") ? entry.supported_endpoints : null;
}

function recordModelEndpointMismatch(record, env = process.env) {
  try {
    const file = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "agent", "model-endpoint-mismatch.json");
    fs.mkdirSync(path.dirname(file), { recursive: true });
    const safeRecord = Object.fromEntries(
      Object.entries(record).map(([key, value]) => [key, typeof value === "string" ? redactDiagnosticText(value).slice(0, 2500) : Array.isArray(value) ? value.map(endpoint => redactDiagnosticText(endpoint).slice(0, 200)) : value])
    );
    fs.writeFileSync(file, JSON.stringify({ category: "model_endpoint_mismatch", ...safeRecord, fix: MODEL_ENDPOINT_FIX }) + "\n");
    return true;
  } catch {
    return false;
  }
}

/**
 * Prefer complete AWF endpoint metadata, retaining the catalog/name preference
 * only when supported or when endpoint metadata is unavailable.
 * @param {{modelsJson: Record<string, unknown>|null, awfReflectData?: any, configuredModel?: string, overrideSource?: string, logger?: (msg: string) => void}} options
 */
function applyCopilotWireAPI({ modelsJson, awfReflectData = null, configuredModel = process.env.COPILOT_MODEL || "", overrideSource = "override", logger = () => {} }) {
  const model = (process.env.COPILOT_MODEL || "").trim();
  const override = process.env.COPILOT_PROVIDER_WIRE_API;
  const supported = getSupportedEndpoints(awfReflectData, model);
  const catalog = getCatalogModelEntry(modelsJson, model, "github-copilot");
  const catalogAPI = typeof catalog?.wire_api === "string" ? catalog.wire_api : null;
  const nameAPI = /^gpt-(?:[5-9]|\d{2,})(?:[.-]|$)/i.test(model.split("?")[0]) ? "responses" : null;
  let wireAPI = override || catalogAPI || nameAPI;
  let source = override ? overrideSource : catalogAPI ? "catalog" : nameAPI ? "model-name rule" : "CLI default";
  if (supported !== null) {
    const usable = Object.keys(WIRE_ENDPOINTS).filter(api => supported.includes(WIRE_ENDPOINTS[api]));
    if (!override) {
      const preferred = wireAPI || "completions";
      if (usable.includes(preferred)) {
        wireAPI = preferred;
      } else {
        wireAPI = usable[0];
        source = "AWF /reflect";
      }
    }
    if (!wireAPI || !supported.includes(WIRE_ENDPOINTS[wireAPI])) {
      const detail = redactDiagnosticText(
        `Model endpoint mismatch: configured model '${configuredModel}'${configuredModel !== model ? ` resolved to '${model}'` : ""}; COPILOT_PROVIDER_WIRE_API=${wireAPI || "(none)"} (${source}, ${WIRE_ENDPOINTS[wireAPI] || "(no usable endpoint)"}); supported endpoints: [${supported.join(", ")}].`
      );
      recordModelEndpointMismatch({ phase: "startup", configured_model: configuredModel, resolved_model: model, wire_api: wireAPI || "", wire_api_source: source, supported_endpoints: supported, detail });
      throw new Error(`${detail} ${MODEL_ENDPOINT_FIX}`);
    }
  }
  if (override) {
    logger(`COPILOT_PROVIDER_WIRE_API already set to ${override} — source=${source}${supported !== null ? " (verified against AWF /reflect)" : ""}`);
  } else if (wireAPI && model) {
    process.env.COPILOT_PROVIDER_WIRE_API = wireAPI;
    logger(`auto-configuring COPILOT_PROVIDER_WIRE_API=${wireAPI} for model ${model} — source=${source}`);
  } else {
    logger(`COPILOT_PROVIDER_WIRE_API unset — source=CLI default`);
  }
}

/**
 * @param {{awfReflectData: any, agentsDir?: string, logger?: (message: string) => void}} options
 */
function warnCopilotSubagentEndpoints({ awfReflectData, agentsDir = path.join(process.env.GITHUB_WORKSPACE || process.cwd(), ".github", "agents"), logger = () => {} }) {
  const mainModel = process.env.COPILOT_MODEL || "";
  const mainEndpoints = getSupportedEndpoints(awfReflectData, mainModel);
  const endpoint = WIRE_ENDPOINTS[process.env.COPILOT_PROVIDER_WIRE_API || "completions"];
  if (!mainEndpoints || !mainEndpoints.includes(endpoint)) return;
  try {
    for (const file of fs.readdirSync(agentsDir, { withFileTypes: true })) {
      if (!file.isFile() || !file.name.endsWith(".md")) continue;
      const text = fs.readFileSync(path.join(agentsDir, file.name), "utf8");
      const frontmatter = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/.exec(text)?.[1];
      const scalar = frontmatter?.match(/^model:[ \t]*(.+)$/m)?.[1]?.trim();
      const model = scalar?.match(/^(?:"([^"]+)"|'([^']+)'|([^#\s]+))(?:\s*(?:#.*)?)?$/);
      const agentModel = model?.[1] || model?.[2] || model?.[3];
      const supported = getSupportedEndpoints(awfReflectData, agentModel);
      if (supported && !supported.includes(endpoint)) {
        logger(
          `warning: custom agent '${file.name}' model '${agentModel}' supports [${supported.join(", ")}], incompatible with main model '${mainModel}' session endpoint ${endpoint}. See https://github.com/github/gh-aw/issues/67460 and https://github.com/github/copilot-cli/issues/5103`
        );
      }
    }
  } catch (err) {
    logger(`custom agent endpoint validation skipped: ${err instanceof Error ? err.message : "unable to read optional custom agents"}`);
  }
}

module.exports = { applyCopilotWireAPI, getSupportedEndpoints, warnCopilotSubagentEndpoints, recordModelEndpointMismatch, MODEL_ENDPOINT_FIX };
