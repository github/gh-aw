// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { closed, parseStrictJSON } = require("./work_queue_codec.cjs");

// This runs only in the compiler's read-only preparation job. It cannot publish
// effects or delivery receipts; the trusted job independently ingests its data.
async function main() {
  const inputFile = process.env.GH_AW_CLAIM_INPUT;
  const outputFile = process.env.GH_AW_CLAIM_OUTPUT;
  const runtimeRoot = process.env.RUNNER_TEMP;
  if (!inputFile || !outputFile || !runtimeRoot) throw new Error("Claim script requires the compiler-produced isolated paths");
  const input = parseStrictJSON(fs.readFileSync(inputFile, "utf8"));
  closed(input, ["version", "claim_handle", "type", "messages"], [], "Claim script input");
  const filename = process.env.GH_AW_CLAIM_SCRIPT_FILENAME;
  if (!filename || path.basename(filename) !== filename || !/^safe_output_script_[A-Za-z_0-9]+\.cjs$/.test(filename)) throw new Error("Invalid compiler-produced Claim script filename");
  const module = require(path.join(runtimeRoot, "gh-aw", "actions", filename));
  const execute = await module.main();
  if (typeof execute !== "function") throw new Error("Claim preparation script must provide a payload preparer");
  const messages = [];
  for (const message of input.messages) {
    const payload = await execute(message, new Map(), new Map());
    if (!payload || typeof payload !== "object" || Array.isArray(payload)) throw new Error("Claim preparation script must return declared payload fields");
    messages.push({ input: message, payload });
  }
  fs.writeFileSync(outputFile, JSON.stringify({ version: 3, claim_handle: input.claim_handle, type: input.type, messages }) + "\n", { mode: 0o600 });
}

module.exports = { main };
