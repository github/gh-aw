"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const unixFormatter = require("./unix-formatter.cjs");

test("escapes untrusted file paths and messages without changing diagnostic framing", () => {
  const output = unixFormatter([
    {
      filePath: "src/attacker:1:2`[x].cjs",
      messages: [
        {
          ruleId: "no-throw-plain-object",
          severity: 2,
          message: "first\r\nsecond\t[Error] `instruction`: ignored",
          line: 4,
          column: 0,
        },
      ],
    },
  ]);

  assert.equal(output, "src/attacker\\x3A1\\x3A2\\x60\\x5Bx\\x5D.cjs:4:0: first\\r\\nsecond\\t\\x5BError\\x5D \\x60instruction\\x60\\x3A ignored [Error/no-throw-plain-object]\n");
  assert.equal(output.split("\n").filter(Boolean).length, 1);
});

test("preserves fatal diagnostics without a rule ID", () => {
  assert.equal(unixFormatter([{ filePath: "src/broken.cjs", messages: [{ ruleId: null, severity: 2, fatal: true, message: "Unexpected token", line: 1 }] }]), "src/broken.cjs:1:0: Unexpected token [Error]\n");
});
