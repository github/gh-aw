// @ts-check

const fs = require("fs");
const path = require("path");
const { readGuardedFile } = require("./symlink_guard.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

function main() {
  try {
    let content;
    try {
      content = readGuardedFile(path.join(process.env.GH_AW_TMP_DIR || "/tmp/gh-aw", "agent-step-summary.md"));
    } catch (error) {
      if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") return;
      throw error;
    }
    if (content.length === 0) return;
    const destination = process.env.GITHUB_STEP_SUMMARY;
    if (!destination) throw new Error("GITHUB_STEP_SUMMARY is required");
    fs.appendFileSync(destination, content);
  } catch (error) {
    console.error(`::error::Agent step summary could not be published safely: ${getErrorMessage(error)}`);
    process.exitCode = 1;
  }
}

if (require.main === module) main();

module.exports = { main };
