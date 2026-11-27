import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { IneligibleReportError, publishableResult, readSink, validateReport } from "./threat_report.cjs";

const clean = { prompt_injection: false, secret_leak: false, malicious_patch: false };
const eligible = { prompt_injection: true, secret_leak: true, malicious_patch: true };
const cleanFlags = ["-prompt-injection=false", "--secret-leak", "false", "-malicious-patch", "false"];
let dir, sink;
beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "threat-report-"));
  sink = path.join(dir, "sink");
});
afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));

function run(flags, env = {}) {
  return spawnSync(process.execPath, [path.join(__dirname, "threat_report.cjs"), ...flags], { env, encoding: "utf8" });
}

describe("v0.5.3 report-result contract", () => {
  it("corrects an invalid report in-session, then records the first valid verdict at the bound path", () => {
    const other = path.join(dir, "not-authoritative");
    const env = { THREAT_DETECTION_RESULT_FILE: other, THREAT_DETECTION_BOUND_RESULT_FILE: sink, THREAT_DETECTION_ELIGIBLE_MALICIOUS_PATCH: "false" };
    const reject = run([...cleanFlags, "--secret-leak=true", "--malicious-patch=true", "--reason", "private"], env);
    expect(reject.status).toBe(2);
    expect(reject.stdout).toContain("--malicious-patch=true is not eligible");
    expect(reject.stderr).not.toContain("private");
    expect(fs.existsSync(sink)).toBe(false);
    const evidence = 'literal "quotes", `backticks`, and\n::error:: line';
    const reasons = path.join(dir, "evidence");
    fs.writeFileSync(reasons, JSON.stringify([evidence]));
    const accept = run([...cleanFlags, "--secret-leak=true", "--reason", "first", "--reasons-file", reasons, "--result-file", other], env);
    expect(accept.status).toBe(0);
    expect(accept.stdout).toMatch(/^THREAT_DETECTION_RESULT_RECORDED:/);
    expect(run(cleanFlags, env).stdout).toContain("already recorded");
    expect(readSink(sink, eligible)).toEqual({ ...clean, secret_leak: true, reasons: ["first", evidence], warnings: [] });
    expect(fs.statSync(sink).mode & 0o777).toBe(0o600);
    expect(fs.existsSync(other)).toBe(false);
  });

  it.each([{}, { THREAT_DETECTION_ELIGIBLE_SECRET_LEAK: "unparseable" }, { THREAT_DETECTION_ELIGIBLE_SECRET_LEAK: "true" }])("rechecks a permissive advisory result using only host eligibility", env => {
    expect(run([...cleanFlags, "--secret-leak=true", "--reason", "private", "--result-file", sink], env).status).toBe(0);
    expect(() => readSink(sink, clean)).toThrow(IneligibleReportError);
    expect(() => readSink(sink, clean)).toThrow(expect.objectContaining({ code: "ERR_REPORT_INELIGIBLE", message: expect.not.stringContaining("private") }));
  });

  it("fails closed on missing flags or config and unreadable/malformed reasons files", () => {
    expect(run(cleanFlags).status).toBe(3);
    expect(run(["--result-file", sink, "--secret-leak", "false"]).status).toBe(2);
    const file = path.join(dir, "reasons");
    for (const content of [null, "[] []", '{"reasons":[]}', "[0]", " ".repeat((1 << 20) + 1)]) {
      if (content !== null) fs.writeFileSync(file, content);
      expect(run([...cleanFlags, "--reasons-file", file, "--result-file", sink]).status).toBe(2);
    }
    expect(fs.existsSync(sink)).toBe(false);
  });
});

