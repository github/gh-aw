"use strict";

const fs = require("fs");
const { runBuiltinGrader } = require("./trace_graders.cjs");

const input = JSON.parse(fs.readFileSync(0, "utf8"));
const grader = input.grader;
let result;
if (grader.source === "inline") {
  const { runCustomGraders } = require("./custom_graders.cjs");
  result = runCustomGraders(input.payload, [grader]).output.results.find(result => result.id === grader.id);
} else if (grader.source === "builtin" && !Object.hasOwn(grader, "script")) {
  result = runBuiltinGrader(grader.id, input.payload, grader);
} else {
  throw new Error("unsupported grader source");
}
process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
if (result.status === "error") process.exitCode = 1;
