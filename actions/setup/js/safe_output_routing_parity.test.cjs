import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";

const require = createRequire(import.meta.url);
const root = fs.mkdtempSync(path.join(process.cwd(), ".safe-output-routing-parity-"));
// Redirect the manager's fixed artifact destinations without changing runtime defaults.
const constants = require("./constants.cjs");
const originalConstants = { ...constants };
for (const [key, value] of Object.entries(constants)) {
  if (typeof value === "string" && value.startsWith("/tmp/gh-aw/")) constants[key] = path.join(root, value.slice("/tmp/gh-aw/".length));
}
const redact = require("./redact_secrets.cjs");
const originalGatewayPaths = [...redact.MCP_GATEWAY_CONFIG_PATHS];
redact.MCP_GATEWAY_CONFIG_PATHS.splice(0, redact.MCP_GATEWAY_CONFIG_PATHS.length, path.join(root, "missing-gateway.json"));
const scope = require("./work_queue_claim_scope.cjs");
const manager = require("./safe_output_handler_manager.cjs");
const { processSafeOutput } = require("./safe_output_processor.cjs");
const { createHandlers } = require("./safe_outputs_handlers.cjs");
const { createAppendFunction } = require("./safe_outputs_append.cjs");
const manifest = require("./safe_output_manifest.cjs");
const { main: actionFactory } = require("./safe_output_action_handler.cjs");
const { withClaimEffectClients } = require("./work_queue_effect_client.cjs");
const { createRestEffectHandler } = require("./work_queue_rest_adapter.cjs");
const { createGraphqlEffectHandler } = require("./work_queue_graphql_adapter.cjs");
const { validateStoredAssignment } = require("./work_queue_binding.cjs");
const { queueFixture } = require("./work_queue_lifecycle.test_helpers.cjs");
const { serializeTransactionLog } = require("./work_queue_replay.cjs");

const processorConfig = { itemType: "noop", configKey: "noop", displayName: "Noop", itemTypeName: "noop", supportsIssue: true, envVars: {} };
const preview = { title: "Preview", description: "Preview", renderItem: item => item.message };
const authorize = request => ({ authorized: true, claim_handle: request.claim_handle });
let oldEnv;
let oldGlobals;

function assignment(count = 1) {
  return {
    version: 3,
    dispatch_id: "dispatch",
    request_id: "request",
    commit_id: "commit",
    policy_epoch: "policy",
    pool: "default",
    worker_profile: "default",
    claims: Array.from({ length: count }, (_, index) => ({ handle: `h${index + 1}`, claim_id: `c${index + 1}`, work_id: `w${index + 1}`, work: {}, result_refs: [] })),
  };
}

function writeOutput(items) {
  process.env.GH_AW_AGENT_OUTPUT = path.join(root, "agent-output.json");
  fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, JSON.stringify({ items }));
}

function activate(count, items, options = {}) {
  const fixture = queueFixture({ count, bound: true, ...options });
  process.env.GH_AW_WORK_QUEUE_ENABLED = "true";
  process.env.GH_AW_WORK_QUEUE_ROLE = "worker";
  process.env.GH_AW_WORK_QUEUE_SNAPSHOT = path.join(root, "snapshot.json");
  fs.writeFileSync(
    process.env.GH_AW_WORK_QUEUE_SNAPSHOT,
    JSON.stringify({ version: 3, sha: "head", transactionLog: serializeTransactionLog(fixture.transactions), captured_at: fixture.at, origin: fixture.workerActor, worker: fixture.assignment, role: "worker" })
  );
  global.context = fixture.workerContext;
  writeOutput(items(fixture.assignment.claims));
  return fixture;
}

function managerOptions(overrides = {}) {
  return {
    authorize: vi.fn(authorize),
    controlIntentPath: path.join(root, "missing-intents.jsonl"),
    deliveryArtifactRoot: path.join(root, "delivery"),
    // An untrusted inventory cannot manufacture a verified delivery or Result.
    readControlInventory: async () => ({ controls: [] }),
    finalizeResults: vi.fn(async () => ({ claims: {} })),
    ...overrides,
  };
}

