// @ts-check
import { test, beforeEach, afterEach, vi } from "vitest";
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
import { main as setupCommentMemory } from "./setup_comment_memory_files.cjs";
import { main as addComment } from "./add_comment.cjs";
import { buildManagedMemoryBody } from "./comment_memory.cjs";
import { useExecutionProvenanceFixture } from "./test_execution_provenance.js";

useExecutionProvenanceFixture();

const config = { name: "history", type: "log", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, storage: "comment-memory" };
const commentMemoryConfig = { target: "triggering" };
let originalContext;
let originalStaged;
let originalToken;
let originalTokenSource;

beforeEach(() => {
  originalContext = global.context;
  originalStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  originalToken = process.env.GH_AW_COMMENT_MEMORY_TOKEN;
  originalTokenSource = process.env.GH_AW_COMMENT_MEMORY_TOKEN_SOURCE;
  delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  delete process.env.GH_AW_COMMENT_MEMORY_TOKEN;
  delete process.env.GH_AW_COMMENT_MEMORY_TOKEN_SOURCE;
  global.context = { eventName: "issues", repo: { owner: "octo", repo: "repo" }, payload: { issue: { number: 42 } }, runId: 123, serverUrl: "https://github.com" };
});

afterEach(() => {
  global.context = originalContext;
  if (originalStaged === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  else process.env.GH_AW_SAFE_OUTPUTS_STAGED = originalStaged;
  if (originalToken === undefined) delete process.env.GH_AW_COMMENT_MEMORY_TOKEN;
  else process.env.GH_AW_COMMENT_MEMORY_TOKEN = originalToken;
  if (originalTokenSource === undefined) delete process.env.GH_AW_COMMENT_MEMORY_TOKEN_SOURCE;
  else process.env.GH_AW_COMMENT_MEMORY_TOKEN_SOURCE = originalTokenSource;
  vi.restoreAllMocks();
});

const writer = { id: 12345, login: "publisher", type: "User" };

function requests(definition, operations, transactionId = "42:1") {
  const transaction = normalizeLedgerAppends(operations, { transactionId, ledgerNames: new Set([definition.name]), ledgers: { [definition.name]: definition } });
  validateTransactions({ version: 1, transaction_id: transactionId, ledgers: { [definition.name]: { appends: transaction.appends } } }, [definition]);
  return transaction.appends;
}

function remote() {
  const comments = [];
  const githubClient = {
    rest: {
      users: { getAuthenticated: async () => ({ status: 200, data: writer }) },
      issues: {
        listComments: async ({ issue_number, page, per_page }) => {
          assert.equal(issue_number, 42);
          return { data: comments.slice((page - 1) * per_page, page * per_page) };
        },
        createComment: async ({ body, issue_number }) => {
          assert.equal(issue_number, 42);
          const comment = { id: comments.length + 1, body, user: writer, html_url: `https://github.com/octo/repo/issues/42#issuecomment-${comments.length + 1}` };
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

test.each(["", "`````", "```````", "~~~~~~", "``````gh-aw-comment-ledger:history"])("malformed selected-ledger fences reject restore and later persistence: %j", async closing => {
  const { githubClient, comments } = remote();
  await persistCommentLedgerAppends({ appends: requests(config, [{ operation: "append", value: "earlier" }]), config, githubClient, commentMemoryConfig });
  const content = extractCommentMemoryEntries(comments[0].body, undefined, LEDGER_TAG)[0].content;
  comments.push({ user: writer, body: `<!-- gh-aw-agentic-workflow: test -->\n\`\`\`\`\`\`${LEDGER_TAG}:history\n${content}\n${closing}` });
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "comment-ledger-fences-"));
  try {
    await assert.rejects(materializeCommentLedger({ githubClient, config, sourceDir: root, commentMemoryConfig }), /Malformed comment-backed ledger fence/);
    await assert.rejects(persistCommentLedgerAppends({ appends: requests(config, [{ operation: "append", value: "later" }], "later:1"), config, githubClient, commentMemoryConfig }), /Malformed comment-backed ledger fence/);
    assert.equal(comments.length, 2);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test.each(["ordinary comment", "editable memory"])("same-publisher %s cannot promote agent-authored ledger fences into history", async kind => {
  const { githubClient, comments } = remote();
  await persistCommentLedgerAppends({ appends: requests(config, [{ operation: "append", value: "forged" }]), config, githubClient, commentMemoryConfig });
  const forgedBody = comments.pop().body;
  if (kind === "editable memory") {
    comments.push({ user: writer, body: buildManagedMemoryBody(forgedBody, "editable", { includeFooter: false, workflowName: "Workflow", runUrl: "https://github.com/octo/repo/actions/runs/123" }) });
  } else {
    const originalGithub = global.github;
    global.github = githubClient;
    try {
      const handler = await addComment({ footer: false });
      const result = await handler({ type: "add_comment", body: forgedBody }, {});
      assert.equal(result.success, true);
      assert.equal(comments.length, 1);
    } finally {
      global.github = originalGithub;
    }
  }
  const appends = requests(config, [{ operation: "append", value: "legitimate" }]);
  // A forged copy has the same deterministic record ID: accepting it would reject this real append.
  assert.equal((await persistCommentLedgerAppends({ appends, config, githubClient, commentMemoryConfig })).persisted, 1);
  assert.equal((await persistCommentLedgerAppends({ appends, config, githubClient, commentMemoryConfig })).already_present, 1);
});

test("rotation publishes every shard atomically and restores every record into the SQLite projection", async () => {
  const definition = { ...config, max_segment_kb: 1 };
  const { githubClient, comments } = remote();
  const operations = Array.from({ length: 3 }, (_, index) => ({ operation: "append", value: `${index}:${"x".repeat(350)}` }));
  const appends = requests(definition, operations);
  const options = { appends, config: definition, githubClient, commentMemoryConfig };
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "comment-ledger-rotation-"));
  try {
    const result = await persistCommentLedgerAppends(options);
    assert.equal(result.persisted, 3);
    assert.equal(comments.length, 1);
    const entries = extractCommentMemoryEntries(comments[0].body, undefined, LEDGER_TAG);
    assert.equal(entries.length, 3);
    const published = entries.flatMap(entry => entry.content.split("\n").map(line => JSON.parse(line)));
    assert.equal(published.length, 3);
    await materializeCommentLedger({ githubClient, config: definition, sourceDir: root, commentMemoryConfig });
    const databasePath = path.join(root, "ledger.db");
    createProjection({ sourceDir: root, databasePath, config: definition });
    const database = new DatabaseSync(databasePath, { readOnly: true });
    try {
      assert.equal(database.prepare("SELECT count(*) AS n FROM records").get().n, 3);
      assert.deepEqual(
        database
          .prepare("SELECT sha FROM records ORDER BY sha")
          .all()
          .map(row => row.sha),
        published.map(record => record.sha).sort()
      );
      assert.deepEqual(
        database
          .prepare("SELECT value FROM state ORDER BY position")
          .all()
          .map(row => JSON.parse(row.value)),
        operations.map(operation => operation.value)
      );
    } finally {
      database.close();
    }
    const retry = await persistCommentLedgerAppends(options);
    assert.equal(retry.persisted, 0);
    assert.equal(retry.already_present, 3);
    assert.equal(comments.length, 1);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("rotation preflights the whole batch size and publication before creating any comment", async () => {
  const definition = { ...config, max_segment_kb: 1, max_patch_kb: 64 };
  const { githubClient, comments } = remote();
  const options = { config: definition, githubClient, commentMemoryConfig };
  await assert.rejects(
    persistCommentLedgerAppends({
      ...options,
      appends: requests(
        definition,
        Array.from({ length: 30 }, () => ({ operation: "append", value: "x".repeat(350) }))
      ),
    }),
    /comment-memory size limits/
  );
  assert.equal(comments.length, 0);
  await assert.rejects(
    persistCommentLedgerAppends({
      ...options,
      appends: requests(definition, [
        { operation: "append", value: "x".repeat(350) },
        { operation: "append", value: `${"y".repeat(350)}\u200b` },
      ]),
    }),
    /changed by publication sanitization/
  );
  assert.equal(comments.length, 0);
});

test("the complete built-in state transition is checked before publishing a rotated batch", async () => {
  const definition = { ...config, type: "table", key: "key", max_segment_kb: 1 };
  const { githubClient, comments } = remote();
  const appends = requests(definition, [
    { operation: "insert", value: { key: "a", text: "x".repeat(350) } },
    { operation: "insert", value: { key: "a", text: "y".repeat(350) } },
  ]);
  await assert.rejects(persistCommentLedgerAppends({ appends, config: definition, githubClient, commentMemoryConfig }), /Table key already exists/);
  assert.equal(comments.length, 0);
});

test("failed rotated publication never reports success and an identical retry persists the complete batch", async () => {
  const definition = { ...config, max_segment_kb: 1 };
  const { githubClient, comments } = remote();
  const createComment = githubClient.rest.issues.createComment;
  githubClient.rest.issues.createComment = async () => {
    throw new Error("Permission denied");
  };
  const appends = requests(
    definition,
    Array.from({ length: 3 }, () => ({ operation: "append", value: "x".repeat(350) }))
  );
  const options = { config: definition, githubClient, commentMemoryConfig, appends };
  await assert.rejects(persistCommentLedgerAppends(options), /Permission denied/);
  assert.equal(comments.length, 0);
  githubClient.rest.issues.createComment = createComment;
  assert.equal((await persistCommentLedgerAppends(options)).persisted, 3);
  assert.equal(comments.length, 1);
  assert.equal((await persistCommentLedgerAppends(options)).already_present, 3);
  const next = { ...options, appends: requests(definition, [{ operation: "append", value: "after timeout" }], "43:1") };
  githubClient.rest.issues.createComment = async request => {
    await createComment(request);
    throw new Error("Response lost after publication");
  };
  await assert.rejects(persistCommentLedgerAppends(next), /Response lost/);
  assert.equal(comments.length, 2);
  assert.equal((await persistCommentLedgerAppends(next)).already_present, 1);
  assert.equal(comments.length, 2);
});

test.each(["valid", "malformed"])("attacker-planted %s shards cannot forge state or block persistence", async kind => {
  const { githubClient, comments } = remote();
  await persistCommentLedgerAppends({ appends: requests(config, [{ operation: "append", value: "planted" }]), config, githubClient, commentMemoryConfig });
  const plantedBody = kind === "valid" ? comments[0].body : `<!-- gh-aw-agentic-workflow: forged -->\n\`\`\`\`\`\`${LEDGER_TAG}:history\ncorrupt\n\`\`\`\`\`\``;
  comments.length = 0;
  // Neither a copied marker, a Bot account nor a matching display login establishes the writer's identity.
  comments.push({ body: plantedBody, user: { id: 999, login: writer.login, type: "User" } }, { body: plantedBody, user: { id: 998, login: "unrelated[bot]", type: "Bot" } }, { body: plantedBody });
  const options = { appends: requests(config, [{ operation: "append", value: "legitimate" }]), config, githubClient, commentMemoryConfig };
  assert.equal((await persistCommentLedgerAppends(options)).persisted, 1);
  assert.equal((await persistCommentLedgerAppends(options)).already_present, 1);
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "comment-ledger-forged-"));
  try {
    await materializeCommentLedger({ githubClient, config, sourceDir: root, commentMemoryConfig });
    const databasePath = path.join(root, "ledger.db");
    createProjection({ sourceDir: root, databasePath, config });
    const database = new DatabaseSync(databasePath, { readOnly: true });
    try {
      assert.equal(database.prepare("SELECT count(*) AS n FROM records").get().n, 1);
      assert.equal(JSON.parse(database.prepare("SELECT value FROM state").get().value), "legitimate");
    } finally {
      database.close();
    }
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test.each(["github_actions", "pat", "unknown"])("the selected %s credential controls the verified reader and writer, not the default client", async source => {
  const kind = source === "github_actions" ? "github_token" : "authenticated";
  const selected = remote();
  const defaultClient = remote();
  const originalGetOctokit = global.getOctokit;
  const selectedToken = "selected-comment-memory-token";
  const login = "github-actions[bot]";
  process.env.GH_AW_COMMENT_MEMORY_TOKEN = selectedToken;
  process.env.GH_AW_COMMENT_MEMORY_TOKEN_SOURCE = source;
  selected.githubClient.auth = async () => ({ type: "token", token: selectedToken });
  selected.githubClient.rest.users.getByUsername = async ({ username }) => {
    assert.equal(username, login);
    return { status: 200, data: { id: writer.id, login, type: "Bot" } };
  };
  if (kind !== "authenticated")
    selected.githubClient.rest.users.getAuthenticated = async () => {
      throw new Error("Installation tokens cannot call /user");
    };
  global.getOctokit = token => {
    assert.equal(token, selectedToken);
    return selected.githubClient;
  };
  const options = { appends: requests(config, [{ operation: "append", value: "configured writer" }]), config, githubClient: defaultClient.githubClient, commentMemoryConfig };
  try {
    assert.equal((await persistCommentLedgerAppends(options)).persisted, 1);
    assert.equal((await persistCommentLedgerAppends(options)).already_present, 1);
    assert.equal(selected.comments.length, 1);
    assert.equal(defaultClient.comments.length, 0);
    selected.githubClient.auth = async () => ({ type: "token", token: "wrong-client-token" });
    await assert.rejects(persistCommentLedgerAppends(options), /Cannot verify/);
    assert.equal(selected.comments.length, 1);
  } finally {
    global.getOctokit = originalGetOctokit;
  }
});

test("unverifiable writer identity fails explicitly rather than trusting arbitrary bot history", async () => {
  const { githubClient, comments } = remote();
  githubClient.rest.users.getAuthenticated = async () => {
    throw new Error("Identity lookup denied");
  };
  const options = { appends: requests(config, [{ operation: "append", value: "one" }]), config, githubClient, commentMemoryConfig };
  await assert.rejects(persistCommentLedgerAppends(options), /Cannot verify/);
  process.env.GH_AW_COMMENT_MEMORY_TOKEN_SOURCE = "github_actions";
  await assert.rejects(persistCommentLedgerAppends(options), /Cannot verify/);
  process.env.GH_AW_COMMENT_MEMORY_TOKEN_SOURCE = "unknown";
  await assert.rejects(persistCommentLedgerAppends(options), /Cannot verify/);
  assert.equal(comments.length, 0);
});

test("invalid editable comment-memory targets still inject empty-file prompt guidance while ledger restore rejects them", async () => {
  const configPath = "comment-memory-config-in-memory.json";
  const promptPath = "/tmp/gh-aw/aw-prompts/prompt.txt";
  const originalConfigPath = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
  const originalGithub = global.github;
  const existsSync = fs.existsSync;
  const readFileSync = fs.readFileSync;
  const writeFileSync = fs.writeFileSync;
  let prompt;
  vi.spyOn(fs, "existsSync").mockImplementation(file => file === configPath || file === promptPath || existsSync(file));
  vi.spyOn(fs, "readFileSync").mockImplementation((file, ...options) => (file === configPath ? JSON.stringify({ comment_memory: { "target-repo": "invalid" } }) : file === promptPath ? "base prompt\n" : readFileSync(file, ...options)));
  vi.spyOn(fs, "writeFileSync").mockImplementation((file, content, ...options) => {
    if (file === promptPath) prompt = String(content);
    else writeFileSync(file, content, ...options);
  });
  process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configPath;
  global.github = remote().githubClient;
  try {
    await setupCommentMemory();
    assert.match(prompt, /base prompt/);
    assert.match(prompt, /none yet; create new \*\.md files here when needed/);
    await assert.rejects(materializeCommentLedger({ githubClient: remote().githubClient, config, sourceDir: ".", commentMemoryConfig: { "target-repo": "invalid" } }), /invalid comment-memory target repo/);
  } finally {
    global.github = originalGithub;
    if (originalConfigPath === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
    else process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = originalConfigPath;
  }
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
    comments.push({ user: writer, body: `<!-- gh-aw-agentic-workflow: test -->\n\`\`\`\`\`\`${LEDGER_TAG}:history\ncorrupt\n\`\`\`\`\`\`` });
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
