const fs = require("fs");
const path = require("path");

function listFiles(directory) {
  return fs
    .readdirSync(directory, { withFileTypes: true })
    .flatMap(entry => {
      const entryPath = path.join(directory, entry.name);
      return entry.isDirectory() ? listFiles(entryPath) : [entryPath];
    })
    .sort();
}

async function main({ core: coreModule = global.core, generateSummary = require("./generate_usage_activity_summary.cjs").main, generateSession = require("./unified_session.cjs").main, usageDirectory = "/tmp/gh-aw/usage" } = {}) {
  for (const [label, generate] of [
    ["usage activity summary", generateSummary],
    ["unified session", generateSession],
  ]) {
    try {
      await generate();
    } catch (error) {
      coreModule.warning(`Unable to generate ${label}: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  for (const file of listFiles(usageDirectory)) {
    coreModule.info(file);
  }
}

module.exports = { main };
