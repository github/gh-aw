// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");

const SYSTEM_OPEN = "<system>";
const SYSTEM_CLOSE = "</system>";

/**
 * Return the workflow task: the rendered prompt after gh-aw's leading <system> block.
 * Falls back to the whole prompt when the block is absent or the remainder is empty.
 * @param {string} prompt
 * @returns {{ text: string, trimmed: boolean }}
 */
function extractRoutingTask(prompt) {
  if (prompt.trimStart().startsWith(SYSTEM_OPEN)) {
    const close = prompt.indexOf(SYSTEM_CLOSE);
    if (close >= 0) {
      const task = prompt.slice(close + SYSTEM_CLOSE.length).trim();
      if (task) return { text: task, trimmed: true };
    }
  }
  return { text: prompt.trim(), trimmed: false };
}

function main() {
  const promptPath = process.env.GH_AW_ROUTING_PROMPT;
  const destination = process.env.GH_AW_ROUTING_CONVERSATION_FILE;
  if (!promptPath || !destination) throw new Error("GH_AW_ROUTING_PROMPT and GH_AW_ROUTING_CONVERSATION_FILE are required");
  const prompt = fs.readFileSync(promptPath, "utf8");
  if (!prompt.trim()) throw new Error("Rendered workflow prompt is empty; cannot route this task");
  const { text, trimmed } = extractRoutingTask(prompt);
  fs.mkdirSync(path.dirname(destination), { recursive: true, mode: 0o700 });
  fs.writeFileSync(destination, JSON.stringify([{ role: "user", parts: [{ text }] }]), { mode: 0o600 });
  console.log(`model-routing conversation: ${text.length} of ${prompt.length} chars (${trimmed ? "task only" : "full prompt fallback"})`);
}

if (require.main === module) main();
module.exports = { extractRoutingTask, main };
