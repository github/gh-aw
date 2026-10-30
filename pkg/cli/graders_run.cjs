"use strict";

const fs = require("fs");
const { runBuiltinGrader } = require("./trace_graders.cjs");

const input = JSON.parse(fs.readFileSync(0, "utf8"));
const grader = input.grader;
let result;
if (grader.source === "inline" || grader.source === "operational-value") {
  const { runCustomGraders } = require("./custom_graders.cjs");
  result = runCustomGraders(input.payload, [grader]).output.results.find(result => result.id === grader.id);
} else if (grader.source === "builtin" && !Object.hasOwn(grader, "script")) {
  result = runBuiltinGrader(grader.id, input.payload, grader);
} else {
  throw new Error("unsupported grader source");
}
const output = {
  version: 1,
  definition: {
    id: grader.id,
    name: grader.name,
    description: grader.description ?? "",
    source: grader.source,
    unit: grader.unit,
    direction: grader.direction,
    threshold: grader.threshold,
    config: grader.config ?? {},
  },
  result,
};
process.stdout.write(`${JSON.stringify(output, null, 2)}\n`);
if (result.status === "error") process.exitCode = 1;
