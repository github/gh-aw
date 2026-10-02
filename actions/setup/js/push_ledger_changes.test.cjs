// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { execFileSync, execSync } from "node:child_process";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { finalId, normalizeLedgerAppends } from "./ledger_transactions.cjs";
import { execGitSync } from "./git_helpers.cjs";
import { main, persistLedgerAppends, readLedgerConfig, readTransactions, validateTransactions } from "./push_ledger_changes.cjs";

const ledgerConfigs = [
  {
    name: "findings",
    schema: { type: "object", required: ["subject"], properties: { subject: { type: "string" } }, additionalProperties: false },
    max_record_kb: 32,
    max_segment_kb: 100,
    max_patch_kb: 10,
  },
];

function transaction(record = { subject: "A finding" }) {
  const transactionId = "run-42:1";
  return {
    version: 1,
    transaction_id: transactionId,
    ledgers: {
      findings: {
        appends: [
          {
            ledger: "findings",
            transaction_id: transactionId,
            index: 0,
            record: { ...record, id: finalId(transactionId, 0) },
          },
        ],
      },
    },
  };
}

test("trusted ledger configuration rejects removed custom replay", () => {
  for (const replay of [null, {}, { script: "return { tables: {} }" }]) {
    const encoded = Buffer.from(JSON.stringify([{ ...ledgerConfigs[0], replay }])).toString("base64");
    assert.throws(() => readLedgerConfig(encoded), /Custom ledger replay is no longer supported/);
  }
});

test("reads and validates only versioned transaction artifacts", () => {
  assert.deepEqual(readTransactions(undefined), { version: 1, ledgers: {} });
  assert.throws(() => validateTransactions({ version: 2, ledgers: {} }, ledgerConfigs), /Invalid validated ledger transaction artifact/);
  assert.throws(() => validateTransactions({ version: 1, ledgers: [] }, ledgerConfigs), /Invalid validated ledger transaction artifact/);
  assert.throws(() => validateTransactions({ ...transaction(), forged: true }, ledgerConfigs), /Invalid validated ledger transaction artifact/);
});

test("revalidates record schemas, reserved envelope fields, and record sizes", () => {
  assert.throws(() => validateTransactions(transaction({ subject: 5 }), ledgerConfigs), /does not match configured schema/);
  assert.throws(() => validateTransactions(transaction({ subject: "valid", parents: [] }), ledgerConfigs), /reserved field/);
  assert.throws(() => validateTransactions(transaction({ subject: "x".repeat(32 * 1024) }), ledgerConfigs), /max-record-kb/);
});

test("main reports records only after the trusted persister succeeds", async () => {
  const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-push-"));
  const outputFile = path.join(outputDir, "output");
  try {
    const result = await main({
      artifact: transaction(),
      ledgerConfigs,
      outputFile,
      owner: "octo",
      repo: "repo",
      token: "test-token",
      serverHost: "github.com",
      workspaceDir: outputDir,
      githubClient: {},
      persistLedger: async ({ appends, config }) => {
        assert.equal(config.name, "findings");
        assert.equal(appends.length, 1);
        return { persisted: 1, already_present: 0, reconciled: 0 };
      },
    });

    assert.equal(result.ledgers.findings.persisted, 1);
    assert.match(fs.readFileSync(outputFile, "utf8"), /"persisted":1/);
  } finally {
    fs.rmSync(outputDir, { recursive: true, force: true });
  }
});

test("built-in persistence reports redacted transaction audit metadata", async () => {
  const config = { ...ledgerConfigs[0], type: "set", schema: undefined };
  const artifact = transaction({ operation: "add", value: { secret: "not-in-audit" } });
  const result = await main({
    artifact,
    ledgerConfigs: [config],
    outputFile: "",
    owner: "octo",
    repo: "repo",
    serverHost: "github.com",
    token: "test-token",
    githubClient: {},
    persistLedger: async () => ({ persisted: 1, already_present: 0, reconciled: 0 }),
  });

  assert.deepEqual(result.ledgers.findings.transactions, [
    {
      id: finalId("run-42:1", 0),
      transaction_id: "run-42:1",
      operation: "add",
      validated: true,
    },
  ]);
  assert.equal(result.ledgers.findings.type, "set");
  assert.doesNotMatch(JSON.stringify(result), /not-in-audit/);
});