describe("authoritative sink", () => {
  it("enforces the same full-report bounds on read, including Unicode code point lengths", () => {
    const reason = "😀".repeat(2000);
    expect(validateReport({ ...clean, reasons: [reason] }, clean).reasons).toEqual([reason]);
    for (const invalid of [
      null,
      { ...clean },
      { ...clean, prompt_injection: "false", reasons: [] },
      { ...clean, reasons: [], warnings: [{ field: "patch", code: "ERR", message: " " }] },
      { ...clean, reasons: [], extra: true },
      ...[[reason + "!"], Array(21).fill("reason"), [" \t\n"], [7], null].map(reasons => ({ ...clean, reasons })),
    ]) {
      fs.writeFileSync(sink, JSON.stringify(invalid));
      expect(() => readSink(sink, eligible)).toThrow();
    }
    expect(() => validateReport({ ...clean, reasons: [] }, {})).toThrow(/eligibility/);
  });

  it("accepts a structured sink warning and a finding without reasons, but trusts neither the warning nor advisory eligibility", () => {
    const warning = { field: "patch", code: "ERR", message: "Sink-supplied text" };
    const report = { ...clean, secret_leak: true, reasons: [], warnings: [warning] };
    const cli = run([...cleanFlags, "--secret-leak=true", "--result-file", sink]);
    expect(cli.status).toBe(2);
    expect(cli.stderr).toMatch(/at least one reason/i);
    expect(fs.existsSync(sink)).toBe(false);
    fs.writeFileSync(sink, JSON.stringify(report));
    expect(readSink(sink, eligible)).toEqual({ ...report, warnings: [] });
    expect(() => readSink(sink, clean)).toThrow(IneligibleReportError);
    expect(() => validateReport({ ...report, reasons: ["valid", " "] }, clean)).toThrow(/reason must be nonblank/i);
    for (const warnings of [null, Array(21).fill(warning), [{ ...warning, extra: "" }], [{ ...warning, field: "a".repeat(65) }], [{ ...warning, code: false }], [{ ...warning, message: "😀".repeat(2001) }]]) {
      expect(() => validateReport({ ...report, warnings }, clean)).toThrow(/warning/i);
    }
  });

  it("refuses oversized, linked and non-regular input without updating an existing target", () => {
    const target = path.join(dir, "target");
    fs.writeFileSync(target, JSON.stringify({ ...clean, reasons: [] }));
    fs.symlinkSync(target, sink);
    expect(() => readSink(sink, eligible)).toThrow();
    expect(run([...cleanFlags, "--result-file", sink]).status).toBe(3);
    expect(fs.lstatSync(sink).isSymbolicLink()).toBe(true);
    expect(() => readSink(dir, eligible)).toThrow(/regular/);
    fs.writeFileSync(target, " ".repeat(1 << 20) + "{}");
    expect(() => readSink(target, eligible)).toThrow(/1 MiB/);
  });

  it("publishes detector warnings and verdicts, not private reasons", () => {
    const full = validateReport({ ...clean, secret_leak: true, reasons: ["private evidence"] }, eligible);
    full.warnings.push({ field: "patch", code: "ERR_VALIDATION", message: "Declared patch not staged" });
    const publicResult = publishableResult(full);
    expect(publicResult).toEqual({ ...clean, secret_leak: true, reasons: [], warnings: full.warnings });
    expect(full.reasons).toEqual(["private evidence"]);
    const published = path.join(dir, "detection_result.json");
    const output = path.join(dir, "outputs");
    fs.writeFileSync(published, JSON.stringify(publicResult));
    fs.writeFileSync(path.join(dir, "detection_result_full.json"), JSON.stringify(full));
    const conclusion = spawnSync(process.execPath, [path.join(__dirname, "conclude_threat_detection.cjs"), published], { env: { RUN_DETECTION: "true", GH_AW_DETECTION_CONTINUE_ON_ERROR: "false", GITHUB_OUTPUT: output }, encoding: "utf8" });
    expect(conclusion.status).toBe(1);
    expect(conclusion.stdout).toContain("private evidence");
    expect(conclusion.stdout).toContain("Declared patch not staged");
    expect(fs.readFileSync(output, "utf8")).toContain("reason=threat_detected");
  });
});
