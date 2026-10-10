import assert from "node:assert/strict";
import { test } from "node:test";
import { readableTrace } from "./trace.mjs";

test("readable traces escape every cell delimiter and line break", () => {
  const trace = [
    {
      number: 1,
      action: "Transition | one\nsecond line",
      state: {
        original: ["item|one"],
        phase: ["scan"],
        proposed: ["candidate"],
        applied: ["candidate"],
        verified: ["readback"],
        scan: "clean",
        lintClean: ["clean"],
        quality: "good\\value",
      },
    },
  ];

  const output = readableTrace(trace, { name: "fixture", expected: "NeverWitness" });

  assert.match(output, /Transition \\| one<br>second line/);
  assert.match(output, /item\\\|one/);
  assert.match(output, /good\\\\value/);
  assert.equal(output.split("\n").filter(line => line.startsWith("| 1 |")).length, 1);
});