beforeEach(() => {
  oldEnv = { ...process.env };
  oldGlobals = Object.fromEntries(["core", "context", "github", "getOctokit"].map(key => [key, Object.getOwnPropertyDescriptor(global, key)]));
  for (const key of Object.keys(process.env)) if (key.startsWith("GH_AW_")) delete process.env[key];
  process.env.RUNNER_TEMP = root;
  process.env.GITHUB_WORKSPACE = root;
  process.env.GITHUB_REPOSITORY = "owner/repo";
  process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({ noop: {} });
  global.core = Object.fromEntries(["info", "debug", "warning", "error", "notice", "setOutput", "setFailed"].map(key => [key, vi.fn()]));
  global.core.summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
  global.context = { repo: { owner: "owner", repo: "repo" }, payload: { issue: { number: 42 } }, eventName: "issues", runId: "42" };
  global.github = { rest: { rateLimit: { get: vi.fn(async () => ({ data: { resources: { core: { remaining: 5000, limit: 5000 } } } })) } } };
  global.getOctokit = vi.fn(() => global.github);
});

afterEach(() => {
  for (const key of Object.keys(process.env)) if (!Object.hasOwn(oldEnv, key)) delete process.env[key];
  Object.assign(process.env, oldEnv);
  for (const [key, descriptor] of Object.entries(oldGlobals)) {
    if (descriptor) Object.defineProperty(global, key, descriptor);
    else delete global[key];
  }
  vi.restoreAllMocks();
});

afterAll(() => {
  Object.assign(constants, originalConstants);
  redact.MCP_GATEWAY_CONFIG_PATHS.splice(0, redact.MCP_GATEWAY_CONFIG_PATHS.length, ...originalGatewayPaths);
  fs.rmSync(root, { recursive: true, force: true });
});

