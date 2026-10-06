import { describe, expect, it } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { redactDiagnosticText, redactAndBoundDiagnostics } = require("./diagnostic_sanitization.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");

describe("diagnostic sanitization", () => {
  it.each(["maskedValues", "secrets"])("redacts the whole %s value before a built-in pattern can consume its prefix", key => {
    const secret = `sk-proj-${"a".repeat(64)}${"b".repeat(16)}`;
    const options = { secrets: [], [key]: [secret] };
    expect(redactDiagnosticText(`Error: ${secret}`, options)).toBe("Error: ***");
    expect(redactAndBoundDiagnostics(`Error: ${secret}`, options)).not.toContain("b".repeat(16));
  });

  it("prefers longer registered secrets over overlapping prefixes", () => {
    expect(redactDiagnosticText("Error: prefix-suffix", { secrets: ["prefix"], maskedValues: ["prefix-suffix"] })).toBe("Error: ***");
  });

  it.each(["&amp;#38;#96;", "&\x00#96;", "&<!-- hidden -->#96;", "&\x1b[0m#96;"])("normalizes encoded delimiters before a later sanitization pass: %s", entity => {
    const input = entity.repeat(3);
    const result = redactAndBoundDiagnostics(input);
    expect(result).toContain("```");
    expect(sanitizeContent(result)).toBe(result);
  });

  it("redacts secrets exposed by entity normalization", () => {
    const result = redactAndBoundDiagnostics("Error: private&#45;value", { secrets: ["private-value"] });
    expect(result).not.toContain("private");
    expect(result).toContain("***");
  });

  it("bounds oversized diagnostics and preserves truncation notice", () => {
    const result = redactAndBoundDiagnostics("x".repeat(9000));
    expect(result).toContain("[Content truncated due to length]");
    expect(result.length).toBeLessThan(8100);
  });
});
