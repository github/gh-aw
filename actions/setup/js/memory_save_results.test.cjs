import { describe, expect, it } from "vitest";
import { createRequire } from "module";

const { readMemorySaveResults } = createRequire(import.meta.url)("./memory_save_results.cjs");

describe("memory save results", () => {
  it.each([
    ["success", "success", "saved", "saved"],
    ["success", "success", "unchanged", "unchanged"],
    ["success", "success", "skipped", "skipped"],
    ["failure", "failure", "saved", "saved"],
    ["cancelled", "cancelled", "saved", "saved"],
    ["failure", "", "saved", "saved"],
    ["cancelled", "", "unchanged", "unchanged"],
    ["failure", "failure", "unchanged", "unchanged"],
    ["failure", "failure", "skipped", "failure"],
    ["cancelled", "cancelled", "skipped", "cancelled"],
    ["skipped", "", "", "skipped"],
    ["cancelled", "", "", "cancelled"],
    ["failure", "", "", "unknown"],
    ["success", "", "saved", "saved"],
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

  it.each(["success", "failure", "cancelled"])("retains successful push metadata with step outcome %s", outcome => {
    const result = { jobResult: "failure", outcome, status: "saved", branch: "memory/default", commit: "b".repeat(40), content: "PRIVATE" };
    expect(readMemorySaveResults(JSON.stringify({ default: result }))).toEqual([{ memoryId: "default", jobResult: "failure", outcome, status: "saved", branch: result.branch, commit: result.commit }]);
  });

  it.each(["unchanged", "skipped", "failure", "cancelled", "unknown"])("omits push metadata for %s", status => {
    const result = { jobResult: "success", outcome: "success", status, branch: "memory/default", commit: "b".repeat(40) };
    const [projected] = readMemorySaveResults(JSON.stringify({ default: result }));
    expect(projected).not.toHaveProperty("branch");
    expect(projected).not.toHaveProperty("commit");
  });

  it.each([
    { branch: "memory/default", commit: "not-a-commit" },
    { branch: "memory/default", commit: "b".repeat(39) },
    { branch: "memory/default\nPRIVATE", commit: "b".repeat(40) },
    { branch: "", commit: "b".repeat(40) },
    { branch: {}, commit: "b".repeat(40) },
    { branch: "memory/default", commit: null },
  ])("omits invalid push metadata: %j", push => {
    const [projected] = readMemorySaveResults(JSON.stringify({ default: { jobResult: "success", outcome: "success", status: "saved", ...push } }));
    expect(projected).not.toHaveProperty("branch");
    expect(projected).not.toHaveProperty("commit");
  });
});
