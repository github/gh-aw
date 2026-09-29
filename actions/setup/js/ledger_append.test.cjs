// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { main, transactionPath } from "./ledger_append.cjs";

test("collects validated appends and writes the versioned artifact", async () => {
  const runnerTemp = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-append-"));
  const previousRunnerTemp = process.env.RUNNER_TEMP;
  const previousRunId = process.env.GITHUB_RUN_ID;
  process.env.RUNNER_TEMP = runnerTemp;
  process.env.GITHUB_RUN_ID = "42";
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
    handler.finalize();

    const artifact = JSON.parse(fs.readFileSync(transactionPath(), "utf8"));
    assert.equal(artifact.version, 1);
    assert.equal(artifact.transaction_id, "42:1");
    assert.equal(artifact.ledgers.findings.appends.length, 2);
    assert.equal(artifact.ledgers.findings.appends[1].record.related_to, artifact.ledgers.findings.appends[0].record.id);
  } finally {
    if (previousRunnerTemp === undefined) delete process.env.RUNNER_TEMP;
    else process.env.RUNNER_TEMP = previousRunnerTemp;
    if (previousRunId === undefined) delete process.env.GITHUB_RUN_ID;
    else process.env.GITHUB_RUN_ID = previousRunId;
    fs.rmSync(runnerTemp, { recursive: true, force: true });
  }
});

test("rejects invalid records at finalization", async () => {
  const handler = await main({ ledgers: [{ name: "findings", schema: { type: "object", required: ["subject"] } }] });
  await handler({ record: { other: true } });
  assert.throws(() => handler.finalize(), /does not match schema/);
});

test("does not follow a symlink when writing the transaction artifact", async () => {
  const runnerTemp = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-append-symlink-"));
  const previousRunnerTemp = process.env.RUNNER_TEMP;
  const target = path.join(runnerTemp, "target");
  process.env.RUNNER_TEMP = runnerTemp;
  try {
    fs.mkdirSync(path.dirname(transactionPath()), { recursive: true });
    fs.writeFileSync(target, "unchanged");
    fs.symlinkSync(target, transactionPath());
    const handler = await main({ ledgers: [{ name: "findings" }] });
    await handler({ record: { subject: "attempt" } });

    assert.throws(() => handler.finalize(), /ELOOP|symbolic link/i);
    assert.equal(fs.readFileSync(target, "utf8"), "unchanged");
  } finally {
    if (previousRunnerTemp === undefined) delete process.env.RUNNER_TEMP;
    else process.env.RUNNER_TEMP = previousRunnerTemp;
    fs.rmSync(runnerTemp, { recursive: true, force: true });
  }
});
