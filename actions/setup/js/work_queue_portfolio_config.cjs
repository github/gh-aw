// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { parseSettings } = require("./work_queue_settings.cjs");

function readPortfolioSettings(directory = process.cwd()) {
  const filename = path.join(directory, ".github/workflows/aw.json");
  let content;
  try {
    content = fs.readFileSync(filename, "utf8");
  } catch (error) {
    if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") return undefined;
    throw error;
  }
  const config = JSON.parse(content);
  if (!config || typeof config !== "object" || Array.isArray(config)) throw new Error(`${filename}: expected a repository configuration object`);
  parseSettings(config.work_queue);
  return config.work_queue;
}

module.exports = { readPortfolioSettings };
