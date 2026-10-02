const fs = require("node:fs");

function readJSONL(filePath) {
  return fs
    .readFileSync(filePath, "utf8")
    .split("\n")
    .filter(line => line.trim())
    .map(line => JSON.parse(line));
}

function verifyCoordinatorFinishIntent({ safeOutputsPath, finishIntentPath }) {
  if (readJSONL(safeOutputsPath).some(record => record?.type === "create_issue")) {
    console.info("Dispatch coordinator smoke failure reported; processing the failure issue");
    return;
  }

  if (!readJSONL(finishIntentPath).some(record => record?.outcome === "completed")) {
    throw new Error("Dispatch coordinator finish intent artifact is missing or incomplete");
  }
}

module.exports = { verifyCoordinatorFinishIntent };
