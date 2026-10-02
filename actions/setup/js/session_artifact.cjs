// @ts-check

const fs = require("fs");
const path = require("path");
const { collectArtifactSecretValues, redactManifestValue } = require("./safe_output_manifest.cjs");
const { collectAddMaskedValues, redactMaskedValues } = require("./add_mask_redaction.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @param {string[]} files @param {unknown} originalError */
function removeFailedSessionArtifacts(files, originalError) {
  try {
    for (const file of files) if (fs.existsSync(file)) fs.unlinkSync(file);
  } catch (cleanupError) {
    throw new AggregateError([originalError, cleanupError], `Failed to remove incomplete session artifact: ${getErrorMessage(cleanupError)}`);
  }
}

/**
 * Redact decoded string values before JSON serialization, including correlation IDs.
 * @param {import("./types/agent_session").AgentSession} events
 * @param {string[]} [maskedValues]
 * @returns {string}
 */
function serializeSessionArtifact(events, maskedValues = []) {
  const redacted = redactManifestValue(events, collectArtifactSecretValues());
  if (!Array.isArray(redacted)) throw new Error("Expected a session event array");
  return redacted.map(event => JSON.stringify(event, (_key, value) => (typeof value === "string" ? redactMaskedValues(value, maskedValues) : value))).join("\n") + (events.length ? "\n" : "");
}

/**
 * A failed write must not publish a truncated session or retain an old output.
 * @param {string} outputPath
 * @param {import("./types/agent_session").AgentSession} events
 * @param {string[]} [maskedValues]
 */
function writeSessionArtifact(outputPath, events, maskedValues = []) {
  const temporaryPath = `${outputPath}.tmp`;
  try {
    const content = serializeSessionArtifact(events, maskedValues);
    fs.mkdirSync(path.dirname(outputPath), { recursive: true });
    fs.writeFileSync(temporaryPath, content, { encoding: "utf8", mode: 0o600, flag: "wx" });
    fs.renameSync(temporaryPath, outputPath);
  } catch (error) {
    removeFailedSessionArtifacts([temporaryPath, outputPath], error);
    throw new Error(`Failed to write session artifact ${outputPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

module.exports = { serializeSessionArtifact, writeSessionArtifact, removeFailedSessionArtifacts, collectAddMaskedValues };