describe("disabled safe-output behavioral parity", () => {
  it.each([undefined, "false"].flatMap(enabled => [undefined, "worker", "dispatcher", "observer", "invalid"].map(role => [enabled, role])))(
    "preserves action inputs, append payloads and API calls with enabled=%s and stray role=%s",
    async (enabled, role) => {
      if (enabled !== undefined) process.env.GH_AW_WORK_QUEUE_ENABLED = enabled;
      if (role !== undefined) process.env.GH_AW_WORK_QUEUE_ROLE = role;
      process.env.GH_AW_WORK_QUEUE_SNAPSHOT = path.join(root, "not-a-snapshot");
      global.context.payload.inputs = { work_queue_assignment: "malformed stray assignment" };
      const message = { type: "custom", claim_handle: null, claim_id: "user-input", work_id: "user-work", _claimScopeError: "ordinary input", body: "Body" };
      const outputFile = path.join(root, "ordinary.jsonl");
      fs.writeFileSync(outputFile, "");
      createAppendFunction(outputFile)(message);
      expect(JSON.parse(fs.readFileSync(outputFile, "utf8"))).toEqual(message);
      const action = await actionFactory({ action_name: "custom" });
      const result = await manager.processMessages(new Map([["custom", action]]), [message]);
      expect(result.success).toBe(true);
      expect(JSON.parse(result.results[0].result.payload)).toEqual({ claim_handle: null, claim_id: "user-input", work_id: "user-work", _claimScopeError: "ordinary input", body: "Body" });
      const client = global.github;
      client.rest.issues = { create: vi.fn(async args => ({ data: { number: 10, ...args } })) };
      const apiHandler = vi.fn(async input => {
        await global.github.rest.issues.create({ owner: "owner", repo: "repo", title: input.body });
        return { success: true };
      });
      expect((await manager.processMessages(new Map([["custom", apiHandler]]), [message])).success).toBe(true);
      expect(global.github).toBe(client);
      expect(client.rest.issues.create).toHaveBeenCalledWith({ owner: "owner", repo: "repo", title: "Body" });
      expect(global.getOctokit).not.toHaveBeenCalled();
      for (const type of ["work_queue_submit", "work_queue_dispatch_next", "work_queue_claim_finish"]) expect(scope.normalizeRuntimeMessage({ type })).toEqual({ type });
    }
  );

  it("keeps legacy processor selection, staged preview, missing messages and ordinary budgets", async () => {
    const message = { type: "noop", message: "Nothing to do", _claimScopeError: "user-defined optional field" };
    writeOutput([message]);
    expect((await processSafeOutput(processorConfig, preview, {})).item).toEqual(message);
    expect((await processSafeOutput({ ...processorConfig, findMultiple: true }, preview, {})).items).toEqual([message]);
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
    expect((await processSafeOutput(processorConfig, preview, {})).reason).toBe("Staged mode - preview generated");
    expect(global.core.summary.write).toHaveBeenCalled();
    delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    const result = await manager.processMessages(new Map(), [message]);
    expect(result.missings.noopMessages).toEqual([{ message: "Nothing to do" }]);
    expect(result.results[0]).toMatchObject({ delegated: true, reason: "Handled by standalone step" });
    const append = vi.fn();
    const handlers = createHandlers({ debug() {} }, append, { add_comment: { max: 1 } });
    handlers.defaultHandler("add_comment")({ body: "First" });
    expect(() => handlers.defaultHandler("add_comment")({ body: "Second" })).toThrow(/limit reached/);
    expect(append).toHaveBeenCalledTimes(1);
  });

  it("runs the real manager and issue handler with legacy authenticated-client and result/manifest semantics", async () => {
    process.env.GH_AW_WORK_QUEUE_ENABLED = "false";
    process.env.GH_AW_WORK_QUEUE_ROLE = "worker";
    const create = vi.fn(async args => ({ data: { number: 19, id: 190, html_url: "https://github.com/owner/repo/issues/19", ...args } }));
    global.github.rest.issues = { create };
    process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({ create_issue: { max: 1, footer: false, "github-token": "configured-test-token" } });
    writeOutput([{ type: "create_issue", title: "Ordinary issue", body: "Body", claim_handle: "arbitrary", _claimScopeError: "ordinary field" }]);
    expect(await manager.main()).toMatchObject({ success: true });
    expect(global.getOctokit).toHaveBeenCalledWith("configured-test-token");
    expect(create).toHaveBeenCalledTimes(1);
    expect(create.mock.calls[0][0]).toMatchObject({ owner: "owner", repo: "repo", title: "Ordinary issue", body: "Body\nRelated to #42" });
    const entry = JSON.parse(fs.readFileSync(constants.MANIFEST_FILE_PATH, "utf8").trim());
    expect(entry).toMatchObject({ type: "create_issue", number: 19, repo: "owner/repo" });
    expect(entry).not.toHaveProperty("claim_handle");
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("preserves the direct ledger_append MCP response rather than returning a handler factory", () => {
    const append = vi.fn();
    const handlers = createHandlers({ debug() {} }, append, {
      ledger_append: {
        ledgers: [
          { name: "history", type: "log" },
          { name: "knowledge", type: "notes" },
        ],
      },
    });
    const result = handlers.ledgerAgentAppendHandler({ ledger: "history", operation: "append", value: "Entry" });
    expect(result).toMatchObject({ content: [{ type: "text", text: JSON.stringify({ result: "success" }) }] });
    expect(handlers.ledgerAgentAppendHandler({ ledger: "knowledge", operation: "note", note: "Use dedicated notes tools" }).isError).toBe(true);
    expect(append).toHaveBeenCalledExactlyOnceWith({ type: "ledger_append", ledger: "history", operation: "append", value: "Entry" });
  });
});

describe("real immutable Claim routing", () => {
  it.each([1, 2])("cannot disable an already trusted %i-Claim execution by changing ambient enablement or role", async count => {
    const fixture = activate(count, () => []);
    const handle = fixture.assignment.claims[0].handle;
    const otherHandle = fixture.assignment.claims[1]?.handle || "foreign";
    const denied = vi.fn(request => ({ authorized: false, claim_handle: request.claim_handle }));
    const append = vi.fn();
    await scope.withClaimExecution({ assignment: fixture.assignment, claim_handle: handle, authorize: denied }, async () => {
      const handlers = createHandlers({ debug: vi.fn() }, append, { ledger_append: { max: 2, ledgers: [{ name: "cache", type: "map" }] } });
      const put = handlers.ledgerBuiltinHandler("map", "put");
      for (const [enabled, role] of [
        ["false", "invalid"],
        ["true", "observer"],
      ]) {
        process.env.GH_AW_WORK_QUEUE_ENABLED = enabled;
        process.env.GH_AW_WORK_QUEUE_ROLE = role;
        if (enabled === "false") expect(scope.readClaimScopeContext()).toBeNull();
        expect(scope.currentClaimHandle()).toBe(handle);
        await expect(scope.assertClaimAuthorized({ type: "noop", ...(count === 1 ? {} : { claim_handle: handle }) })).rejects.toThrow(/not available/);
        expect(() => scope.normalizeRuntimeMessage({ type: "noop", claim_handle: otherHandle })).toThrow(/foreign|escape/);
        expect(() => put({ claim_handle: otherHandle, key: "foreign", value: "rejected" })).toThrow(/foreign|escape/);
        expect(put({ ...(count === 1 ? {} : { claim_handle: handle }), key: role, value: "selected" }).isError).not.toBe(true);
      }
    });
    expect(denied).toHaveBeenCalledTimes(2);
    expect(append).toHaveBeenCalledTimes(2);
    expect(append.mock.calls.every(([entry]) => entry.claim_handle === handle)).toBe(true);
    expect(global.getOctokit).not.toHaveBeenCalled();
  });

  it.each([1, 2])("runs the manager's original %i-Claim assignment with independently scoped noops and artifacts", async count => {
    const fixture = activate(count, members => members.map(member => ({ type: "noop", message: member.handle, ...(count === 1 ? {} : { claim_handle: member.handle }) })));
    const options = managerOptions();
    const result = await manager.main(options);
    expect(result.rejected).toEqual([]);
    expect(result.settlements).toHaveLength(count);
    for (const [index, settlement] of result.settlements.entries()) {
      expect(settlement).toMatchObject({ claim_handle: fixture.assignment.claims[index].handle, success: true });
      expect(settlement.delivery.verification).toBe("unknown");
      expect(fs.existsSync(path.join(scope.claimArtifactPath(options.deliveryArtifactRoot, settlement.claim_handle, fixture.assignment), "delivery-receipt.json"))).toBe(true);
    }
    expect(options.finalizeResults).toHaveBeenCalledTimes(1);
    expect(global.core.setOutput).toHaveBeenCalledWith("work_queue_outcome", "delivery_pending");
    expect(global.getOctokit).not.toHaveBeenCalled();
  });

  it("rejects omitted, malformed, foreign and conflicting selectors without shrinking original multi-Claim membership", async () => {
    const fixture = activate(2, members => [
      { type: "noop", message: "missing selector" },
      { type: "noop", message: "malformed", claim_handle: null },
      { type: "noop", message: "foreign", claim_handle: "foreign" },
      { type: "noop", message: "conflict", claim_handle: members[0].handle, claim_id: members[1].claim_id },
      { type: "noop", message: "valid sibling", claim_handle: members[1].handle },
    ]);
    const result = await manager.main(managerOptions());
    expect(result.rejected).toHaveLength(4);
    expect(result.settlements).toHaveLength(2);
    expect(result.settlements[1]).toMatchObject({ claim_handle: fixture.assignment.claims[1].handle, success: true });
    expect(global.core.setFailed).toHaveBeenCalledWith("4 safe-output message(s) have invalid Claim scope");
  });

  it.each(["cancelled", "result", "not-completed"])("does not provision credentials or handlers for a %s Claim and still processes its sibling", async state => {
    const fixture = activate(2, members => members.map(member => ({ type: "noop", message: member.handle, claim_handle: member.handle })));
    process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({ noop: { "github-token": "private-test-token" } });
    const rejected = fixture.assignment.claims[0].handle;
    const options = managerOptions({
      authorize: vi.fn(request => ({ claim_handle: request.claim_handle, authorized: request.claim_handle !== rejected, ...(request.claim_handle === rejected ? { state, suppressed: state !== "not-completed" } : {}) })),
    });
    const result = await manager.main(options);
    expect(result.settlements[0].success).toBe(state !== "not-completed");
    expect(result.settlements[1].success).toBe(true);
    expect(global.getOctokit).toHaveBeenCalledTimes(1);
    expect(options.authorize.mock.calls.findIndex(([request]) => request.claim_handle === rejected)).toBe(0);
  });

  it("keeps staged Claims read-only, permits pre-Completion preview, and never invokes terminal reconciliation", async () => {
    activate(2, members => members.map(member => ({ type: "noop", message: member.handle, claim_handle: member.handle })));
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
    process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = "invalid configuration must not be loaded";
    const options = managerOptions();
    const result = await manager.main(options);
    expect(result.settlements.every(settlement => settlement.delivery.verification === "staged_preview")).toBe(true);
    expect(options.authorize.mock.calls.every(([request]) => request.requireCompletion === false)).toBe(true);
    expect(options.finalizeResults).not.toHaveBeenCalled();
    expect(global.getOctokit).not.toHaveBeenCalled();
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("scopes MCP limits and review state, including run_id on the actual approve_workflow_run tool", () => {
    const fixture = activate(2, () => []);
    const append = vi.fn();
    const handlers = createHandlers({ debug() {} }, append, { approve_workflow_run: { max: 1 } });
    const approve = handlers.defaultHandler("approve_workflow_run");
    for (const member of fixture.assignment.claims) {
      approve({ claim_handle: member.handle, run_id: "99" });
      expect(() => approve({ claim_handle: member.handle, run_id: "100" })).toThrow(/limit reached/);
    }
    expect(append.mock.calls.map(([entry]) => entry)).toEqual(fixture.assignment.claims.map(member => ({ type: "approve_workflow_run", claim_handle: member.handle, run_id: "99" })));
    expect(() => approve({ run_id: "99" })).toThrow(/multi-Claim/);
    expect(() => handlers.defaultHandler("noop")({ claim_handle: fixture.assignment.claims[0].handle, run_id: "99" })).toThrow(/trusted authority/);
    handlers.createPullRequestReviewCommentHandler({ claim_handle: fixture.assignment.claims[0].handle, body: "First Claim review", path: "file.js", line: 1 });
    expect(() => handlers.submitPullRequestReviewHandler({ claim_handle: fixture.assignment.claims[1].handle })).toThrow(/contentless review/);
    handlers.submitPullRequestReviewHandler({ claim_handle: fixture.assignment.claims[0].handle });
    expect(() => handlers.submitPullRequestReviewHandler({ claim_handle: fixture.assignment.claims[0].handle })).toThrow(/contentless review/);
  });

  it("gives direct ledger_append and built-in ledger tools the correct Claim and independent limits", () => {
    const original = activate(2, () => []).assignment;
    const append = vi.fn();
    const handlers = createHandlers({ debug() {} }, append, {
      ledger_append: {
        max: 1,
        ledgers: [
          { name: "history", type: "log" },
          { name: "cache", type: "map" },
        ],
      },
    });
    for (const member of original.claims) {
      const message = { claim_handle: member.handle, ledger: "history", operation: "append", value: member.handle };
      expect(handlers.ledgerAgentAppendHandler(message).isError).not.toBe(true);
      expect(() => handlers.ledgerAgentAppendHandler(message)).toThrow(/limit reached/);
      expect(() => handlers.ledgerBuiltinHandler("map", "put")({ claim_handle: member.handle, ledger: "cache", key: "key", value: "value" })).toThrow(/limit reached/);
    }
    expect(append.mock.calls.map(([entry]) => entry.claim_handle)).toEqual(original.claims.map(member => member.handle));
    expect(() => handlers.ledgerAgentAppendHandler({ ledger: "history", operation: "append", value: "Ambiguous" })).toThrow(/multi-Claim/);
  });

  it("normalizes singleton MCP append scope and strips queue fields only from scoped action inputs", async () => {
    activate(1, () => []);
    const output = path.join(root, "singleton.jsonl");
    fs.writeFileSync(output, "");
    const handlers = createHandlers({ debug() {} }, createAppendFunction(output));
    handlers.defaultHandler("approve_workflow_run")({ run_id: "99" });
    const message = JSON.parse(fs.readFileSync(output, "utf8"));
    expect(message).toMatchObject({ type: "approve_workflow_run", run_id: "99" });
    createAppendFunction(output)({ type: "approve-workflow-run", run_id: "100" });
    const dashed = JSON.parse(fs.readFileSync(output, "utf8").trim().split("\n")[1]);
    expect(dashed).toMatchObject({ type: "approve_workflow_run", claim_handle: message.claim_handle, run_id: "100" });
    const original = scope.readClaimScopeContext().assignment;
    await scope.withClaimExecution({ assignment: original, claim_handle: original.claims[0].handle, authorize }, async () => {
      const action = await actionFactory({ action_name: "scoped" });
      const result = await action({ ...message, claim_id: original.claims[0].claim_id, work_id: original.claims[0].work_id, title: "Scoped" }, {});
      expect(JSON.parse(result.payload)).toEqual({ run_id: "99", title: "Scoped" });
    });
  });

  it("keeps processor previews pre-Completion but authorizes ordinary processing and rejects invalid scope", async () => {
    const checks = vi.fn(authorize);
    const original = activate(1, () => [{ type: "noop", message: "Preview" }]).assignment;
    const handle = original.claims[0].handle;
    await scope.withClaimExecution({ assignment: original, claim_handle: handle, authorize: checks }, async () => {
      expect((await processSafeOutput(processorConfig, preview, { target: "42" })).item.claim_handle).toBe(handle);
      expect(checks.mock.calls[0][0].requireCompletion).toBe(true);
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      expect((await processSafeOutput(processorConfig, preview, {})).reason).toBe("Staged mode - preview generated");
      expect(checks.mock.calls[1][0].requireCompletion).toBe(false);
      writeOutput([{ type: "noop", message: "Foreign", claim_handle: "h2" }]);
      expect((await processSafeOutput(processorConfig, preview, {})).success).toBe(false);
      expect(checks).toHaveBeenCalledTimes(2);
    });
  });

  it("rejects retained handler factories from a different dispatch even when their local handle is identical", async () => {
    const original = assignment();
    let handlers;
    await scope.withClaimExecution({ assignment: original, claim_handle: "h1", authorize }, async () => {
      handlers = await manager.loadHandlers({ noop: { max: 1 } }, undefined, []);
    });
    const other = structuredClone(original);
    other.dispatch_id = "another-dispatch";
    other.claims[0].claim_id = "another-claim";
    await scope.withClaimExecution({ assignment: other, claim_handle: "h1", authorize }, async () => {
      const result = await manager.processMessages(handlers, [{ type: "noop", message: "Not the factory's Claim" }]);
      expect(result.results[0]).toMatchObject({ success: false });
      expect(result.results[0].error).toMatch(/original immutable Claim identity/);
    });
  });

  it("does not construct replaced builtin clients before prepared adapter authorization, while retaining disabled loading", async () => {
    const config = {
      create_issue: { max: 1, "github-token": "prepared-test-token" },
      claim_adapters: { create_issue: { mode: "prepared", "effect-type": "create_issue", "target-repo": "owner/repo", "field-map": { title: "title" } } },
    };
    const ordinary = await manager.loadHandlers(config, undefined, []);
    expect(ordinary.has("create_issue")).toBe(true);
    expect(global.getOctokit).toHaveBeenCalledWith("prepared-test-token");
    global.getOctokit.mockClear();
    const original = activate(1, () => []).assignment;
    const denied = vi.fn(request => ({ authorized: false, claim_handle: request.claim_handle }));
    await scope.withClaimExecution({ assignment: original, claim_handle: original.claims[0].handle, authorize: denied }, async () => {
      const handlers = await manager.loadHandlers(config, undefined, []);
      expect(handlers.has("create_issue")).toBe(true);
      expect(global.getOctokit).not.toHaveBeenCalled();
      await expect(handlers.get("create_issue")({ type: "create_issue", title: "Rejected" })).rejects.toThrow(/not available/);
      expect(global.getOctokit).not.toHaveBeenCalled();
    });
  });

  it("rejects ordinary builtin/action factories borrowed by a later Claim before exporting payloads or using raw clients", async () => {
    const create = vi.fn(async args => ({ data: { ...args, id: 19, number: 19, html_url: "https://github.com/owner/repo/issues/19" } }));
    global.github.rest.issues = { create };
    const handlers = await manager.loadHandlers({ create_issue: { footer: false } }, undefined, []);
    const action = await actionFactory({ action_name: "ordinary" });
    const original = activate(1, () => []).assignment;
    await scope.withClaimExecution({ assignment: original, claim_handle: original.claims[0].handle, authorize }, async () => {
      const result = await manager.processMessages(handlers, [{ type: "create_issue", title: "Borrowed", body: "Body" }]);
      expect(result.results[0]).toMatchObject({ success: false });
      expect(result.results[0].error).toMatch(/factory context/);
      expect(create).not.toHaveBeenCalled();
      await expect(action({ type: "ordinary", title: "Borrowed" }, {})).rejects.toThrow(/original Claim factory/);
      expect(global.core.setOutput).not.toHaveBeenCalledWith("action_ordinary_payload", expect.anything());
    });
  });

  it("isolates real handler budgets, API effect receipts, temporary IDs and errors across Claims", async () => {
    const original = assignment(2);
    global.github.rest.repos = { get: vi.fn(async () => ({ data: { full_name: "owner/repo", id: 7 } })) };
    global.github.rest.issues = { create: vi.fn(async args => ({ data: { ...args, id: 19, number: 19, html_url: "https://github.com/owner/repo/issues/19" } })) };
    const source = global.github;
    const effects = [[], []];
    const results = [];
    for (const [index, member] of original.claims.entries()) {
      results.push(
        await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize, effects: effects[index] }, () =>
          withClaimEffectClients({ claim_handle: member.handle, authorize, authorizeGithub: source }, async () => {
            const handlers = await manager.loadHandlers({ create_issue: { max: 1, footer: false } }, undefined, []);
            return manager.processMessages(handlers, [
              { type: "create_issue", title: "Scoped", body: "Body", temporary_id: "aw_shared", claim_handle: member.handle },
              { type: "create_issue", title: "Over budget", body: "Body", claim_handle: member.handle },
            ]);
          })
        )
      );
    }
    expect(source.rest.issues.create).toHaveBeenCalledTimes(2);
    expect(results.every(result => result.results[0].success && !result.results[1].success)).toBe(true);
    expect(results.map(result => result.temporaryIdMap.aw_shared.number)).toEqual([19, 19]);
    expect(effects.map(entries => entries.length)).toEqual([1, 1]);
    expect(effects.map(entries => entries[0].claim_handle)).toEqual(["h1", "h2"]);
    expect(global.github).toBe(source);
  });

  it("contains a failed native API effect to its Claim while the actual manager executes a sibling", async () => {
    const fixture = activate(2, members => members.map((member, index) => ({ type: "create_issue", claim_handle: member.handle, title: index === 0 ? "First" : "Second", body: "Body" })), {
      configureWork: work => (work.payload.effect_contract = { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1 }] }),
    });
    process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({ create_issue: { max: 1, footer: false } });
    global.github.rest.repos = { get: vi.fn(async () => ({ data: { full_name: "owner/repo", id: 7 } })) };
    global.github.rest.issues = {
      create: vi.fn(async args => {
        if (args.title === "First") throw Object.assign(new Error("First Claim API failure"), { status: 422 });
        return { data: { ...args, id: 19, number: 19, html_url: "https://github.com/owner/repo/issues/19" } };
      }),
    };
    const create = global.github.rest.issues.create;
    const result = await manager.main(managerOptions());
    expect(create).toHaveBeenCalledTimes(2);
    expect(result.settlements[0]).toMatchObject({ claim_handle: fixture.assignment.claims[0].handle, success: false });
    expect(result.settlements[1]).toMatchObject({ claim_handle: fixture.assignment.claims[1].handle, success: true });
    expect(result.settlements.every(settlement => settlement.delivery.verification === "unknown")).toBe(true);
    expect(global.core.setFailed).toHaveBeenCalledWith("One or more Claim-scoped safe-output passes failed; independently valid siblings were reconciled");
  });

  it("never allows manifest filename spelling or retained logger state to escape its original Claim", async () => {
    const original = assignment(2);
    const filename = path.join(root, "ordinary", "claims", "items.jsonl");
    let logger;
    await scope.withClaimExecution({ assignment: original, claim_handle: "h1" }, () => {
      manifest.ensureManifestExists(filename);
      expect(fs.existsSync(filename)).toBe(false);
      const destination = path.join(scope.claimArtifactPath(path.dirname(filename), "h1"), "items.jsonl");
      expect(fs.existsSync(destination)).toBe(true);
      logger = manifest.createManifestLogger(filename);
      logger({ type: "create_issue", number: 1 });
      expect(JSON.parse(fs.readFileSync(destination, "utf8")).claim_handle).toBe("h1");
    });
    await scope.withClaimExecution({ assignment: original, claim_handle: "h2" }, () => {
      expect(() => logger({ type: "create_issue", number: 2 })).toThrow(/original immutable Claim identity/);
    });
  });

  it("rejects mutated stored membership before native worker binding can be borrowed", () => {
    const fixture = queueFixture({ count: 2, bound: true });
    const state = fixture.readWorkQueueLog;
    expect(typeof state).toBe("function");
    const projection = require("./work_queue_replay.cjs").replayTransactions(fixture.transactions);
    expect(validateStoredAssignment(projection, fixture.assignment).assignment).toEqual(fixture.assignment);
    const narrowed = structuredClone(fixture.assignment);
    narrowed.claims.pop();
    expect(() => validateStoredAssignment(projection, narrowed)).toThrow(/assignment_mismatch/);
  });
});

