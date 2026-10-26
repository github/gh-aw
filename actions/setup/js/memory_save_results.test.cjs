import { describe, expect, it } from "vitest";
import { createRequire } from "module";

const { readMemorySaveResults } = createRequire(import.meta.url)("./memory_save_results.cjs");

describe("memory save results", () => {
  it.each([
    ["success", "success", "saved", "saved"],
    ["success", "success", "unchanged", "unchanged"],
    ["success", "success", "skipped", "skipped"],
    ["failure", "failure", "saved", "failure"],
    ["cancelled", "cancelled", "saved", "cancelled"],
    ["skipped", "", "", "skipped"],
    ["cancelled", "", "", "cancelled"],
    ["failure", "", "", "unknown"],
    ["success", "", "saved", "unknown"],
    ["success", "success", "", "unknown"],
    ["success", "skipped", "", "skipped"],
    // A sibling memory may fail even though this memory was saved.
    ["failure", "success", "saved", "saved"],
  ])("projects job=%s outcome=%s status=%s as %s", (jobResult, outcome, status, expected) => {
    expect(readMemorySaveResults(JSON.stringify({ default: { jobResult, outcome, status, error: "PRIVATE_ERROR", content: "PRIVATE_CONTENT" } }))).toEqual([
      { memoryId: "default", jobResult, ...(outcome ? { outcome } : {}), status: expected },
    ]);
  });

  it.each(["", "invalid", "null", "[]", "42", '{"default":null}', '{"default":{"jobResult":"PRIVATE"}}', '{"PRIVATE ID":{"jobResult":"success"}}'])("ignores malformed or absent evidence: %s", value => {
    expect(readMemorySaveResults(value)).toEqual([]);
  });

  it("retains valid memories in deterministic order without private output fields", () => {
    const result = { jobResult: "success", outcome: "success", status: "saved", secret: "PRIVATE" };
    expect(readMemorySaveResults(JSON.stringify({ z: result, invalid: [], a: result })).map(result => result.memoryId)).toEqual(["a", "z"]);
    expect(JSON.stringify(readMemorySaveResults(JSON.stringify({ a: result })))).not.toContain("PRIVATE");
  });
});
