// @ts-check
import { test, beforeEach, afterEach } from "vitest";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { DatabaseSync } from "node:sqlite";
import { materializeCommentLedger, persistCommentLedgerAppends, LEDGER_TAG } from "./comment_memory_ledger.cjs";
import { extractCommentMemoryEntries, COMMENT_MEMORY_MAX_FILE_BYTES } from "./comment_memory_helpers.cjs";
import { createProjection } from "./create_ledger_projection.cjs";
import { normalizeLedgerAppends } from "./ledger_transactions.cjs";
import { main as persistLedgers, validateTransactions } from "./push_ledger_changes.cjs";

const config = { name: "history", type: "log", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, storage: "comment-memory" };
const commentMemoryConfig = { target: "triggering" };
let originalContext;
let originalStaged;

beforeEach(() => {
  originalContext = global.context;
  originalStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  global.context = { repo: { owner: "octo", repo: "repo" }, payload: { issue: { number: 42 } }, runId: 123, serverUrl: "https://github.com" };
});

afterEach(() => {
  global.context = originalContext;
  if (originalStaged === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  else process.env.GH_AW_SAFE_OUTPUTS_STAGED = originalStaged;
});

function requests(definition, operations, transactionId = "42:1") {
  const transaction = normalizeLedgerAppends(operations, { transactionId, ledgerNames: new Set([definition.name]), ledgers: { [definition.name]: definition } });
  validateTransactions({ version: 1, transaction_id: transactionId, ledgers: { [definition.name]: { appends: transaction.appends } } }, [definition]);
  return transaction.appends;
}

function remote() {
  const comments = [];
  const githubClient = {
    rest: {
      issues: {
        listComments: async ({ issue_number, page, per_page }) => {
          assert.equal(issue_number, 42);
          return { data: comments.slice((page - 1) * per_page, page * per_page) };
        },
        createComment: async ({ body, issue_number }) => {
          assert.equal(issue_number, 42);
          const comment = { id: comments.length + 1, body, html_url: `https://github.com/octo/repo/issues/42#issuecomment-${comments.length + 1}` };
          comments.push(comment);
          return { data: comment };
        },
      },
    },
  };
  return { githubClient, comments };
}

test.each([
  ["log", [{ operation: "append", value: { text: "@user <tag> \n``````\n</details> \u00e9" } }], "state", 1],
  [
    "set",
    [
      { operation: "add", value: "a" },
      { operation: "add", value: "b" },
      { operation: "remove", value: "a" },
    ],
    "state",
    1,
  ],
  [
    "map",
    [
      { operation: "put", key: "a", value: 1 },
      { operation: "delete", key: "a" },
    ],
    "state",
    0,
  ],
  ["counter", [{ operation: "increment", name: "runs", amount: 2 }], "state", 1],
  [
    "table",
    [
      { operation: "insert", value: { key: "a", score: 1 } },
      { operation: "update", key: "a", patch: { score: 2 } },
    ],
    "state",
    1,
  ],
  ["notes", [{ operation: "note", subject: "Parser", note: "A note", reason: "Evidence", citations: [{ type: "repository", path: "pkg/parser/parser.go", start_line: 1 }] }], "notes", 1],
])("comment-backed %s persists readable JSONL and builds a read-only SQLite projection for the agent", async (type, operations, table, count) => {
  const definition = { ...config, type, ...(type === "table" ? { key: "key" } : {}) };
  const { githubClient, comments } = remote();
  const appends = requests(definition, operations);
  const options = { appends, config: definition, githubClient, commentMemoryConfig };
  const first = await persistCommentLedgerAppends(options);
  assert.equal(first.persisted, appends.length);
  assert.match(first.url, /issuecomment-1$/);
  assert.equal(comments.length, 1);
  assert.deepEqual(extractCommentMemoryEntries(comments[0].body), []);
  const entries = extractCommentMemoryEntries(comments[0].body, undefined, LEDGER_TAG);
  assert.equal(entries.length, 1);
  const publishedRecords = entries[0].content.split("\n").map(line => JSON.parse(line));
  assert.equal(publishedRecords.length, appends.length);
  assert.equal(publishedRecords[0].version, 1);
  const retry = await persistCommentLedgerAppends(options);
  assert.equal(retry.persisted, 0);
  assert.equal(retry.already_present, appends.length);
  assert.equal(comments.length, 1);
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "comment-ledger-test-"));
  try {
    await materializeCommentLedger({ githubClient, config: definition, sourceDir: root, commentMemoryConfig });
    const databasePath = path.join(root, "ledger.db");
    createProjection({ sourceDir: root, databasePath, config: definition });
    const database = new DatabaseSync(databasePath, { readOnly: true });
    try {
      assert.equal(database.prepare(`SELECT count(*) AS n FROM ${table}`).get().n, count);
      assert.equal(database.prepare("SELECT count(*) AS n FROM records").get().n, appends.length);
      assert.deepEqual(
        database
          .prepare("SELECT sha FROM records ORDER BY sha")
          .all()
          .map(row => row.sha),
        publishedRecords.map(record => record.sha).sort()
      );
      if (type === "log") assert.deepEqual(JSON.parse(database.prepare("SELECT value FROM state").get().value), operations[0].value);
    } finally {
      database.close();
    }
    assert.equal(fs.statSync(databasePath).mode & 0o777, 0o444);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("successive batches append immutable comments and reject conflicting transaction IDs", async () => {
  const { githubClient, comments } = remote();
  const first = requests(config, [{ operation: "append", value: "one" }]);
  await persistCommentLedgerAppends({ appends: first, config, githubClient, commentMemoryConfig });
  const firstBody = comments[0].body;
  await persistCommentLedgerAppends({ appends: requests(config, [{ operation: "append", value: "two" }], "43:1"), config, githubClient, commentMemoryConfig });
  assert.equal(comments.length, 2);
  assert.equal(comments[0].body, firstBody);
  await assert.rejects(persistCommentLedgerAppends({ appends: requests(config, [{ operation: "append", value: "conflict" }]), config, githubClient, commentMemoryConfig }), /Conflicting ledger record ID/);
  assert.equal(comments.length, 2);
});

test("the trusted persistence entry point routes comment storage without creating a Git branch", async () => {
  const { githubClient, comments } = remote();
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "comment-ledger-config-"));
  const originalConfigPath = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
  try {
    const configPath = path.join(root, "config.json");
    fs.writeFileSync(configPath, JSON.stringify({ comment_memory: commentMemoryConfig }));
    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configPath;
    const appends = requests(config, [{ operation: "append", value: "persisted" }]);
    const result = await persistLedgers({
      artifact: { version: 1, transaction_id: "42:1", ledgers: { history: { appends } } },
      ledgerConfigs: [config],
      githubClient,
      outputFile: "",
    });
    assert.equal(result.ledgers.history.persisted, 1);
    assert.equal(result.ledgers.history.storage, "comment-memory");
    assert.equal(result.ledgers.history.branch, undefined);
    assert.equal(comments.length, 1);
  } finally {
    if (originalConfigPath === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
    else process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = originalConfigPath;
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("full comment scans and aggregate history limits cannot silently discard earlier records", async () => {
  const { githubClient, comments } = remote();
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "comment-ledger-limits-"));
  try {
    for (let index = 0; index < 11; index++) {
      await persistCommentLedgerAppends({
        appends: requests(config, [{ operation: "append", value: "x".repeat(4000) }], `batch-${index}:1`),
        config,
        githubClient,
        commentMemoryConfig,
      });
    }
    await assert.rejects(
      persistCommentLedgerAppends({
        appends: requests(config, [{ operation: "append", value: "x".repeat(4000) }], "overflow:1"),
        config,
        githubClient,
        commentMemoryConfig,
      }),
      /comment-memory size limits/
    );
    assert.equal(comments.length, 11);
    let pages = 0;
    githubClient.rest.issues.listComments = async () => {
      pages++;
      return { data: Array.from({ length: 100 }, () => ({ body: "ordinary comment" })) };
    };
    await assert.rejects(materializeCommentLedger({ githubClient, config, sourceDir: root, commentMemoryConfig }), /scan limit reached/);
    assert.equal(pages, 50);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("size limits and staged mode fail closed without publishing records", async () => {
  const { githubClient, comments } = remote();
  await assert.rejects(
    persistCommentLedgerAppends({
      appends: requests({ ...config, max_patch_kb: 32 }, [{ operation: "append", value: "x".repeat(COMMENT_MEMORY_MAX_FILE_BYTES) }]),
      config: { ...config, max_patch_kb: 32 },
      githubClient,
      commentMemoryConfig,
    }),
    /comment-memory size limits/
  );
  assert.equal(comments.length, 0);
  const appends = requests(config, [{ operation: "append", value: "preview" }]);
  const staged = await persistCommentLedgerAppends({ appends, config, githubClient, commentMemoryConfig: { ...commentMemoryConfig, staged: true } });
  assert.equal(staged.persisted, 0);
  process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
  assert.equal((await persistCommentLedgerAppends({ appends, config, githubClient, commentMemoryConfig })).persisted, 0);
  assert.equal(comments.length, 0);
});

test("publication cannot silently change hashed JSONL records", async () => {
  const { githubClient, comments } = remote();
  await assert.rejects(
    persistCommentLedgerAppends({
      appends: requests(config, [{ operation: "append", value: "invisible\u200btext" }]),
      config,
      githubClient,
      commentMemoryConfig,
    }),
    /changed by publication sanitization/
  );
  assert.equal(comments.length, 0);
});

test("missing targets, cross-repository denial, corrupt history and API errors are not empty-ledger fallbacks", async () => {
  const { githubClient, comments } = remote();
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "comment-ledger-errors-"));
  const options = { githubClient, config, sourceDir: root, commentMemoryConfig };
  try {
    global.context.payload = {};
    await assert.rejects(materializeCommentLedger(options), /no target/);
    global.context.payload = { issue: { number: 42 } };
    await assert.rejects(materializeCommentLedger({ ...options, commentMemoryConfig: { "target-repo": "other/repo" } }), /No allowlist/);
    comments.push({ body: `<!-- gh-aw-agentic-workflow: test -->\n\`\`\`\`\`\`${LEDGER_TAG}:history\ncorrupt\n\`\`\`\`\`\`` });
    await assert.rejects(materializeCommentLedger(options), /Invalid comment-backed ledger JSONL shard/);
    comments.length = 0;
    githubClient.rest.issues.createComment = async () => {
      throw new Error("Permission denied");
    };
    await assert.rejects(persistCommentLedgerAppends({ appends: requests(config, [{ operation: "append", value: "one" }]), config, githubClient, commentMemoryConfig }), /Permission denied/);
    assert.equal(comments.length, 0);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
