import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const checkerPath = path.join(import.meta.dirname, "../fixtures/genai/check_genai_conformance.mjs");
const reportDirs = [];
const requiredMetrics = [
  "gen_ai.client.inference.usage.input_tokens",
  "gen_ai.client.inference.usage.output_tokens",
  "gen_ai.client.inference.usage.cache_read.input_tokens",
  "gen_ai.client.inference.usage.cache_write.input_tokens",
  "gen_ai.invoke_agent.duration",
];

afterEach(() => {
  for (const dir of reportDirs.splice(0)) fs.rmSync(dir, { recursive: true, force: true });
});

function writeReports(nestedAdvice) {
  const reportDir = fs.mkdtempSync(path.join(os.tmpdir(), "genai-conformance-"));
  reportDirs.push(reportDir);
  const report = {
    samples: [
      {
        metrics: requiredMetrics.map(name => ({ name })),
        spans: nestedAdvice ? [{ live_check_result: { all_advice: [{ id: "missing_attribute", context: { attribute_key: "gen_ai.provider.name" } }] } }] : [],
      },
    ],
  };
  for (const scenario of ["agent_success_latest", "agent_failure_latest"]) {
    fs.writeFileSync(path.join(reportDir, `${scenario}.json`), JSON.stringify(report));
  }
  return reportDir;
}

describe("GenAI conformance advice validation", () => {
  it("rejects nested Weaver advice for missing GenAI attributes", () => {
    const reportDir = writeReports(true);
    const result = spawnSync(process.execPath, [checkerPath, reportDir], { encoding: "utf8" });
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("missing GenAI attribute gen_ai.provider.name");
  });

  it("accepts reports without GenAI findings", () => {
    const reportDir = writeReports(false);
    const result = spawnSync(process.execPath, [checkerPath, reportDir], { encoding: "utf8" });
    expect(result.status).toBe(0);
  });
});