test("claims audit counts only after persistence succeeds", async () => {
  const config = { ...ledgerConfigs[0], type: "claims", schema: undefined };
  const requests = [
    { operation: "claim", subject: "topic", claim: "assertion", reason: "evidence", citations: [{ type: "repository", path: "README.md" }] },
    { operation: "vote", claim_id: "#first", vote: "up" },
    { operation: "vote", claim_id: "#first", vote: "down" },
  ];
  const normalized = normalizeLedgerAppends(
    requests.map((request, index) => ({ ...request, ...(index === 0 ? { temp_id: "first" } : {}) })),
    {
      transactionId: "claims:1",
      ledgerNames: new Set(["findings"]),
      ledgers: { findings: config },
    }
  );
  const artifact = { version: 1, transaction_id: "claims:1", ledgers: { findings: { appends: normalized.appends } } };
  let persist = async () => {
    throw new Error("not durable");
  };
  const options = { artifact, ledgerConfigs: [config], outputFile: "", githubClient: {}, owner: "octo", repo: "repo", serverHost: "github.com", token: "test-token", persistLedger: (...args) => persist(...args) };
  await assert.rejects(main(options), /not durable/);
  persist = async () => ({ persisted: 3, already_present: 0, reconciled: 0 });
  const result = await main(options);
  assert.deepEqual(Object.fromEntries(["claims_added", "claim_votes_added", "upvotes", "downvotes", "invalid_vote_requests"].map(key => [key, result.ledgers.findings[key]])), {
    claims_added: 1,
    claim_votes_added: 2,
    upvotes: 1,
    downvotes: 1,
    invalid_vote_requests: 0,
  });
  persist = async () => ({ persisted: 1, already_present: 2, reconciled: 2, persisted_ids: [normalized.appends[2].record.id] });
  const retried = await main(options);
  assert.equal(retried.ledgers.findings.claims_added, 0);
  assert.equal(retried.ledgers.findings.claim_votes_added, 1);
  assert.equal(retried.ledgers.findings.upvotes, 0);
  assert.equal(retried.ledgers.findings.downvotes, 1);
});

