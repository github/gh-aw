// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { main, transactionPath } from "./ledger_append.cjs";
import { validateTransactions } from "./push_ledger_changes.cjs";
import { createReducer } from "./ledger_builtin.cjs";

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
    if (previousRunAttempt === undefined) delete process.env.GITHUB_RUN_ATTEMPT;
    else process.env.GITHUB_RUN_ATTEMPT = previousRunAttempt;
    fs.rmSync(runnerTemp, { recursive: true, force: true });
  }
});

test("normalizes the safe-output type discriminator before validating built-in appends", async () => {
  const runnerTemp = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-append-envelope-"));
  const previousRunnerTemp = process.env.RUNNER_TEMP;
  process.env.RUNNER_TEMP = runnerTemp;
  try {
    const handler = await main({ ledgers: [{ name: "events", type: "log" }] });
    await handler({ type: "ledger_append", ledger: "events", operation: "append", value: { ok: true } });
    handler.finalize();

    const artifact = JSON.parse(fs.readFileSync(transactionPath(), "utf8"));
    assert.equal(artifact.ledgers.events.appends.length, 1);
    const append = artifact.ledgers.events.appends[0].record;
    assert.match(append.id, /^ldg-/);
    assert.deepEqual({ operation: append.operation, value: append.value }, { operation: "append", value: { ok: true } });
  } finally {
    if (previousRunnerTemp === undefined) delete process.env.RUNNER_TEMP;
    else process.env.RUNNER_TEMP = previousRunnerTemp;
    fs.rmSync(runnerTemp, { recursive: true, force: true });
  }
});

test("rejects invalid append envelopes with input kinds but without payload contents", async () => {
  const handler = await main({ ledgers: [{ name: "events", type: "log" }] });
  for (const [message, receivedType] of [
    [null, "null"],
    [[], "array"],
    [undefined, "undefined"],
    ["private payload", "string"],
    [42, "number"],
    [true, "boolean"],
  ]) {
    await assert.rejects(() => handler(message), { name: "TypeError", message: `Invalid ledger append message: expected an object, received ${receivedType}` });
  }
  for (const [type, receivedType] of [
    ["private discriminator", "string"],
    [null, "null"],
    [[], "array"],
    [{ private: "payload" }, "object"],
    [42, "number"],
    [false, "boolean"],
  ]) {
    await assert.rejects(() => handler({ type, ledger: "events", operation: "append", value: "private payload" }), {
      name: "TypeError",
      message: `Invalid ledger append message type: expected "ledger_append", received ${receivedType}`,
    });
  }
});

test("Caveman table upsert rejects the reported key field and persists the corrected payload shape", async () => {
  const runnerTemp = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-caveman-"));
  const previousRunnerTemp = process.env.RUNNER_TEMP;
  process.env.RUNNER_TEMP = runnerTemp;
  const config = {
    name: "caveman-run-history",
    type: "table",
    key: "run_id",
    schema: {
      type: "object",
      required: ["record_type", "run_id", "date", "files_processed", "files_optimized", "planned_outcome"],
      properties: {
        record_type: { enum: ["caveman_run"] },
        run_id: { type: "string" },
        date: { type: "string" },
        files_processed: { type: "integer", minimum: 0 },
        files_optimized: { type: "integer", minimum: 0 },
        planned_outcome: { enum: ["pull_request_requested", "noop"] },
      },
      additionalProperties: false,
    },
    max_record_kb: 4,
    max_segment_kb: 100,
    max_patch_kb: 10,
  };
  const rejectedPayload = {
    key: "37372003041",
    ledger: "caveman-run-history",
    operation: "upsert",
    value: { date: "2026-10-05", files_optimized: 0, files_processed: 5, planned_outcome: "noop", record_type: "caveman_run", run_id: "37372003041" },
    type: "ledger_append",
  };
  try {
    const rejected = await main({ ledgers: [config] });
    await rejected(rejectedPayload);
    assert.throws(() => rejected.finalize(), { name: "TypeError", message: 'Invalid ledger operation fields: unexpected field "key" for upsert' });
    assert.equal(fs.existsSync(transactionPath()), false);

    const { key, ...correctedPayload } = rejectedPayload;
    const invalidRunId = await main({ ledgers: [config] });
    await invalidRunId({ ...correctedPayload, value: { ...correctedPayload.value, run_id: Number(key) } });
    assert.throws(() => invalidRunId.finalize(), /string primary key: "run_id"/);

    const corrected = await main({ ledgers: [config] });
    await corrected(correctedPayload);
    corrected.finalize();
    const artifact = JSON.parse(fs.readFileSync(transactionPath(), "utf8"));
    assert.doesNotThrow(() => validateTransactions(artifact, [config]));
    const record = artifact.ledgers["caveman-run-history"].appends[0].record;
    assert.equal(Object.hasOwn(record, "key"), false);
    assert.deepEqual(record.value, correctedPayload.value);
    const reducer = createReducer(config);
    reducer.apply(record);
    assert.deepEqual(reducer.output().tables.state.rows, [{ key, value: JSON.stringify(Object.fromEntries(Object.entries(record.value).sort())) }]);
  } finally {
    if (previousRunnerTemp === undefined) delete process.env.RUNNER_TEMP;
    else process.env.RUNNER_TEMP = previousRunnerTemp;
    fs.rmSync(runnerTemp, { recursive: true, force: true });
  }
});

test("built-in transaction diagnostics name unknown fields without their values", async () => {
  const handler = await main({ ledgers: [{ name: "events", type: "log" }] });
  await handler({ operation: "append", value: "private payload", extra: "private metadata" });
  assert.throws(() => handler.finalize(), { name: "TypeError", message: 'Invalid built-in transaction fields: unexpected field "extra"' });
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
