// @ts-check

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");

const { generateCompactSchema } = require("./generate_compact_schema.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Writes large content to a file and returns metadata
 * @param {string} content - The content to write
 * @returns {{ filename: string, description: string }} Object with filename and description
 */
function writeLargeContentToFile(content) {
  const logsDir = require("./runtime_paths.cjs").runtimePath("safeoutputs");

  try {
    fs.mkdirSync(logsDir, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create directory ${logsDir}: ${getErrorMessage(err)}`, { cause: err });
  }

  // Generate SHA256 hash of content
  const hash = crypto.createHash("sha256").update(content).digest("hex");

  // MCP tools return JSON, so always use .json extension
  const filename = `${hash}.json`;
  const filepath = path.join(logsDir, filename);

  try {
    fs.writeFileSync(filepath, content, "utf8");
  } catch (err) {
    throw new Error(`Failed to write file ${filepath}: ${getErrorMessage(err)}`, { cause: err });
  }

  const description = generateCompactSchema(content);

  return { filename, description };
}

module.exports = {
  writeLargeContentToFile,
};
