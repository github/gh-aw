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
          get: vi.fn().mockResolvedValue({ data: { has_issues: true } }),
          getContent: vi
            .fn()
            .mockResolvedValue({ data: { type: "file", encoding: "base64", content: Buffer.from('# gh-aw-manifest: {"version":1,"write_gate":{"version":1,"record_job":"conclusion","artifact_immutable":true}}\n').toString("base64") } }),
        },
        actions: {
          createWorkflowDispatch: vi.fn().mockResolvedValue({ data: {} }),
          getWorkflowRun: vi.fn().mockResolvedValue({ data: { id: 123, created_at: "2026-11-15T12:00:00Z", status: "completed", event: "issues", run_attempt: 1, head_sha: "a".repeat(40), path: ".github/workflows/test.lock.yml" } }),
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

  async function prepareProof(messages, decisions = messages.map(() => "hold"), publication = null, productionConfig = null, productionPublications = []) {
    const handlerConfig = productionConfig || Object.fromEntries([...new Set(messages.map(message => message.type))].map(type => [type, type === "add_comment" ? { target: "triggering" } : { max: 10 }]));
    const gateConfig = { review_repo: publication?.reviewRepo || "", allowed_repos: publication?.reviewRepo || "", approval_environment: "production" };
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
      { id: "publication_invocations", kind: "value", value: [] },
      { id: "dispatch_contexts", kind: "value", value: {} },
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
        { id: "publication_invocations", kind: "value", sha256: inputDigest([]) },
        { id: "dispatch_contexts", kind: "value", sha256: inputDigest({}) },
      ],
      items,
    };
    let publicationInputs = [];
    if (publication) {
      const memory = path.join(directory, "gh-aw", "memory");
      fs.mkdirSync(memory, { recursive: true });
      fs.writeFileSync(path.join(memory, "notes.json"), '{"kept":true}');
      const files = require("./pending_writes_files.cjs").describeDirectory(memory);
      const invocation = {
        module: publication.module || "push_repo_memory.cjs",
        env: { ARTIFACT_DIR: memory, MEMORY_ID: "default", TARGET_REPO: run.repository, BRANCH_NAME: "memory/original", MAX_FILE_COUNT: "17", FILE_GLOB_FILTER: "*.json" },
        input_binding_ids: ["repo_memory:default"],
        path_env: { ARTIFACT_DIR: "repo_memory:default" },
      };
      bindings.find(binding => binding.id === "publication_invocations").value = [{ job: "push_repo_memory", id: "default", ...invocation }];
      manifest.inputs.find(input => input.id === "publication_invocations").sha256 = inputDigest(bindings.find(binding => binding.id === "publication_invocations").value);
      bindings.push({ id: "repo_memory:default", kind: "directory", path: memory });
      manifest.inputs.push({ id: "repo_memory:default", kind: "directory", files });
      const item = {
        output_id: "job_0123456789abcdef",
        type: "repo_memory",
        target: { repository: run.repository, job: "push_repo_memory", branch: "memory/original" },
        depends_on: publication.depends_on || [],
        settings: { staged: false, sha256: inputDigest({}) },
        free_text: [],
        input_sha256: inputDigest({ id: "repo_memory:default", kind: "directory", files }),
        publication: {
          id: "default",
          sha256: inputDigest({ id: "repo_memory:default", kind: "directory", files }),
          invocation: {
            module: invocation.module,
            sha256: inputDigest(invocation),
            input_binding_ids: invocation.input_binding_ids,
            env: Object.entries(invocation.env).map(([name, value]) => ({
              name,
              length: Buffer.byteLength(value),
              sha256: require("./pending_writes_files.cjs").sha256(value),
              ...(name === "ARTIFACT_DIR" ? { binding_id: "repo_memory:default" } : {}),
            })),
          },
        },
        complete: true,
        reason_category: "ready",
      };
      items.push({ ...item, item_sha256: itemDigest(item) });
      decisions.push(publication.decision || "hold");
    }
    if (productionConfig) {
      const currentContext = global.context;
      const runId = process.env.GITHUB_RUN_ID;
      const workflowRef = process.env.GITHUB_WORKFLOW_REF;
      try {
        global.context = originalContext;
        process.env.GITHUB_RUN_ID = run.run_id;
        process.env.GITHUB_WORKFLOW_REF = run.workflow_ref;
        const prepared = await require("./pending_writes_plan.cjs").buildPendingWrites({ agentOutputPath: sourcePath, handlerConfig, gateConfig, context: originalContext, run, bindings: [], publications: productionPublications });
        bindings.splice(0, bindings.length, ...prepared.bindings);
        items.splice(0, items.length, ...prepared.manifest.items);
        Object.assign(manifest, prepared.manifest);
        publicationInputs = prepared.publicationInputs;
      } finally {
        global.context = currentContext;
        if (runId === undefined) delete process.env.GITHUB_RUN_ID;
        else process.env.GITHUB_RUN_ID = runId;
        if (workflowRef === undefined) delete process.env.GITHUB_WORKFLOW_REF;
        else process.env.GITHUB_WORKFLOW_REF = workflowRef;
      }
    }
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
    writePendingInputSnapshot({ directory: snapshotDirectory, manifest, bindings, publicationInputs, handlerConfig, gateConfig, context: originalContext, runnerTemp: directory });
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
            status: inventory.items[index].gate_decision === "apply" ? "success" : "skipped",
            skipped: inventory.items[index].gate_decision !== "apply",
            reason: inventory.items[index].gate_decision === "hold" ? "gate_held" : "gate_dropped",
            gate_decision: inventory.items[index].gate_decision,
            ...(decisions[index] === "apply" && messages[index]?.temporary_id ? { temporaryId: messages[index].temporary_id, number: 55, repo: run.repository } : {}),
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
        const invocations = bindings.find(binding => binding.id === "publication_invocations").value;
        vi.stubEnv("GH_AW_PENDING_WRITES_SPEC", JSON.stringify({ publications: invocations.map(({ job, id, ...invocation }) => ({ job, id, invocation })) }));
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

  it("applies a source-pinned held dispatch after real approval without rewriting its caller context", async () => {
    const message = { type: "dispatch_workflow", output_id: "so_aaaaaaaaaaaa", workflow_name: "worker", ref: "release/v1", inputs: { value: "original" } };
    const config = { dispatch_workflow: { workflows: ["worker"], "target-repo": "test-owner/test-repo", allowed_refs: ["release/**"], workflow_files: { worker: ".lock.yml" }, aw_context_workflows: ["worker"] } };
    const proof = await prepareProof([message], ["hold"], null, config);
    const dispatch = global.github.rest.actions.createWorkflowDispatch;
    vi.stubEnv("GH_AW_RUN_URL", "123");
    for (const pin of ["pending_writes_sha256", "write_decisions_sha256", "pending_write_inputs_sha256", "safe_output_items_sha256"]) vi.stubEnv(`GH_AW_REPLAY_${pin.toUpperCase()}`, proof.record[pin]);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_RUN_ATTEMPT", proof.record.run_attempt);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_WORKFLOW_SHA", proof.record.workflow_sha);
    await executeHeldReplayFromEnv({
      restorePendingInputs,
      runHandlers: async () => {
        const handlers = await loadHandlers(config);
        const result = await processMessages(handlers, [message], createManifestLogger(path.join(directory, "dispatch-replay-items.jsonl")));
        expect(result.results[0]).toMatchObject({ success: true, gate_decision: "apply", approvers: ["actual-reviewer"] });
        return { success: true };
      },
    });
    expect(dispatch).toHaveBeenCalledOnce();
    expect(dispatch.mock.calls[0][0]).toMatchObject({ workflow_id: "worker.lock.yml", ref: "refs/heads/release/v1", inputs: { value: "original" } });
    expect(JSON.parse(dispatch.mock.calls[0][0].inputs.aw_context)).toMatchObject({ run_id: "123", time: "2026-11-15T12:00:00.000Z" });
    expect(process.env.GITHUB_RUN_ID).toBe("999");
    expect(global.context.runId).toBe(999);
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

  it.each(["saved", "unchanged", "skipped"])("replays frozen native publication with actual %s result semantics", async status => {
    const proof = await prepareProof([{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Held producer" }], ["hold"], { depends_on: ["so_aaaaaaaaaaaa"] });
    const native = require("./push_repo_memory.cjs");
    const { withPendingPublication } = require("./pending_writes.cjs");
    const { describeDirectory } = require("./pending_writes_files.cjs");
    const execute = vi.spyOn(native, "main").mockImplementation(async () =>
      withPendingPublication(
        "push_repo_memory",
        "default",
        () => ({ id: "repo_memory:default", kind: "directory", files: describeDirectory(process.env.ARTIFACT_DIR) }),
        async () => {
          expect(process.env.MAX_FILE_COUNT).toBe("17");
          expect(process.env.FILE_GLOB_FILTER).toBe("*.json");
          expect(process.env.BRANCH_NAME).toBe("memory/original");
          expect(fs.readFileSync(path.join(process.env.ARTIFACT_DIR, "notes.json"), "utf8")).toBe('{"kept":true}');
          global.core.setOutput("save_status", status);
        }
      )
    );
    vi.stubEnv("MAX_FILE_COUNT", "999");
    vi.stubEnv("GH_AW_RUN_URL", "123");
    for (const pin of ["pending_writes_sha256", "write_decisions_sha256", "pending_write_inputs_sha256", "safe_output_items_sha256"]) vi.stubEnv(`GH_AW_REPLAY_${pin.toUpperCase()}`, proof.record[pin]);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_RUN_ATTEMPT", proof.record.run_attempt);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_WORKFLOW_SHA", proof.record.workflow_sha);
    const currentItems = path.join(directory, "current-publications.jsonl");
    try {
      await executeHeldReplayFromEnv({
        restorePendingInputs,
        runPublications: restored => require("./write_gate_publications.cjs").executeHeldPublications(restored, createManifestLogger(currentItems)),
        runHandlers: async () => {
          const handlers = await loadHandlers(proof.handlerConfig);
          await processMessages(handlers, [{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Held producer" }]);
          return { success: true };
        },
      });
      expect(execute).toHaveBeenCalledOnce();
      expect(process.env.MAX_FILE_COUNT).toBe("999");
      const row = fs
        .readFileSync(currentItems, "utf8")
        .trim()
        .split("\n")
        .map(JSON.parse)
        .find(row => row.output_id === "job_0123456789abcdef");
      expect(row).toMatchObject({ gate_decision: "apply", status: status === "saved" ? "success" : "skipped", approvers: ["actual-reviewer"] });
    } finally {
      execute.mockRestore();
    }
  });

  it("approval alone does not complete a held publication prerequisite", async () => {
    const proof = await prepareProof([{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Held producer" }], ["hold"], { depends_on: ["so_aaaaaaaaaaaa"] });
    const authorization = await authorizeHeldReplay(proof);
    proof.bind();
    const manifest = require("./pending_writes.cjs").readAuthorizedManifest();
    const item = manifest.items.find(item => item.publication);
    const inventory = require("./write_gate_inventory.cjs").readDecisionInventory(proof.decisionsPath, proof.record.write_decisions_sha256, proof.record.pending_writes_sha256);
    const dependency = require("./write_gate_runtime.cjs").publicationDependenciesApplied;
    expect(await withHeldReplayAuthorization(authorization, () => dependency(item, manifest, inventory))).toBe(false);
  });

  it("uses original frozen ledger preparation after approval without recreating the transaction artifact", async () => {
    const message = { type: "ledger_append", output_id: "so_aaaaaaaaaaaa", ledger: "metrics", record: { value: 1 } };
    const config = { ledger_append: { ledgers: [{ name: "metrics", max_record_kb: 32, max_patch_kb: 100 }] } };
    const transactions = path.join(directory, "gh-aw", "ledger-transactions.json");
    const bindingID = "generated:push_ledger_changes:transactions";
    const publication = {
      job: "push_ledger_changes",
      id: "transactions",
      type: "ledger",
      kind: "ledger",
      path: transactions,
      invocation: {
        module: "push_ledger_changes.cjs",
        env: { GH_AW_LEDGER_TRANSACTIONS: transactions, GH_AW_LEDGER_CONFIG: JSON.stringify(config.ledger_append) },
        input_binding_ids: ["agent_output", bindingID],
        path_env: { GH_AW_LEDGER_TRANSACTIONS: bindingID },
      },
    };
    const proof = await prepareProof([message], ["hold", "apply"], null, config, [publication]);
    const writer = require("./push_ledger_changes.cjs");
    const { withPendingPublication } = require("./pending_writes.cjs");
    const execute = vi.spyOn(writer, "main").mockImplementation(() =>
      withPendingPublication(
        "push_ledger_changes",
        "transactions",
        () => writer.readTransactions(),
        async () => {
          expect(writer.readTransactions().transaction_id).toBe("123:1");
          return { ledgers: { metrics: { requested: 1, persisted: 1, already_present: 0 } } };
        }
      )
    );
    vi.stubEnv("GH_AW_RUN_URL", "123");
    for (const pin of ["pending_writes_sha256", "write_decisions_sha256", "pending_write_inputs_sha256", "safe_output_items_sha256"]) vi.stubEnv(`GH_AW_REPLAY_${pin.toUpperCase()}`, proof.record[pin]);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_RUN_ATTEMPT", proof.record.run_attempt);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_WORKFLOW_SHA", proof.record.workflow_sha);
    const receipts = path.join(directory, "ledger-replay.jsonl");
    try {
      await executeHeldReplayFromEnv({
        restorePendingInputs,
        runHandlers: async () => {
          const manager = require("./safe_output_handler_manager.cjs");
          const handlers = await loadHandlers(config);
          const result = await processMessages(handlers, [message], createManifestLogger(receipts));
          expect(result.results[0]).toMatchObject({ success: true, result: { queued: true, delegated: true } });
          await manager.finalizeLedgerAppend(config, handlers);
          return { success: true };
        },
        runPublications: restored => require("./write_gate_publications.cjs").executeHeldPublications(restored, createManifestLogger(receipts)),
      });
      expect(execute).toHaveBeenCalledOnce();
      expect(process.env.GITHUB_RUN_ID).toBe("999");
      const rows = fs.readFileSync(receipts, "utf8").trim().split("\n").map(JSON.parse);
      expect(rows.filter(row => row.status === "success")).toHaveLength(1);
      expect(rows[0]).toMatchObject({ type: "ledger", gate_decision: "apply", approvers: ["actual-reviewer"] });
    } finally {
      execute.mockRestore();
    }
  });

  it.each(["invocation", "staged", "failure"])("does not claim a held native effect after %s veto", async veto => {
    const proof = await prepareProof([{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Source" }], ["hold"], {});
    const native = require("./push_repo_memory.cjs");
    const { withPendingPublication } = require("./pending_writes.cjs");
    const { describeDirectory } = require("./pending_writes_files.cjs");
    const persist = vi.fn(() => {
      if (veto === "failure") global.core.setFailed("native rejected the original data");
      global.core.setOutput("save_status", "saved");
    });
    const execute = vi
      .spyOn(native, "main")
      .mockImplementation(() => withPendingPublication("push_repo_memory", "default", () => ({ id: "repo_memory:default", kind: "directory", files: describeDirectory(process.env.ARTIFACT_DIR) }), persist));
    vi.stubEnv("GH_AW_RUN_URL", "123");
    for (const pin of ["pending_writes_sha256", "write_decisions_sha256", "pending_write_inputs_sha256", "safe_output_items_sha256"]) vi.stubEnv(`GH_AW_REPLAY_${pin.toUpperCase()}`, proof.record[pin]);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_RUN_ATTEMPT", proof.record.run_attempt);
    vi.stubEnv("GH_AW_REPLAY_SOURCE_WORKFLOW_SHA", proof.record.workflow_sha);
    if (veto === "staged") vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    const filename = path.join(directory, "veto-publications.jsonl");
    try {
      const operation = executeHeldReplayFromEnv({
        restorePendingInputs: options => {
          const restored = restorePendingInputs(options);
          if (veto === "invocation") restored.publication_invocations[0].env.MAX_FILE_COUNT = "170";
          return restored;
        },
        runHandlers: async () => ({ success: true }),
        runPublications: restored => require("./write_gate_publications.cjs").executeHeldPublications(restored, createManifestLogger(filename)),
      });
      if (veto === "staged") await operation;
      else await expect(operation).rejects.toThrow("Held publication did not complete");
      expect(persist).toHaveBeenCalledTimes(veto === "failure" ? 1 : 0);
      const rows = fs.readFileSync(filename, "utf8").trim().split("\n").map(JSON.parse);
      expect(rows.some(row => row.status === "success")).toBe(false);
      expect(rows[0].status).toBe(veto === "staged" ? "skipped" : "failure");
    } finally {
      execute.mockRestore();
    }
  });

  it.each(["apply", "hold", "stage", "drop", "review"])("records each original publication %s without claiming its later effect", async decision => {
    const proof = await prepareProof([{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Original source" }], ["hold"], { decision });
    proof.bind();
    const rows = [];
    await require("./write_gate_publications.cjs").recordPublicationDecisions(row => rows.push(row));
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ output_id: "job_0123456789abcdef", status: "skipped", gate_decision: decision === "review" ? "stage" : decision });
    expect(api.create).not.toHaveBeenCalled();
    expect(api.createComment).not.toHaveBeenCalled();
  });

  it("publishes a held-native review only as a trusted issue in the usable original review repo", async () => {
    const proof = await prepareProof([{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Source" }], ["hold"], { decision: "review", reviewRepo: "test-owner/test-repo" });
    proof.bind();
    const rows = [];
    await require("./write_gate_publications.cjs").recordPublicationDecisions(row => rows.push(row));
    expect(api.create).toHaveBeenCalledOnce();
    expect(api.create.mock.calls[0][0]).toMatchObject({ owner: "test-owner", repo: "test-repo" });
    expect(rows[0]).toMatchObject({ type: "create_issue", status: "success", gate_decision: "review", review_publication: true, url: "https://github.com/test-owner/test-repo/issues/101" });
    expect(api.createComment).not.toHaveBeenCalled();
  });

  it.each(["hold", "review"])("narrows unsupported %s conversion to stage without loosening stricter decisions", async decision => {
    const proof = await prepareProof([{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Source" }], ["hold"], { module: "../untrusted.cjs", decision, reviewRepo: "test-owner/test-repo" });
    proof.bind();
    const rows = [];
    await require("./write_gate_publications.cjs").recordPublicationDecisions(row => rows.push(row));
    expect(rows[0]).toMatchObject({ gate_decision: "stage", decision_reason: "gate_replay_unsupported", skipped: true });
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("was staged"));
    expect(api.create).not.toHaveBeenCalled();
  });

  it("restores selected review App policy without falling back to maintenance credentials", async () => {
    vi.stubEnv("GH_AW_WRITE_GATE_REVIEW_USE_APP", "true");
    vi.stubEnv("GH_AW_WRITE_GATE_REVIEW_TOKEN", "");
    const proof = await prepareProof([{ type: "add_comment", output_id: "so_aaaaaaaaaaaa", body: "Source" }], ["hold"], { decision: "review", reviewRepo: "test-owner/test-repo" });
    proof.bind();
    const restored = restorePendingInputs({
      directory: path.dirname(proof.snapshotPath),
      expectedSha256: proof.record.pending_write_inputs_sha256,
      manifest: require("./pending_writes.cjs").readAuthorizedManifest(),
      runnerTemp: path.join(directory, "restored-review"),
    });
    expect(restored.environment.GH_AW_WRITE_GATE_REVIEW_USE_APP).toBe("true");
    const rows = [];
    await require("./write_gate_publications.cjs").recordPublicationDecisions(row => rows.push(row));
    expect(rows[0].gate_decision).toBe("stage");
    expect(api.create).not.toHaveBeenCalled();
  });

  it("records failed and deferred decisions without duplicating actual success receipts", () => {
    const filename = path.join(directory, "complete-decisions.jsonl");
    const log = createManifestLogger(filename);
    const successful = { type: "add_comment", output_id: "so_aaaaaaaaaaaa", item_sha256: "a".repeat(64), gate_decision: "apply", decision_reason: "checked" };
    log({ ...successful, status: "success" });
    require("./write_gate_publications.cjs").recordUnpublishedItemDecisions(
      [
        { ...successful, success: true },
        { ...successful, output_id: "so_bbbbbbbbbbbb", success: false },
        { ...successful, output_id: "so_cccccccccccc", success: false, deferred: true },
      ],
      filename
    );
    const rows = fs.readFileSync(filename, "utf8").trim().split("\n").map(JSON.parse);
    expect(rows.map(row => row.status)).toEqual(["success", "failure", "skipped"]);
    expect(rows[1]).toMatchObject({ gate_decision: "apply", reason: "gate_handler_failed", decision_reason: "checked" });
    expect(rows[2].skipped).toBe(true);
  });
});
