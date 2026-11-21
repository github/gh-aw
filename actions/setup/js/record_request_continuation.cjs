// @ts-check
/// <reference types="@actions/github-script" />

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { main: createHandler } = require("./request_continuation_handler.cjs");

async function main() {
  const max = Number(process.env.GH_AW_REQUEST_CONTINUATION_MAX || "1");
  if (!Number.isSafeInteger(max) || max < 1) {
    throw new Error("GH_AW_REQUEST_CONTINUATION_MAX must be a positive integer");
  }
  const output = loadAgentOutput();
  if (!output.success) return;
  const handler = await createHandler({ max });
  for (const item of output.items) {
    if (item.type === "request_continuation") {
      const result = await handler(item);
      if (!result.success) core.warning(`request_continuation: ${result.error}`);
    }
  }
}

module.exports = { main };