test("invalid claim vote targets are rejected and audited without counting durable mutations", async () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "claims-vote-audit-"));
  const outputFile = path.join(directory, "output");
  const config = { ...ledgerConfigs[0], type: "claims", schema: undefined };
  const artifact = transaction({ operation: "vote", claim_id: finalId("missing", 0), vote: "down" });
  try {
    await assert.rejects(
      main({
        artifact,
        ledgerConfigs: [config],
        outputFile,
        githubClient: {},
        owner: "octo",
        repo: "repo",
        serverHost: "github.com",
        token: "test-token",
        persistLedger: async () => {
          throw new TypeError("Vote references a missing claim");
        },
      }),
      /missing claim/
    );
    const audit = JSON.parse(fs.readFileSync(outputFile, "utf8").trim().slice("ledger_result=".length));
    assert.deepEqual(audit.ledgers.findings, { requested: 1, persisted: 0, rejected: 1, invalid_vote_requests: 1 });
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test("appends canonical ledger state and delegates the upstream push", async () => {
  const workspaceDir = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-persist-"));
  const largeTransaction = transaction({ subject: "x".repeat(32500) });
  const config = { ...ledgerConfigs[0], max_patch_kb: 100 };
  const pushCalls = [];
  const previousCore = global.core;
  global.core = { debug: () => {}, info: () => {}, warning: () => {}, setFailed: () => {} };
  try {
    execSync("git init && git config user.name test && git config user.email test@example.com && git commit --allow-empty -m base", {
      cwd: workspaceDir,
      stdio: "pipe",
    });
    const baseRef = execSync("git rev-parse HEAD", { cwd: workspaceDir, encoding: "utf8" }).trim();
    assert.doesNotThrow(() => validateTransactions(largeTransaction, [config]));
    const persisted = await persistLedgerAppends({
      appends: largeTransaction.ledgers.findings.appends,
      config,
      githubClient: {},
      owner: "octo",
      repo: "repo",
      token: "test-token",
      serverHost: "github.com",
      workspaceDir,
      checkoutLedgerBranchFn: async ({ branchName }) => {
        execGitSync(["checkout", "-b", branchName], { cwd: workspaceDir, stdio: "pipe" });
        return baseRef;
      },
      pushChangesFn: async options => {
        pushCalls.push(options);
        return true;
      },
    });
    const shard = execFileSync("git", ["ls-tree", "-r", "--name-only", "ledgers/findings"], { cwd: workspaceDir, encoding: "utf8" }).trim();
    const canonical = execFileSync("git", ["show", `ledgers/findings:${shard}`], { cwd: workspaceDir, encoding: "utf8" });
    assert.equal(persisted.persisted, 1);
    assert.equal(pushCalls.length, 1);
    assert.equal(pushCalls[0].baseRef, baseRef);
    assert.equal(pushCalls[0].branchName, "ledgers/findings");
    assert.match(shard, /^ledger\/shards\//);
    assert.equal(JSON.parse(canonical.trim()).payload.id, largeTransaction.ledgers.findings.appends[0].record.id);
  } finally {
    if (previousCore === undefined) delete global.core;
    else global.core = previousCore;
    fs.rmSync(workspaceDir, { recursive: true, force: true });
  }
});

test("trusted claims persistence uses canonical envelope IDs without duplicating payload metadata", async () => {
  const workspaceDir = fs.mkdtempSync(path.join(os.tmpdir(), "claims-persist-"));
  const previousCore = global.core;
  global.core = { debug: () => {}, info: () => {}, warning: () => {}, setFailed: () => {} };
  const config = { ...ledgerConfigs[0], type: "claims", schema: undefined };
  const normalized = normalizeLedgerAppends(
    [
      { operation: "claim", temp_id: "finding", subject: "readme", claim: "Readme exists", reason: "Inspected", citations: [{ type: "repository", path: "README.md" }] },
      { operation: "vote", claim_id: "#finding", vote: "up" },
    ],
    { transactionId: "claims:1", ledgerNames: new Set(["findings"]), ledgers: { findings: config } }
  );
  try {
    execSync("git init && git config user.name test && git config user.email test@example.com && git commit --allow-empty -m base", { cwd: workspaceDir, stdio: "pipe" });
    const baseRef = execSync("git rev-parse HEAD", { cwd: workspaceDir, encoding: "utf8" }).trim();
    const result = await persistLedgerAppends({
      appends: normalized.appends,
      config,
      githubClient: {},
      owner: "octo",
      repo: "repo",
      token: "test-token",
      serverHost: "github.com",
      workspaceDir,
      checkoutLedgerBranchFn: async ({ branchName }) => {
        execGitSync(["checkout", "-b", branchName], { cwd: workspaceDir, stdio: "pipe" });
        return baseRef;
      },
      pushChangesFn: async () => true,
    });
    assert.equal(result.persisted, 2);
    const shards = execFileSync("git", ["ls-tree", "-r", "--name-only", "ledgers/findings"], { cwd: workspaceDir, encoding: "utf8" }).trim().split("\n");
    const records = shards.flatMap(shard =>
      execFileSync("git", ["show", `ledgers/findings:${shard}`], { cwd: workspaceDir, encoding: "utf8" })
        .trim()
        .split("\n")
        .map(line => JSON.parse(line))
    );
    assert.deepEqual(
      records.map(record => record.id),
      normalized.appends.map(append => append.record.id)
    );
    assert.ok(records.every(record => !Object.hasOwn(record.payload, "id")));
    assert.equal(records[1].payload.claim_id, records[0].id);
  } finally {
    if (previousCore === undefined) delete global.core;
    else global.core = previousCore;
    fs.rmSync(workspaceDir, { recursive: true, force: true });
  }
});

test("readTransactions rejects malformed JSON and symlink artifacts", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-transactions-"));
  const file = path.join(directory, "artifact.json");
  const link = path.join(directory, "link.json");
  try {
    fs.writeFileSync(file, "{");
    assert.throws(() => readTransactions(file), /Failed to read validated ledger transaction artifact/);
    fs.writeFileSync(file, JSON.stringify(transaction()));
    fs.symlinkSync(file, link);
    assert.throws(() => readTransactions(link), /Failed to read validated ledger transaction artifact/);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});
