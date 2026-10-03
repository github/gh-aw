// @ts-check
"use strict";

const os = require("os");
const path = require("path");

// RUNNER_TEMP itself is not writable inside the agent sandbox.
const AWF_REFLECT_OUTPUT_PATH = "/tmp/gh-aw/agent/awf-reflect.json";
const AWF_REFLECT_LEGACY_PATH = path.join(process.env.RUNNER_TEMP || os.tmpdir(), "awf-reflect.json");

module.exports = { AWF_REFLECT_OUTPUT_PATH, AWF_REFLECT_LEGACY_PATH };
