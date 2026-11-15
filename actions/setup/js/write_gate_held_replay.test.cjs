// @ts-check

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import { canonicalPendingJSON, inputDigest, itemDigest, pendingContext, pendingEnvironment } from "./pending_writes.cjs";
import { readInput, sha256 } from "./pending_writes_files.cjs";
import { buildWriteDecisionInventory, writeDecisionInventory } from "./write_gate_inventory.cjs";
import { useExecutionProvenanceFixture } from "./test_execution_provenance.js";

useExecutionProvenanceFixture();
// Exercise the production CommonJS singleton scopes, not Vitest's ESM copies.
const require = createRequire(import.meta.url);
const { getReplayPolicy } = require("./apply_safe_outputs_replay.cjs");
const { authorizeHeldReplay, withHeldReplayAuthorization } = require("./write_gate_replay_approval.cjs");
const { loadHandlers, processMessages } = require("./safe_output_handler_manager.cjs");
const { createManifestLogger } = require("./safe_output_manifest.cjs");
const { writePendingInputSnapshot, restorePendingInputs } = require("./pending_write_inputs.cjs");
const { executeHeldReplayFromEnv } = require("./write_gate_replay_driver.cjs");

describe("source-bound approved held replay", () => {
  let directory;
  let originalContext;
  let api;
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "held-replay-proof-"));
    vi.stubEnv("RUNNER_TEMP", directory);
    originalContext = { repo: { owner: "test-owner", repo: "test-repo" }, eventName: "issues", payload: { issue: { number: 42 } }, actor: "original-actor", runId: 123, serverUrl: "https://github.com" };
    vi.stubGlobal("context", { ...originalContext, runId: 999, payload: { issue: { number: 999 } } });
    vi.stubEnv("GITHUB_RUN_ID", "999");
    vi.stubEnv("GITHUB_JOB", "apply_held_safe_outputs");
    vi.stubEnv("GH_AW_JOB_CHECK_RUN_ID", "456");
    vi.stubEnv("GH_AW_WORKFLOW_NAME", "Original workflow");
    vi.stubEnv("GH_AW_TARGET_REPO_SLUG", "");
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() });
    api = {
      create: vi.fn().mockResolvedValue({ data: { number: 101, html_url: "https://github.com/test-owner/test-repo/issues/101", title: "Created", body: "Created", state: "open" } }),
      createComment: vi.fn().mockResolvedValue({ data: { id: 3, html_url: "https://github.com/test-owner/test-repo/issues/42#issuecomment-3" } }),
      get: vi.fn().mockResolvedValue({ data: { number: 42, title: "Original target", body: "Original target", state: "open" } }),
    };
    vi.stubGlobal("github", {
      rest: {
        issues: api,
        repos: {
          getContent: vi
            .fn()
            .mockResolvedValue({ data: { type: "file", encoding: "base64", content: Buffer.from('# gh-aw-manifest: {"version":1,"write_gate":{"version":1,"record_job":"conclusion","artifact_immutable":true}}\n').toString("base64") } }),
        },
        actions: {
          getWorkflowRun: vi.fn().mockResolvedValue({ data: { status: "completed", event: "issues", run_attempt: 1, head_sha: "a".repeat(40), path: ".github/workflows/test.lock.yml" } }),
          listJobsForWorkflowRunAttempt: vi.fn(),
          getReviewsForRun: vi.fn().mockResolvedValue({ data: [{ state: "approved", user: { id: 17, login: "actual-reviewer", type: "User" }, environments: [{ id: 7, name: "production" }], comment: "Reviewed held items" }] }),
        },
      },
      paginate: vi.fn().mockImplementation(async (_method, parameters) =>
        parameters.run_id === 123
          ? [{ name: "conclusion", status: "completed", steps: ["Record write gate verdict", "Upload write gate verdict"].map(name => ({ name, status: "completed", conclusion: "success" })) }]
          : [
              {
                name: "Apply approved held safe outputs",
                status: "in_progress",
                check_run_url: "https://api.github.com/repos/test-owner/test-repo/check-runs/456",
                steps: [{ name: "Apply approved held safe outputs", status: "in_progress" }],
              },
            ]
      ),
    });
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  async function prepareProof(messages, decisions = messages.map(() => "hold")) {
    const handlerConfig = Object.fromEntries([...new Set(messages.map(message => message.type))].map(type => [type, type === "add_comment" ? { target: "triggering" } : { max: 10 }]));
    const gateConfig = { review_repo: "", allowed_repos: "", approval_environment: "production" };
    const sourcePath = path.join(directory, "gh-aw", "source", "agent_output.json");
    fs.mkdirSync(path.dirname(sourcePath), { recursive: true });
    fs.writeFileSync(sourcePath, JSON.stringify({ items: messages }));
    const source = readInput(sourcePath);
    const run = { repository: "test-owner/test-repo", workflow_ref: "test-owner/test-repo/.github/workflows/test.lock.yml@refs/heads/main", workflow_sha: "b".repeat(40), run_id: "123", run_attempt: "1", event: "issues" };
    const bindings = [
      { id: "agent_output", kind: "file", path: sourcePath },
      { id: "handler_config", kind: "value", value: handlerConfig },
      { id: "gate_config", kind: "value", value: gateConfig },
      { id: "context", kind: "value", value: pendingContext(originalContext) },
      { id: "environment", kind: "value", value: pendingEnvironment() },
    ];
    const items = messages.map((message, index) => {
      const item = {
        output_id: message.output_id,
        type: message.type,
        target: { repository: run.repository, ...(message.type === "add_comment" ? { kind: "issue", number: 42 } : {}) },
        depends_on: message.depends_on || [],
        settings: { staged: false, sha256: inputDigest(handlerConfig[message.type]), mention_allowances: [] },
        free_text: [],
        input_sha256: inputDigest(message),
        complete: true,
        reason_category: "ready",
      };
      return { ...item, item_sha256: itemDigest(item) };
    });
    const manifest = {
      schema: "gh-aw/pending-writes/v1",
      run,
      detection: { enabled: false, mode: "disabled", conclusion: "success" },
      complete: true,
      inputs: [
        { id: "agent_output", kind: "file", length: source.length, sha256: source.sha256, mode: source.mode },
        { id: "handler_config", kind: "value", sha256: inputDigest(handlerConfig) },
        { id: "gate_config", kind: "value", sha256: inputDigest(gateConfig) },
        { id: "context", kind: "value", sha256: inputDigest(pendingContext(originalContext)) },
        { id: "environment", kind: "value", sha256: inputDigest(pendingEnvironment()) },
      ],
      items,
    };
    const manifestPath = path.join(directory, "pending-writes.json");
    fs.writeFileSync(manifestPath, canonicalPendingJSON(manifest));
    const manifestSha = readInput(manifestPath).sha256;
    const decisionsPath = path.join(directory, "write-decisions.normalized.json");
    const inventory = buildWriteDecisionInventory(
      manifest,
      manifestSha,
      { decision: "partial", reason: "items_checked", items: items.map((item, index) => ({ output_id: item.output_id, decision: decisions[index], reason: "approval_required" })) },
      vi.fn()
    );
    const decisionSha = writeDecisionInventory(decisionsPath, inventory);
    const snapshotDirectory = path.join(directory, "original-inputs");
    writePendingInputSnapshot({ directory: snapshotDirectory, manifest, bindings, handlerConfig, gateConfig, context: originalContext, runnerTemp: directory });
    const snapshotPath = path.join(snapshotDirectory, "pending-write-inputs.json");
    const itemsPath = path.join(directory, "original-items.jsonl");
    fs.writeFileSync(
      itemsPath,
      items
        .map((item, index) =>
          JSON.stringify({
            type: item.type,
            output_id: item.output_id,
            item_sha256: item.item_sha256,
            status: decisions[index] === "apply" ? "success" : "skipped",
            skipped: decisions[index] !== "apply",
            reason: decisions[index] === "hold" ? "gate_held" : "gate_dropped",
            gate_decision: decisions[index],
            ...(decisions[index] === "apply" && messages[index].temporary_id ? { temporaryId: messages[index].temporary_id, number: 55, repo: run.repository } : {}),
          })
        )
        .join("\n") + "\n"
    );
    const record = {
      version: 1,
      repository: run.repository,
      run_id: run.run_id,
      run_attempt: run.run_attempt,
      head_sha: "a".repeat(40),
      workflow_sha: run.workflow_sha,
      workflow_ref: run.workflow_ref,
      safe_outputs_result: "success",
      verdict: "partial",
      reason: "items_checked",
      pending_writes_sha256: manifestSha,
      write_decisions_sha256: decisionSha,
      pending_write_inputs_sha256: readInput(snapshotPath).sha256,
      safe_output_items_sha256: readInput(itemsPath).sha256,
    };
    vi.stubGlobal("exec", {
      exec: vi.fn().mockImplementation(async (_command, arguments_) => {
        const destination = arguments_[arguments_.indexOf("--dir") + 1];
        const artifact = arguments_[arguments_.indexOf("--name") + 1];
        if (artifact === "write-gate") fs.writeFileSync(path.join(destination, "write_gate.json"), JSON.stringify(record));
        else if (artifact === "pending-writes") fs.copyFileSync(manifestPath, path.join(destination, "pending-writes.json"));
        else if (artifact === "write-decisions") fs.copyFileSync(decisionsPath, path.join(destination, "write-decisions.normalized.json"));
        else if (artifact === "pending-write-inputs") fs.cpSync(snapshotDirectory, destination, { recursive: true });
        else if (artifact === "safe-outputs-items") fs.copyFileSync(itemsPath, path.join(destination, "safe-output-items.jsonl"));
        else throw new Error("Unexpected test artifact");
        return 0;
      }),
    });
    const policy = await getReplayPolicy("123", "test-owner", "test-repo", true);
    return {
      policy,
      manifestPath,
      decisionsPath,
      snapshotPath,
      itemsPath,
      bind: () => {
        vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "true");
        vi.stubEnv("GH_AW_PENDING_WRITES", manifestPath);
        vi.stubEnv("GH_AW_PENDING_WRITES_SHA256", manifestSha);
        vi.stubEnv("GH_AW_PENDING_WRITES_BINDINGS", JSON.stringify(bindings));
        vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify(handlerConfig));
        vi.stubEnv("GH_AW_WRITE_GATE_DECISIONS", decisionsPath);
        vi.stubEnv("GH_AW_WRITE_GATE_DECISIONS_SHA256", decisionSha);
        vi.stubEnv("GH_AW_WRITE_GATE_REVIEW_REPO", gateConfig.review_repo);
        vi.stubEnv("GH_AW_WRITE_GATE_ALLOWED_REPOS", gateConfig.allowed_repos);
        vi.stubEnv("GH_AW_WRITE_GATE_APPROVAL_ENVIRONMENT", gateConfig.approval_environment);
        vi.stubGlobal("context", originalContext);
      },
      handlerConfig,
      record,
    };
  }

  it("requires actual approval and applies only held items to the original target, not the label issue", async () => {
    const messages = [
      { type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Held comment" },
      { type: "add_comment", output_id: "so_bbbbbbbbbbbb", body: "Dropped comment" },
    ];
    const proof = await prepareProof(messages, ["hold", "drop"]);
    const authorization = await authorizeHeldReplay(proof);
    proof.bind();
    const handlers = await loadHandlers(proof.handlerConfig);
    const currentItems = path.join(directory, "replay-items.jsonl");
    const result = await withHeldReplayAuthorization(authorization, () => processMessages(handlers, messages, createManifestLogger(currentItems)));
    expect(api.createComment).toHaveBeenCalledTimes(1);
    expect(api.createComment.mock.calls[0][0]).toMatchObject({ issue_number: 42 });
    expect(api.createComment.mock.calls[0][0].body).toContain("/actions/runs/999");
    expect(result.results[0]).toMatchObject({ gate_decision: "apply", approvers: ["actual-reviewer"], replay_source_run_id: "123" });
    expect(result.results[1]).toMatchObject({ gate_decision: "drop", skipped: true });
    const row = JSON.parse(fs.readFileSync(currentItems, "utf8").split("\n")[0]);
    expect(row.approvers).toEqual(["actual-reviewer"]);
  });

  it("unapproved replay and altered held digests make no writes", async () => {
    const proof = await prepareProof([{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Held" }]);
    global.github.rest.actions.getReviewsForRun.mockResolvedValueOnce({ data: [] });
    await expect(authorizeHeldReplay(proof)).rejects.toThrow("approved environment review");
    fs.appendFileSync(proof.manifestPath, "\n");
    await expect(authorizeHeldReplay(proof)).rejects.toThrow("inputs_changed");
    expect(api.createComment).not.toHaveBeenCalled();
    expect(api.create).not.toHaveBeenCalled();
  });

  it("executes the reviewed driver using the actual original-input restorer and frozen context", async () => {
    const messages = [{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Held comment" }];
    const proof = await prepareProof(messages);
    vi.stubEnv("GH_AW_RUN_URL", "123");
    for (const pin of ["pending_writes_sha256", "write_decisions_sha256", "pending_write_inputs_sha256", "safe_output_items_sha256"]) vi.stubEnv(`GH_AW_REPLAY_${pin.toUpperCase()}`, proof.record[pin]);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_RUN_ATTEMPT", proof.record.run_attempt);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_WORKFLOW_SHA", proof.record.workflow_sha);
    await executeHeldReplayFromEnv({
      restorePendingInputs,
      runHandlers: async () => {
        const handlers = await loadHandlers(proof.handlerConfig);
        const result = await processMessages(handlers, messages);
        return { success: result.results.every(row => row.success || row.skipped) };
      },
    });
    expect(api.createComment).toHaveBeenCalledOnce();
    expect(api.createComment.mock.calls[0][0]).toMatchObject({ issue_number: 42 });
    expect(global.context.payload.issue.number).toBe(999);
  });

  it("applies held temporary-ID dependencies in order without releasing unapplied producers", async () => {
    const messages = [
      { type: "create_issue", output_id: "so_aaaaaaaaaaaa", temporary_id: "aw_abc", title: "Parent", body: "Parent" },
      { type: "add_comment", output_id: "so_bbbbbbbbbbbb", depends_on: ["so_aaaaaaaaaaaa"], body: "Related to #aw_abc" },
    ];
    const proof = await prepareProof(messages);
    const authorization = await authorizeHeldReplay(proof);
    proof.bind();
    const handlers = await loadHandlers(proof.handlerConfig);
    const result = await withHeldReplayAuthorization(authorization, () => processMessages(handlers, messages));
    expect(result.results.map(row => row.success)).toEqual([true, true]);
    expect(api.create).toHaveBeenCalledOnce();
    expect(api.createComment).toHaveBeenCalledOnce();
    expect(api.createComment.mock.calls[0][0].body).toContain("101");
  });

  it("uses pinned original-applied dependency evidence without replaying the producer", async () => {
    const messages = [
      { type: "create_issue", output_id: "so_aaaaaaaaaaaa", temporary_id: "aw_abc", title: "Parent", body: "Parent" },
      { type: "add_comment", output_id: "so_bbbbbbbbbbbb", depends_on: ["so_aaaaaaaaaaaa"], body: "Related to #aw_abc" },
    ];
    const proof = await prepareProof(messages, ["apply", "hold"]);
    const authorization = await authorizeHeldReplay(proof);
    proof.bind();
    const handlers = await loadHandlers(proof.handlerConfig);
    const result = await withHeldReplayAuthorization(authorization, () => processMessages(handlers, messages));
    expect(api.create).not.toHaveBeenCalled();
    expect(api.createComment).toHaveBeenCalledOnce();
    expect(result.results.find(row => row.output_id === messages[1].output_id).success).toBe(true);
    expect(api.createComment.mock.calls[0][0].body).toContain("55");
  });
});
