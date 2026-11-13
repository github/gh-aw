// @ts-check
const { redactArtifactMaskedValues } = require("./add_mask_redaction.cjs");

const replacementForValue = () => "[REDACTED]";

/**
 * @param {string} content
 * @param {string[]} maskedValues
 * @returns {string}
 */
function redactNonJsonLines(content, maskedValues) {
  let redacted = "";
  let pendingText = "";
  const flushText = () => {
    if (!pendingText) return;
    redacted += redactArtifactMaskedValues(pendingText, maskedValues, undefined, replacementForValue, { textMode: true });
    pendingText = "";
  };

  for (let cursor = 0; cursor < content.length;) {
    const newline = content.indexOf("\n", cursor);
    const end = newline === -1 ? content.length : newline + 1;
    const rawLine = content.slice(cursor, end);
    const line = newline === -1 ? rawLine : rawLine.slice(0, -1);
    let isJson = false;
    try {
      JSON.parse(line);
      isJson = true;
    } catch {
      isJson = false;
    }
    if (isJson) {
      flushText();
      redacted += rawLine;
    } else {
      pendingText += rawLine;
    }
    cursor = end;
  }

  flushText();
  return redacted;
}

/**
 * @param {string | Buffer | null | undefined} output
 * @param {string[]} maskedValues
 * @returns {string}
 */
function sanitizeText(output, maskedValues) {
  return redactArtifactMaskedValues(output?.toString("utf8") || "", maskedValues, undefined, replacementForValue, { textMode: true });
}

/**
 * @param {string | Buffer | null | undefined} output
 * @param {string[]} maskedValues
 * @returns {string}
 */
function sanitizeStdout(output, maskedValues) {
  const text = output?.toString("utf8") || "";
  const lineRedacted = redactArtifactMaskedValues(text, maskedValues, undefined, replacementForValue);
  return redactNonJsonLines(lineRedacted, maskedValues);
}

/**
 * @param {() => string[]} getSensitiveValues
 */
function createAtomicOutputSanitizers(getSensitiveValues) {
  return {
    sanitizeText: output => sanitizeText(output, getSensitiveValues()),
    sanitizeStdout: output => sanitizeStdout(output, getSensitiveValues()),
  };
}

module.exports = { createAtomicOutputSanitizers };
