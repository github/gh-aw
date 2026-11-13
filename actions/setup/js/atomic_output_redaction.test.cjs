// @ts-check
import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { createAtomicOutputSanitizers } = require("./atomic_output_redaction.cjs");

describe("Atomic output redaction", () => {
  it("preserves valid JSONL when a registered mask matches a JSON literal", () => {
    const sanitizers = createAtomicOutputSanitizers(() => ["true"]);
    const output = sanitizers.sanitizeStdout('{"done":true}\n{"done":false}\n');

    expect(output.trimEnd().split("\n").map(JSON.parse)).toEqual([{ done: true }, { done: false }]);
  });

  it("redacts escaped JSON secrets and multiline raw diagnostics", () => {
    const multilineSecret = "first-secret-line\nsecond-secret-line";
    const sanitizers = createAtomicOutputSanitizers(() => ["escaped-secret", multilineSecret]);
    const output = sanitizers.sanitizeStdout(`${JSON.stringify({ credential: "escaped-secret" })}\n[diagnostic] ${multilineSecret}\n`);

    const [jsonRecord, ...diagnosticLines] = output.split("\n");
    expect(JSON.parse(jsonRecord)).toEqual({ credential: "[REDACTED]" });
    expect(diagnosticLines.join("\n")).not.toContain("first-secret-line");
    expect(diagnosticLines.join("\n")).not.toContain("second-secret-line");
    expect(diagnosticLines.join("\n")).toContain("[REDACTED]");
  });
});
