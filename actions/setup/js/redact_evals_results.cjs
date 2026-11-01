// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const { EVALS_OUTPUT_PATH, EVALS_STATUS_PATH } = require("./evals_constants.cjs");
const { main: redactWorkspaceSecrets, redactSecrets, redactBuiltInPatterns, extractMCPGatewayTokens, MCP_GATEWAY_CONFIG_PATHS } = require("./redact_secrets.cjs");
const { getOTLPSecretValues } = require("./otlp_header_helpers.cjs");
const { redactArtifactMaskedValues } = require("./add_mask_redaction.cjs");

function getSecretValues() {
  const secretNames = (process.env.GH_AW_SECRET_NAMES || "")
    .split(",")
    .map(name => name.trim())
    .filter(Boolean);

  /** @type {string[]} */
  const secretValues = [];
  for (const secretName of secretNames) {
    const value = process.env[`SECRET_${secretName}`];
    if (typeof value === "string" && value.trim() !== "") {
      secretValues.push(value.trim());
    }
  }

  secretValues.push(...extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS));
  return secretValues;
}

/**
 * Verifies that the published evals files (answer records or the not-evaluated
 * marker) contain no remaining secret values.
 */
function verifyRedaction() {
  const secretValues = [];
  let secretValuesLoaded = false;
  for (const filePath of [EVALS_OUTPUT_PATH, EVALS_STATUS_PATH]) {
    if (!fs.existsSync(filePath)) {
      continue;
    }

    let content;
    try {
      content = fs.readFileSync(filePath, "utf8");
    } catch (err) {
      throw new Error(`Failed to read file ${filePath}: ${String(err)}`, { cause: err });
    }
    if (!secretValuesLoaded) {
      secretValues.push(...getSecretValues());
      secretValuesLoaded = true;
    }
    const hasLingeringSecrets = redactBuiltInPatterns(content).redactionCount > 0 || redactSecrets(content, secretValues).redactionCount > 0 || redactArtifactMaskedValues(content, getOTLPSecretValues()) !== content;

    if (hasLingeringSecrets) {
      core.setFailed(`Secret redaction verification failed for ${filePath}: unredacted values remain`);
      return;
    }
  }
}

async function main() {
  await redactWorkspaceSecrets();
  verifyRedaction();
}

module.exports = { main, getSecretValues, verifyRedaction };
