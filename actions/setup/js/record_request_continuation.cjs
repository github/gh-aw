// @ts-check
/// <reference types="@actions/github-script" />

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { main: createHandler } = require("./request_continuation_handler.cjs");
const fs = require("fs");

async function main() {
  const max = Number(process.env.GH_AW_REQUEST_CONTINUATION_MAX || "1");
  if (!Number.isSafeInteger(max) || max < 1) {
    throw new Error("GH_AW_REQUEST_CONTINUATION_MAX must be a positive integer");
  }
  const output = loadAgentOutput();
  if (!output.success) return;
  const handler = await createHandler({ max });
  const outputPath = process.env.GH_AW_AGENT_OUTPUT;
  if (!outputPath) return;
  const sourceOutput = JSON.parse(fs.readFileSync(outputPath, "utf8"));
  let changed = false;
  for (const [index, item] of output.items.entries()) {
    if (item.type === "request_continuation") {
      const sourceItem = sourceOutput.items[index];
      if (sourceItem && typeof sourceItem === "object" && Object.hasOwn(sourceItem, "timestamp")) {
        delete sourceItem.timestamp;
        changed = true;
      }
      const result = await handler(item, {});
      if (!result.success) {
        core.warning(`request_continuation: ${result.error}`);
      } else if (sourceItem && typeof sourceItem === "object") {
        sourceItem.timestamp = result.timestamp;
        changed = true;
      }
    }
  }
  if (changed) fs.writeFileSync(outputPath, JSON.stringify(sourceOutput));
}

module.exports = { main };
