// @ts-check

const { getErrorMessage } = require("./error_helpers.cjs");

const fs = require("fs");
const { ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { normalizeRuntimeMessage } = require("./work_queue_claim_scope.cjs");

/**
 * Create an append function for the safe outputs file
 * @param {string} outputFile - Path to the output file
 * @returns {Function} A function that appends entries to the safe outputs file
 */
function createAppendFunction(outputFile) {
  /**
   * Append an entry to the safe outputs file
   *
   * CRITICAL: The output file is in JSONL (JSON Lines) format where each entry
   * MUST be a single line. JSON.stringify must be called WITHOUT formatting
   * parameters (no indentation, no pretty-printing) to ensure one JSON object per line.
   *
   * @param {Object} entry - The entry to append
   */
  return function appendSafeOutput(entry) {
    if (!outputFile) throw new Error(`${ERR_VALIDATION}: No output file configured`);
    // Normalize type to use underscores (convert any dashes to underscores)
    entry.type = entry.type.replace(/-/g, "_");
    entry = normalizeRuntimeMessage(entry);
    // CRITICAL: Use JSON.stringify WITHOUT formatting parameters for JSONL format
    // Each entry must be on a single line, followed by a newline character
    const jsonLine = JSON.stringify(entry) + "\n";
    try {
      if (fs.constants.O_NOFOLLOW === undefined) {
        throw new Error(`${ERR_SYSTEM}: O_NOFOLLOW is not available on this platform; cannot append safe outputs safely`);
      }
      const flags = fs.constants.O_WRONLY | fs.constants.O_APPEND | fs.constants.O_CREAT | fs.constants.O_NOFOLLOW;
      const fd = fs.openSync(outputFile, flags, 0o600);
      try {
        fs.fchmodSync(fd, 0o600);
        fs.writeFileSync(fd, jsonLine, "utf8");
      } finally {
        fs.closeSync(fd);
      }
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to write to output file: ${getErrorMessage(error)}`, { cause: error });
    }
  };
}

module.exports = { createAppendFunction };
