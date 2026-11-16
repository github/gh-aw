#!/usr/bin/env node
"use strict";

const { spawnSync } = require("node:child_process");
const path = require("node:path");

const GRAPH_ARGUMENT = "--dir=/tmp/gh-aw/graft";
const COMMANDS = new Set(["map", "ask", "callers", "skeleton", "grep"]);

function queryArguments(args) {
  if (args.length === 1 && args[0] === "--help") return args;
  const [graph, command, ...parameters] = args;
  if (graph !== GRAPH_ARGUMENT || !COMMANDS.has(command)) {
    throw new Error(`Use graft ${GRAPH_ARGUMENT} with map, ask, callers, skeleton, or grep.`);
  }
  if (parameters.some(argument => argument === "--dir" || argument.startsWith("--dir="))) {
    throw new Error("Graft queries cannot override the pre-built graph directory.");
  }
  return [GRAPH_ARGUMENT, command, ...parameters];
}

if (require.main === module) {
  try {
    const args = queryArguments(process.argv.slice(2));
    const cli = path.join(__dirname, "../node_modules/@nanonets/graft/dist/cli.js");
    const result = spawnSync(process.execPath, [cli, ...args], { stdio: "inherit" });
    if (result.error) throw result.error;
    if (result.signal) throw new Error(`Graft query terminated by ${result.signal}.`);
    if (result.status === null) throw new Error("Graft query exited without a status.");
    process.exitCode = result.status;
  } catch (error) {
    console.error(`Graft query failed: ${error.message}`);
    process.exitCode = 1;
  }
}

module.exports = { queryArguments };
