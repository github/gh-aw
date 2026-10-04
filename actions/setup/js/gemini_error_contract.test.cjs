import { describe, it, expect, vi } from "vitest";
import fs from "node:fs";
import vm from "node:vm";
import { createRequire } from "node:module";
import { parseGeminiLog } from "./parse_gemini_log.cjs";
import { selectSessionResult, projectSessionResult } from "./agent_session.cjs";
import { generatePlainTextSummary, generateCopilotCliStyleSummary } from "./log_parser_shared.cjs";

const localRequire = createRequire(import.meta.url);
const bootstrapSource = fs.readFileSync(new URL("./log_parser_bootstrap.cjs", import.meta.url), "utf8");
const providerError = { type: "error", severity: "error", message: "Provider temporarily unavailable", timestamp: "attempt-1" };
const diagnostic = { severity: "error", message: providerError.message };
const terminalSuccess = { type: "result", status: "success", errors: [], stats: { input_tokens: 3, output_tokens: 2, duration_ms: 0 } };
const cases = [
  {
    name: "provider error followed by source success",
    records: [providerError, terminalSuccess],
    errors: [diagnostic],
    diagnosticTimestamps: ["attempt-1"],
    terminal: true,
  },
  {
    name: "distinct retries followed by source success",
    records: [providerError, { ...providerError, timestamp: "attempt-2" }, terminalSuccess],
    errors: [diagnostic, diagnostic],
    diagnosticTimestamps: ["attempt-1", "attempt-2"],
    terminal: true,
  },
  {
    name: "warning followed by source success",
    records: [{ ...providerError, severity: "warning" }, terminalSuccess],
    errors: [],
    diagnosticTimestamps: [],
    terminal: true,
  },
  {
    name: "standalone error without a terminal source record",
    records: [providerError],
    errors: [diagnostic],
    diagnosticTimestamps: ["attempt-1"],
    terminal: false,
  },
];

const jsonl = records => records.map(record => JSON.stringify(record)).join("\n");

describe("Gemini diagnostic history is not terminal failure", () => {
  it.each(cases)("T-UAS-015/028/035: $name", ({ records, errors, diagnosticTimestamps, terminal }) => {
    const parsed = parseGeminiLog(jsonl(records));
    const selected = selectSessionResult(parsed.logEntries);
    const projected = projectSessionResult(parsed.logEntries);
    const diagnostics = parsed.logEntries.filter(event => event.type === "session.result" && event.data.errors?.length);

    expect(parsed.logEntries.filter(event => event.type === "gemini.error")).toHaveLength(records.length - Number(terminal));
    expect(diagnostics.map(event => event.timestamp)).toEqual(diagnosticTimestamps);
    expect(selected.errors).toEqual(errors);
    expect(projected.errors).toEqual(errors);
    for (const event of diagnostics) {
      for (const field of ["status", "success", "is_error", "isError", "numTurns", "durationMs", "totalCostUsd", "usage"]) {
        expect(event.data[field]).toBeUndefined();
      }
    }
    for (const field of ["status", "success", "is_error", "isError", "num_turns", "total_cost_usd"]) {
      expect(projected[field]).toBeUndefined();
    }
    if (terminal) {
      expect(parsed.logEntries.at(-1).data).toMatchObject({ status: "success", errors: [], stats: terminalSuccess.stats });
      expect(selected.status).toBe("success");
      expect(projected.usage).toEqual({ input_tokens: 3, output_tokens: 2 });
      expect(projected.duration_ms).toBe(0);
    } else {
      for (const field of ["status", "success", "is_error", "isError", "numTurns", "durationMs", "totalCostUsd", "usage"]) {
        expect(selected[field]).toBeUndefined();
      }
      expect(projected.usage).toBeUndefined();
      expect(projected.duration_ms).toBeUndefined();
    }

    const plain = generatePlainTextSummary(parsed.logEntries, { parserName: "Gemini" });
    const actions = generateCopilotCliStyleSummary(parsed.logEntries, { parserName: "Gemini" });
    expect(parsed.markdown.includes("**Errors:**")).toBe(errors.length > 0);
    for (const summary of [plain, actions]) expect(summary.includes("  Errors:")).toBe(errors.length > 0);
    for (const summary of [parsed.markdown, plain, actions]) {
      expect(summary.includes(providerError.message)).toBe(errors.length > 0);
      expect(summary).not.toMatch(/session (?:failed|succeeded)|(?:failed|succeeded) session|terminal (?:failure|success)/i);
      expect(summary).not.toMatch(/Turns:|Cost:|Failed Tools:|Tools:/);
      if (!terminal) expect(summary).not.toMatch(/Duration:|Tokens:|Token Usage:|Total:/);
    }
  });

  it.each(cases)("bootstrap does not fail from $name", async ({ records, errors, terminal }) => {
    const input = jsonl(records);
    const parsed = parseGeminiLog(input);
    const inputPath = "synthetic-gemini.jsonl";
    // Bootstrap artifact and telemetry writes are intercepted entirely in memory.
    const mockFs = {
      existsSync: target => target === inputPath,
      statSync: () => ({ isDirectory: () => false }),
      readFileSync: target => {
        expect(target).toBe(inputPath);
        return input;
      },
      mkdirSync: vi.fn(),
      appendFileSync: vi.fn(),
      writeFileSync: vi.fn(),
    };
    const writeSessionArtifact = vi.fn();
    const core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) },
    };
    const module = { exports: {} };
    vm.runInNewContext(
      bootstrapSource,
      {
        module,
        exports: module.exports,
        core,
        process: { env: { GH_AW_AGENT_OUTPUT: inputPath } },
        require: name => (name === "fs" ? mockFs : name === "./session_artifact.cjs" ? { writeSessionArtifact } : localRequire(name)),
      },
      { filename: "log_parser_bootstrap.cjs" }
    );
    await module.exports.runLogParser({ parserName: "Gemini", parseLog: parseGeminiLog });

    expect(core.setFailed).not.toHaveBeenCalled();
    expect(core.error).not.toHaveBeenCalled();
    expect(core.warning).not.toHaveBeenCalled();
    expect(core.summary.write).toHaveBeenCalledOnce();
    expect(core.summary.addRaw.mock.calls[0][0]).toContain(generateCopilotCliStyleSummary(parsed.logEntries, { parserName: "Gemini" }));
    expect(writeSessionArtifact).toHaveBeenCalledOnce();
    const execution = { type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [] } };
    expect(writeSessionArtifact.mock.calls[0][1]).toEqual([...parsed.logEntries, ...(errors.length ? [execution] : [])]);
    expect(mockFs.writeFileSync).not.toHaveBeenCalled();
    expect(mockFs.appendFileSync).toHaveBeenCalledTimes(Number(terminal));
    if (terminal) {
      expect(JSON.parse(mockFs.appendFileSync.mock.calls[0][1])).toEqual({ type: "result", usage: { input_tokens: 3, output_tokens: 2 } });
    }
  });
});