describe("native adapter input boundaries", () => {
  const restAdapter = {
    "target-repo": "owner/repo",
    "field-map": { title: "title" },
    request: { method: "POST", route: "/repos/{owner}/{repo}/issues", permission: "issues" },
    verifier: { route: "/repos/{owner}/{repo}/issues/{receipt_id}", fields: { title: "title" }, "resource-kind": "issue", "number-field": "number" },
  };

  it("rejects missing REST inputs, oversized payloads and foreign repositories before native reads/writes", async () => {
    const source = { request: vi.fn(async () => ({ data: { id: 1 } })) };
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize }, async () => {
      const handler = createRestEffectHandler(restAdapter, source);
      await expect(handler({ type: "custom" })).rejects.toThrow(/missing a declared input field/);
      await expect(handler({ type: "custom", title: "x".repeat(1024 * 1024 + 1) })).rejects.toThrow(/byte ceiling/);
      await expect(handler({ type: "custom", title: "Title", repo: "foreign/repo" })).rejects.toThrow(/conflicts/);
      expect(source.request).not.toHaveBeenCalled();
      expect((await handler({ type: "custom", title: "Title" })).success).toBe(true);
      expect(source.request).toHaveBeenCalledWith("POST /repos/owner/repo/issues", { title: "Title" });
    });
  });

  it("preserves REST and GraphQL staged behavior without touching client credentials", async () => {
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
    const source = { request: vi.fn(), graphql: vi.fn() };
    const graphqlAdapter = {
      "target-repo": "owner/repo",
      "field-map": { title: "title" },
      graphql: {
        mutation: "createIssue",
        "input-type": "CreateIssueInput",
        "response-field": "issue",
        "resource-type": "Issue",
        "resource-kind": "issue",
        "repository-field": "repository.nameWithOwner",
        "repository-input": "repositoryId",
        permission: "issues",
        fields: { title: "title" },
      },
    };
    const checks = vi.fn(authorize);
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize: checks }, async () => {
      for (const factory of [createRestEffectHandler, createGraphqlEffectHandler]) {
        expect(await factory(factory === createRestEffectHandler ? restAdapter : graphqlAdapter, source)({ type: "custom", title: "Title" })).toMatchObject({ success: true, staged: true, claim_handle: "h1" });
      }
    });
    expect(checks.mock.calls.every(([request]) => request.requireCompletion === false)).toBe(true);
    expect(source.request).not.toHaveBeenCalled();
    expect(source.graphql).not.toHaveBeenCalled();
  });
});

