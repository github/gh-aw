const fs = require("fs");
const path = require("path");

function listFiles(directory) {
  let entries;
  try {
    entries = fs.readdirSync(directory, { withFileTypes: true });
  } catch (error) {
    throw new Error(`Failed to read usage artifact directory ${directory}: ${String(error)}`, { cause: error });
  }
  return entries
    .flatMap(entry => {
      const entryPath = path.join(directory, entry.name);
      return entry.isDirectory() ? listFiles(entryPath) : [entryPath];
    })
    .sort();
}

async function generateBestEffort(label, generate, coreModule) {
  try {
    await generate();
  } catch (error) {
    coreModule.warning(`Unable to generate ${label}: ${error instanceof Error ? error.message : String(error)}`);
  }
}

async function main({ core: coreModule = global.core, generateSummary = require("./generate_usage_activity_summary.cjs").main, generateSession = require("./unified_session.cjs").main, usageDirectory = "/tmp/gh-aw/usage" } = {}) {
  await generateBestEffort("usage activity summary", generateSummary, coreModule);
  await generateBestEffort("unified session", generateSession, coreModule);

  for (const file of listFiles(usageDirectory)) {
    coreModule.info(file);
  }
}

module.exports = { main };
