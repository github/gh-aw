// @ts-check
"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { persistLedgerAppends, readLedgerAppendRequests } = require("./push_ledger_changes.cjs");

test("reads only ledger append requests from the safe-output artifact", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-safe-outputs-"));
  const file = path.join(directory, "safeoutputs.jsonl");
  try {
    fs.writeFileSync(file, `${JSON.stringify({ type: "ledger_append", ledger: "findings", record: { subject: "bug" } })}\n${JSON.stringify({ type: "create_issue", title: "other" })}\n`);
    assert.deepEqual(readLedgerAppendRequests(file), [{ ledger: "findings", temp_id: undefined, record: { subject: "bug" } }]);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test("commits ledger transactions and delegates upstream pushes to repo-memory helpers", async () => {
  const workspaceDir = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-push-"));
  const commands = [];
  const pushCalls = [];
  try {
    fs.mkdirSync(path.join(workspaceDir, ".git"));
    const result = await persistLedgerAppends({
      name: "findings",
      ledger: {
        appends: [{ transaction_id: "run-1", record: { id: "ldg-record" } }],
      },
      config: { branchName: "ledgers/findings", maxSegmentKB: 1 },
      transactionId: "run-1",
      workspaceDir,
      targetRepo: "owner/repo",
      githubClient: {},
      ghToken: "test-token",
      serverHost: "github.test",
      checkoutBranchFn: async ({ branchName }) => {
        assert.equal(branchName, "ledgers/findings");
        return "base-sha";
      },
      execGitSyncFn: (args, options) => {
        commands.push({ args, options });
        return args[0] === "branch" ? "main\n" : "";
      },
      pushChangesFn: async options => pushCalls.push(options),
    });

    assert.equal(result.persisted, 1);
    assert.equal(result.branch, "ledgers/findings");
    assert.equal(pushCalls.length, 1);
    assert.equal(pushCalls[0].baseRef, "base-sha");
    assert.equal(pushCalls[0].branchName, "ledgers/findings");
    assert.ok(commands.some(({ args }) => args[0] === "commit"));
    assert.equal(fs.readFileSync(path.join(workspaceDir, "ledger/transactions/run-1.jsonl"), "utf8"), `${JSON.stringify({ transaction_id: "run-1", record: { id: "ldg-record" } })}\n`);
  } finally {
    fs.rmSync(workspaceDir, { recursive: true, force: true });
  }
});