describe("tool schema gating", () => {
  it.each([undefined, false, true])("injects the optional immutable selector only for work_queue_scoped=%s", scoped => {
    const source = path.join(root, "tools-source.json");
    const config = path.join(root, "tools-config.json");
    const meta = path.join(root, "tools-meta.json");
    const output = path.join(root, "tools.json");
    const builtin = { name: "noop", description: "Noop", inputSchema: { type: "object", additionalProperties: false, properties: { message: { type: "string" } }, required: ["message"] } };
    const dynamic = { name: "custom", description: "Custom", inputSchema: { type: "object", additionalProperties: false, properties: { title: { type: "string" } }, required: ["title"] } };
    fs.writeFileSync(source, JSON.stringify([builtin]));
    fs.writeFileSync(config, JSON.stringify({ noop: {} }));
    fs.writeFileSync(meta, JSON.stringify({ dynamic_tools: [dynamic], ...(scoped === undefined ? {} : { work_queue_scoped: scoped }) }));
    execFileSync(process.execPath, [path.join(import.meta.dirname, "generate_safe_outputs_tools.cjs")], {
      env: { ...process.env, GH_AW_SAFE_OUTPUTS_TOOLS_SOURCE_PATH: source, GH_AW_SAFE_OUTPUTS_CONFIG_PATH: config, GH_AW_SAFE_OUTPUTS_TOOLS_META_PATH: meta, GH_AW_SAFE_OUTPUTS_TOOLS_PATH: output },
      stdio: "pipe",
    });
    const tools = JSON.parse(fs.readFileSync(output, "utf8"));
    for (const tool of tools) {
      expect(Object.hasOwn(tool.inputSchema.properties, "claim_handle")).toBe(scoped === true);
      expect(tool.inputSchema.required).not.toContain("claim_handle");
      expect(tool.inputSchema.additionalProperties).toBe(false);
    }
  });

  it("preserves a legacy custom claim_handle input but refuses to overwrite it in queue schemas", async () => {
    const source = path.join(root, "reserved-source.json");
    const config = path.join(root, "reserved-config.json");
    const meta = path.join(root, "reserved-meta.json");
    const output = path.join(root, "reserved-tools.json");
    fs.writeFileSync(source, "[]");
    fs.writeFileSync(config, "{}");
    const dynamic = { name: "custom", inputSchema: { type: "object", properties: { claim_handle: { type: "integer" } } } };
    const env = { ...process.env, GH_AW_SAFE_OUTPUTS_TOOLS_SOURCE_PATH: source, GH_AW_SAFE_OUTPUTS_CONFIG_PATH: config, GH_AW_SAFE_OUTPUTS_TOOLS_META_PATH: meta, GH_AW_SAFE_OUTPUTS_TOOLS_PATH: output };
    const run = () => execFileSync(process.execPath, [path.join(import.meta.dirname, "generate_safe_outputs_tools.cjs")], { env, stdio: "pipe" });
    fs.writeFileSync(meta, JSON.stringify({ dynamic_tools: [dynamic] }));
    run();
    expect(JSON.parse(fs.readFileSync(output, "utf8"))[0].inputSchema.properties.claim_handle).toEqual({ type: "integer" });
    fs.writeFileSync(meta, JSON.stringify({ work_queue_scoped: true, dynamic_tools: [dynamic] }));
    Object.assign(process.env, env);
    await expect(require("./generate_safe_outputs_tools.cjs").main()).rejects.toThrow(/claim_handle is reserved/);
  });
});
