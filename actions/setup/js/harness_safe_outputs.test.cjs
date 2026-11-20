// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { getSafeOutputsByteOffset, hasAttemptTerminalSafeOutput } = require("./harness_safe_outputs.cjs");
const { claudeSafeOutputsOffset } = require("./claude_runtime.cjs");
let directory;
afterEach(() => {
  vi.restoreAllMocks();
  if (directory) fs.rmSync(directory, { recursive: true, force: true });
  directory = undefined;
});

function outputFile() {
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "harness-safe-outputs-"));
  return path.join(directory, "safe-outputs.jsonl");
}

describe("attempt-scoped safe-output observation", () => {
  it("captures byte sizes rather than UTF-8 character counts and ignores stale output", () => {
    const file = outputFile();
    const stale = '{"type":"add_comment","body":"old résumé"}\n';
    fs.writeFileSync(file, stale);
    const offset = getSafeOutputsByteOffset(file);
    expect(offset).toBe(Buffer.byteLength(stale));
    expect(claudeSafeOutputsOffset(file)).toBe(offset);
    expect(hasAttemptTerminalSafeOutput(file, offset)).toBe(false);
    fs.appendFileSync(file, '{"type":"add_comment","body":"current"}\n');
    expect(hasAttemptTerminalSafeOutput(file, offset)).toBe(true);
  });

  it("returns zero for missing and unset files", () => {
    expect(getSafeOutputsByteOffset("")).toBe(0);
    expect(getSafeOutputsByteOffset(outputFile())).toBe(0);
    expect(claudeSafeOutputsOffset("")).toBe(0);
  });

  it("retains Claude's strict inspection errors while tolerating errors by default", () => {
    const error = Object.assign(new Error("denied"), { code: "EACCES" });
    vi.spyOn(fs, "statSync").mockImplementation(() => {
      throw error;
    });
    const offset = getSafeOutputsByteOffset("file");
    expect(offset).toBeNaN();
    const readFileSync = vi.fn(() => '{"type":"add_comment","body":"old"}\n');
    expect(hasAttemptTerminalSafeOutput("file", offset, { readFileSync })).toBe(false);
    expect(readFileSync).not.toHaveBeenCalled();
    expect(() => claudeSafeOutputsOffset("file")).toThrow("Failed to inspect Claude safe-outputs file: denied");
  });

  it.each([NaN, Infinity, -1, 0.5, Number.MAX_SAFE_INTEGER + 1])("rejects invalid offsets without reading whole-run output: %s", offset => {
    const readFileSync = vi.fn(() => '{"type":"add_comment","body":"old"}\n');
    expect(hasAttemptTerminalSafeOutput("file", offset, { readFileSync })).toBe(false);
    expect(readFileSync).not.toHaveBeenCalled();
  });

  it.each(["missing_data", "report_incomplete"])("preserves opt-in diagnostic terminal types: %s", type => {
    const file = outputFile();
    fs.writeFileSync(file, JSON.stringify({ type, message: "diagnostic" }) + "\n");
    expect(hasAttemptTerminalSafeOutput(file, 0)).toBe(false);
    expect(hasAttemptTerminalSafeOutput(file, 0, { includeMissingData: true, includeReportIncomplete: true })).toBe(true);
  });
});
