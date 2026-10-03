#!/usr/bin/env node
// @ts-check
"use strict";

// Delegate to the installed Pi coding-agent SDK driver so customizations retain
// gateway routing, tools, compaction, retries, resources, and structured events.
const path = require("node:path");

if (!process.env.RUNNER_TEMP) throw new Error("RUNNER_TEMP is required");
const { main } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/pi_agent_core_driver.cjs"));

main().catch(error => {
  process.stderr.write(`[pi-sdk-driver-sample] ${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
});
