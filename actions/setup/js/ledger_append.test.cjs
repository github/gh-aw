// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { compact, main, transactionPath } from "./ledger_append.cjs";

test("collects validated appends and writes the versioned artifact", async () => {
  const runnerTemp = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-append-"));
  const previousRunnerTemp = process.env.RUNNER_TEMP;
  const previousRunId = process.env.GITHUB_RUN_ID;
  const previousRunAttempt = process.env.GITHUB_RUN_ATTEMPT;
  process.env.RUNNER_TEMP = runnerTemp;
  process.env.GITHUB_RUN_ID = "42";
  process.env.GITHUB_RUN_ATTEMPT = "1";
  try {
    const handler = await main({
      ledgers: [
        {
          name: "findings",
          schema: { type: "object", required: ["subject"], properties: { subject: { type: "string" }, related_to: { type: "string" } }, additionalProperties: false },
          max_record_kb: 1,
          max_patch_kb: 1,
        },
      ],
    });
    await handler({ ledger: "findings", temp_id: "first", record: { subject: "initial" } });
    await handler({ ledger: "findings", record: { subject: "follow-up", related_to: "#first" } });
    const compactHandler = await compact({
      ledgers: [{ name: "findings", schema: { type: "object", required: ["subject"], properties: { subject: { type: "string" }, related_to: { type: "string" } }, additionalProperties: false }, max_record_kb: 1, max_patch_kb: 1 }],
    });
    await compactHandler({ ledger: "findings", operations: [{ op: "insert", record: { subject: "summary" } }] });
    handler.finalize();

    const artifact = JSON.parse(fs.readFileSync(transactionPath(), "utf8"));
    assert.equal(artifact.version, 1);
    assert.equal(artifact.transaction_id, "42:1");
    assert.equal(artifact.ledgers.findings.appends.length, 2);
    assert.equal(artifact.ledgers.findings.appends[1].record.related_to, artifact.ledgers.findings.appends[0].record.id);
    assert.equal(artifact.ledgers.findings.compactions[0].index, 2);
    assert.equal(artifact.ledgers.findings.compactions[0].record.subject, "summary");
  } finally {
    if (previousRunnerTemp === undefined) delete process.env.RUNNER_TEMP;
    else process.env.RUNNER_TEMP = previousRunnerTemp;
    if (previousRunId === undefined) delete process.env.GITHUB_RUN_ID;
    else process.env.GITHUB_RUN_ID = previousRunId;
    if (previousRunAttempt === undefined) delete process.env.GITHUB_RUN_ATTEMPT;
    else process.env.GITHUB_RUN_ATTEMPT = previousRunAttempt;
    fs.rmSync(runnerTemp, { recursive: true, force: true });
  }
});

test("rejects invalid records at finalization", async () => {
  const handler = await main({ ledgers: [{ name: "findings", schema: { type: "object", required: ["subject"] } }] });
  await handler({ record: { other: true } });
  assert.throws(() => handler.finalize(), /does not match schema/);
});

test("does not overwrite a file through a pre-existing artifact symlink", async () => {
  const runnerTemp = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-append-symlink-"));
  const previousRunnerTemp = process.env.RUNNER_TEMP;
  process.env.RUNNER_TEMP = runnerTemp;
  try {
    const artifactPath = transactionPath();
    const sentinelPath = path.join(path.dirname(artifactPath), "sentinel.json");
    fs.mkdirSync(path.dirname(artifactPath), { recursive: true });
    fs.writeFileSync(sentinelPath, "unchanged");
    fs.symlinkSync(sentinelPath, artifactPath);
    const handler = await main({ ledgers: [{ name: "findings" }] });
    await handler({ record: { subject: "blocked" } });
    assert.throws(() => handler.finalize());
    assert.equal(fs.readFileSync(sentinelPath, "utf8"), "unchanged");
    assert.equal(fs.readFileSync(sentinelPath, "utf8"), "unchanged");
  } finally {
    if (previousRunnerTemp === undefined) delete process.env.RUNNER_TEMP;
    else process.env.RUNNER_TEMP = previousRunnerTemp;
    fs.rmSync(runnerTemp, { recursive: true, force: true });
  }
});
