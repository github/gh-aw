// @ts-check

const fs = require("fs");
const path = require("path");
const { collectArtifactSecretValues, redactManifestValue } = require("./safe_output_manifest.cjs");
const { collectAddMaskedValues, redactMaskedValues } = require("./add_mask_redaction.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");

/** @param {string[]} files @param {unknown} originalError */
function removeFailedSessionArtifacts(files, originalError) {
  try {
    for (const file of files) if (fs.existsSync(file)) fs.unlinkSync(file);
  } catch (cleanupError) {
    throw new AggregateError([originalError, cleanupError], `${ERR_SYSTEM}: Failed to remove incomplete session artifact: ${getErrorMessage(cleanupError)}`);
  }
}

/**
 * Redact decoded string values before JSON serialization, including correlation IDs.
 * @param {import("./types/agent_session").AgentSession} events
 * @param {string[]} [maskedValues]
 * @param {{rejectStructuralRedaction?: boolean}} [options]
 * @returns {string}
 */
function serializeSessionArtifact(events, maskedValues = [], options = {}) {
  const redacted = redactManifestValue(events, collectArtifactSecretValues());
  if (!Array.isArray(redacted)) throw new Error(`${ERR_VALIDATION}: Expected a session event array`);
  const serialized = redacted.map(event =>
    JSON.stringify(event, (_key, value) => {
      if (typeof value === "string") return redactMaskedValues(value, maskedValues);
      if (options.rejectStructuralRedaction && value && typeof value === "object" && !Array.isArray(value)) {
        return Object.fromEntries(Object.entries(value).map(([key, nested]) => [redactMaskedValues(key, maskedValues), nested]));
      }
      return value;
    })
  );
  if (options.rejectStructuralRedaction) {
    try {
      events.forEach((event, index) => {
        const serializedEvent = JSON.parse(serialized[index]);
        // Compare the JSON shape so ordinary omission of undefined optional fields is ignored.
        // eslint-disable-next-line gh-aw-custom/prefer-structured-clone -- Structural validation must match JSON serialization semantics.
        const originalSerializedEvent = JSON.parse(JSON.stringify(event));
        assertSessionStructurePreserved(originalSerializedEvent, serializedEvent, maskedValues, event.type);
      });
    } catch (error) {
      throw new Error(`${ERR_VALIDATION}: Session redaction changed structural fields or serialization`, { cause: error });
    }
  }
  return serialized.join("\n") + (events.length ? "\n" : "");
}

const sessionPayloadFields = new Map([
  ["user.message", ["content"]],
  ["assistant.message", ["content"]],
  ["assistant.reasoning", ["content"]],
  ["assistant.refusal", ["content", "explanation"]],
  ["tool.execution_start", ["input", "parameters", "command"]],
  ["tool.execution_complete", ["output", "result", "error", "fileEdits"]],
]);

/** @param {unknown} original @param {unknown} redacted @param {string[]} maskedValues @param {string} eventType @param {string[]} [fields] */
function assertSessionStructurePreserved(original, redacted, maskedValues, eventType, fields = []) {
  // Only schema-defined payloads may change; matching a nested key is not enough.
  if (fields.length === 2 && fields[0] === "data" && sessionPayloadFields.get(eventType)?.includes(fields[1])) return;
  if (Array.isArray(original)) {
    if (!Array.isArray(redacted) || original.length !== redacted.length) throw new Error(`${ERR_VALIDATION}: Session redaction changed event structure`);
    original.forEach((value, index) => assertSessionStructurePreserved(value, redacted[index], maskedValues, eventType, [...fields, String(index)]));
  } else if (original && typeof original === "object") {
    const keys = Object.keys(original);
    if (!redacted || typeof redacted !== "object" || keys.length !== Object.keys(redacted).length || keys.some(key => !Object.hasOwn(redacted, key))) {
      throw new Error(`${ERR_VALIDATION}: Session redaction changed structural field names`);
    }
    const redactedEntries = new Map(Object.entries(redacted));
    for (const [key, value] of Object.entries(original)) {
      if (redactMaskedValues(key, maskedValues) !== key) throw new Error(`${ERR_VALIDATION}: Session mask collides with a structural field name`);
      assertSessionStructurePreserved(value, redactedEntries.get(key), maskedValues, eventType, [...fields, key]);
    }
  } else if (original !== redacted) {
    throw new Error(`${ERR_VALIDATION}: Session redaction changed a structural field`);
  }
}

/**
 * A failed write must not publish a truncated session or retain an old output.
 * @param {string} outputPath
 * @param {import("./types/agent_session").AgentSession} events
 * @param {string[]} [maskedValues]
 * @param {{rejectStructuralRedaction?: boolean}} [options]
 */
function writeSessionArtifact(outputPath, events, maskedValues = [], options = {}) {
  const temporaryPath = `${outputPath}.tmp`;
  try {
    const content = serializeSessionArtifact(events, maskedValues, options);
    fs.mkdirSync(path.dirname(outputPath), { recursive: true });
    fs.writeFileSync(temporaryPath, content, { encoding: "utf8", mode: 0o600, flag: "wx" });
    fs.renameSync(temporaryPath, outputPath);
  } catch (error) {
    removeFailedSessionArtifacts([temporaryPath, outputPath], error);
    throw new Error(`${ERR_SYSTEM}: Failed to write session artifact ${outputPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

module.exports = { serializeSessionArtifact, writeSessionArtifact, removeFailedSessionArtifacts, collectAddMaskedValues };
