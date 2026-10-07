"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");

/** @param {unknown} value @returns {string} */
function validateModelIdentifier(value) {
  if (typeof value !== "string") return "";
  // Reject rather than repair telemetry that could forge footer marker fields.
  if (!value || value.length > 128 || /[^A-Za-z0-9._/:@-]/.test(value)) return "";
  return value;
}

function readFallbackMetadata(filePath) {
  try {
    return JSON.parse(require("fs").readFileSync(filePath, "utf8"));
  } catch (error) {
    throw new Error(`Cannot read model fallback metadata '${filePath}': ${getErrorMessage(error)}`, { cause: error });
  }
}

function recordFallbackModel(model, env, infoPath) {
  const fs = require("fs");
  const info = fs.existsSync(infoPath) ? readFallbackMetadata(infoPath) : {};
  const phase = env.GH_AW_PHASE || "agent";
  if (phase === "agent") {
    info.model = model;
    info.fallback_model = model;
  } else {
    info[`${phase}_fallback_model`] = model;
  }
  try {
    fs.mkdirSync(require("path").dirname(infoPath), { recursive: true });
    fs.writeFileSync(infoPath, JSON.stringify(info, null, 2));
  } catch (error) {
    throw new Error(`Cannot record model fallback metadata '${infoPath}': ${getErrorMessage(error)}`, { cause: error });
  }
}

function getFallbackModel(infoPath = `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`, phase = "agent") {
  const fs = require("fs");
  if (!fs.existsSync(infoPath)) return "";
  const info = readFallbackMetadata(infoPath);
  const model = phase === "agent" ? info.fallback_model : info[`${phase}_fallback_model`];
  return validateModelIdentifier(model);
}

function recordFallbackModelFromUsage(content, env = process.env, infoPath = `${env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`, logger = console.warn) {
  let model = "";
  for (const line of content.split("\n")) {
    if (!line.includes('"model_fallback"')) continue;
    let entry;
    try {
      entry = JSON.parse(line);
    } catch (error) {
      logger(`Skipping malformed AWF fallback usage record: ${getErrorMessage(error)}`);
      continue;
    }
    if (typeof entry?._schema !== "string" || !entry._schema.startsWith("token-usage/") || !entry.model_fallback || Number(entry.status) >= 400) continue;
    const validated = validateModelIdentifier(entry.model || entry.model_fallback.model);
    if (validated) model = validated;
  }
  if (model) recordFallbackModel(model, env, infoPath);
  return model;
}

module.exports = { validateModelIdentifier, getFallbackModel, recordFallbackModelFromUsage };
