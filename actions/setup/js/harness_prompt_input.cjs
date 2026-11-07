// @ts-check

"use strict";

const fs = require("fs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Remove harness-only prompt-file pairs and read the last prompt as UTF-8.
 * Delivery (stdin or positional argv) remains the engine's responsibility.
 * @param {string[]} args
 * @param {{ logger: (message: string) => void, allowMissingPath?: boolean }} options
 * @returns {{ args: string[], prompt: string | null }}
 */
function resolveHarnessPromptFileArgs(args, { logger, allowMissingPath = false }) {
  /** @type {string[]} */
  const filteredArgs = [];
  /** @type {string | null} */
  let prompt = null;

  for (let i = 0; i < args.length; i++) {
    if (args[i] !== "--prompt-file") {
      filteredArgs.push(args[i]);
      continue;
    }
    if (i + 1 >= args.length) {
      if (!allowMissingPath) throw new Error("--prompt-file requires a readable file path");
      logger("warning: --prompt-file provided without a path; leaving arguments unchanged");
      filteredArgs.push(args[i]);
      continue;
    }

    const promptFile = args[++i];
    try {
      const stat = fs.statSync(promptFile);
      logger(`resolved --prompt-file: path=${promptFile} size=${stat.size}B`);
      prompt = fs.readFileSync(promptFile, "utf8");
    } catch (error) {
      throw new Error(`--prompt-file '${promptFile}' is not readable: ${getErrorMessage(error)}`, { cause: error });
    }
  }

  return { args: filteredArgs, prompt };
}

module.exports = { resolveHarnessPromptFileArgs };
