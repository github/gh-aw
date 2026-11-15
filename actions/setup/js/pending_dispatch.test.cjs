import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { prepare, withPendingWrite } = require("./pending_writes.cjs");
const { buildWriteDecisionInventory, writeDecisionInventory } = require("./write_gate_inventory.cjs");
const { guardPendingWriteClient } = require("./pending_writes_client.cjs");

describe("handler-derived pending dispatch", () => {
  let directory;
  let options;
  let workflowDispatch;
  let repositoryDispatch;
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "pending-dispatch-"));
    fs.mkdirSync(path.join(directory, "gh-aw"));
    workflowDispatch = vi.fn().mockResolvedValue({ data: {} });
    repositoryDispatch = vi.fn().mockResolvedValue({ data: {} });
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
    vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, eventName: "workflow_dispatch", payload: {}, runId: 123 });
    vi.stubGlobal("github", {
      rest: {
        actions: { createWorkflowDispatch: workflowDispatch, getWorkflowRun: vi.fn().mockResolvedValue({ data: { id: 123, created_at: "2026-11-15T12:00:00Z" } }) },
        repos: { createDispatchEvent: repositoryDispatch, get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }) },
      },
    });
    vi.stubEnv("GITHUB_REPOSITORY", "owner/repo");
    vi.stubEnv("GITHUB_REF", "");
    vi.stubEnv("GITHUB_HEAD_REF", "");
    vi.stubEnv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/test.md@main");
    vi.stubEnv("GITHUB_RUN_ID", "123");
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "1");
    options = {
      agentOutputPath: path.join(directory, "gh-aw", "agent_output.json"),
      manifestPath: path.join(directory, "gh-aw", "pending-writes.json"),
      handlerConfig: {},
      context: global.context,
      run: { repository: "owner/repo", workflow_ref: process.env.GITHUB_WORKFLOW_REF, workflow_sha: "a".repeat(40), run_id: "123", run_attempt: "1", event: "workflow_dispatch" },
      bindings: [],
      publications: [],
    };
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  async function authorize(message) {
    fs.writeFileSync(options.agentOutputPath, JSON.stringify({ items: [message] }));
    const result = await prepare(options);
    vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "true");
    vi.stubEnv("GH_AW_PENDING_WRITES", options.manifestPath);
    vi.stubEnv("GH_AW_PENDING_WRITES_SHA256", result.sha256);
    vi.stubEnv("GH_AW_PENDING_WRITES_BINDINGS", JSON.stringify(result.bindings));
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify(options.handlerConfig));
    vi.stubEnv("GH_AW_PENDING_WRITES_SPEC", '{"publications":[]}');
    const decisions = path.join(directory, "decisions.json");
    const inventory = buildWriteDecisionInventory(result.manifest, result.sha256, { decision: "allow", reason: "checked" }, () => {});
    vi.stubEnv("GH_AW_WRITE_GATE_DECISIONS", decisions);
    vi.stubEnv("GH_AW_WRITE_GATE_DECISIONS_SHA256", writeDecisionInventory(decisions, inventory));
    vi.stubGlobal("github", guardPendingWriteClient(global.github));
    return result;
  }

  it("plans and executes the actual configured cross-repository workflow/ref and converted inputs", async () => {
    const config = { workflows: ["worker"], "target-repo": "other/project", allowed_repos: ["other/project"], allowed_refs: ["release/**"], workflow_files: { worker: ".lock.yml" }, aw_context_workflows: ["worker"] };
    options.handlerConfig.dispatch_workflow = config;
    const message = { type: "dispatch_workflow", workflow_name: "worker", repo: "ignored/project", ref: "release/v1", inputs: { count: 2, value: { private: "not public" } } };
    const result = await authorize(message);
    expect(result.manifest.complete).toBe(true);
    expect(result.manifest.items[0].target).toEqual({ repository: "other/project", workflow_id: "worker.lock.yml", ref: "refs/heads/release/v1" });
    expect(workflowDispatch).not.toHaveBeenCalled();
    expect(result.bytes.toString()).not.toContain("not public");
    const validate = new (require("ajv/dist/2020"))().compile(require("./pending_writes.schema.json"));
    expect(validate(result.manifest), JSON.stringify(validate.errors)).toBe(true);
    const handler = await require("./dispatch_workflow.cjs").main(config);
    const outcome = await withPendingWrite(message, () => handler(message, {}), 0);
    expect(outcome.success).toBe(true);
    expect(workflowDispatch).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "other",
        repo: "project",
        workflow_id: "worker.lock.yml",
        ref: "refs/heads/release/v1",
        inputs: expect.objectContaining({ count: "2", value: '{"private":"not public"}', aw_context: expect.any(String) }),
      })
    );
  });

  it("plans the actual configured repository event and exact injected caller payload", async () => {
    const config = { tools: { deploy: { repository: "other/project", allowed_repositories: ["other/project"], event_type: "deploy", workflow: "worker" } } };
    options.handlerConfig.dispatch_repository = config;
    const message = { type: "dispatch_repository", tool_name: "deploy", inputs: { private: "not public" } };
    const result = await authorize(message);
    expect(result.manifest.complete).toBe(true);
    expect(result.manifest.items[0].target).toEqual({ repository: "other/project", event_type: "deploy" });
    expect(repositoryDispatch).not.toHaveBeenCalled();
    expect(result.bytes.toString()).not.toContain("not public");
    const handler = await require("./dispatch_repository.cjs").main(config);
    expect((await withPendingWrite(message, () => handler(message, {}), 0)).success).toBe(true);
    expect(repositoryDispatch).toHaveBeenCalledWith(
      expect.objectContaining({ owner: "other", repo: "project", event_type: "deploy", client_payload: expect.objectContaining({ workflow: "worker", private: "not public", aw_context: expect.any(Object) }) })
    );
  });

  it.each(["workflow_id", "ref", "inputs"])("refuses changed workflow %s before the actual API call", async field => {
    options.handlerConfig.dispatch_workflow = { workflows: ["worker"], workflow_files: { worker: ".yml" }, "target-ref": "refs/heads/main" };
    const message = { type: "dispatch_workflow", workflow_name: "worker", inputs: { value: "original" } };
    await authorize(message);
    const request = { owner: "owner", repo: "repo", workflow_id: "worker.yml", ref: "refs/heads/main", inputs: { value: "original" } };
    request[field] = field === "inputs" ? { value: "changed" } : "changed";
    await expect(withPendingWrite(message, () => global.github.rest.actions.createWorkflowDispatch(request), 0)).rejects.toThrow("dispatch_changed");
    expect(workflowDispatch).not.toHaveBeenCalled();
  });

  it.each(["event_type", "client_payload"])("refuses changed repository %s before the actual API call", async field => {
    const config = { tools: { deploy: { repository: "owner/repo", event_type: "deploy", workflow: "worker" } } };
    options.handlerConfig.dispatch_repository = config;
    const message = { type: "dispatch_repository", tool_name: "deploy", inputs: { value: "original" } };
    await authorize(message);
    const contexts = JSON.parse(process.env.GH_AW_PENDING_WRITES_BINDINGS).find(binding => binding.id === "dispatch_contexts").value;
    const planned = await require("./dispatch_repository.cjs").plan(config, message, Object.values(contexts)[0]);
    const request = { owner: "owner", repo: "repo", ...planned.effect };
    request[field] = field === "client_payload" ? { value: "changed" } : "changed";
    await expect(withPendingWrite(message, () => global.github.rest.repos.createDispatchEvent(request), 0)).rejects.toThrow("dispatch_changed");
    expect(repositoryDispatch).not.toHaveBeenCalled();
  });

  it("fails closed on an unavailable scoped default-ref read without a main fallback or credential retry", async () => {
    options.handlerConfig.dispatch_workflow = { workflows: ["worker"], "target-repo": "other/private", allowed_repos: ["other/private"], workflow_files: { worker: ".yml" } };
    const get = vi.fn().mockRejectedValue(Object.assign(new Error("not accessible"), { status: 404 }));
    const client = { rest: { repos: { get } } };
    const getOctokit = vi.fn().mockReturnValue(client);
    vi.stubGlobal("getOctokit", getOctokit);
    options.readTokens = { "other/private": "existing-read" };
    options.readToken = "workflow-token";
    const result = await authorize({ type: "dispatch_workflow", workflow_name: "worker" });
    expect(result.manifest.items[0]).toMatchObject({ complete: false, reason_category: "read_unavailable" });
    expect(getOctokit).toHaveBeenCalledTimes(1);
    expect(getOctokit).toHaveBeenCalledWith("existing-read", expect.any(Object));
    expect(get).toHaveBeenCalledTimes(1);
    expect(workflowDispatch).not.toHaveBeenCalled();
  });

  it("accepts the same scoped default-ref read when the target is available", async () => {
    options.handlerConfig.dispatch_workflow = { workflows: ["worker"], "target-repo": "other/private", allowed_repos: ["other/private"], workflow_files: { worker: ".yml" } };
    const get = vi.fn().mockResolvedValue({ data: { default_branch: "release" } });
    vi.stubGlobal("getOctokit", vi.fn().mockReturnValue({ rest: { repos: { get } } }));
    options.readTokens = { "other/private": "existing-read" };
    const result = await authorize({ type: "dispatch_workflow", workflow_name: "worker" });
    expect(result.manifest.items[0]).toMatchObject({ complete: true, target: { repository: "other/private", ref: "refs/heads/release" } });
    expect(get).toHaveBeenCalledWith({ owner: "other", repo: "private" });
    expect(workflowDispatch).not.toHaveBeenCalled();
  });

  it("produces byte-identical manifests despite wall-clock changes", async () => {
    options.handlerConfig.dispatch_repository = { tools: { deploy: { repository: "owner/repo", event_type: "deploy" } } };
    const message = { type: "dispatch_repository", tool_name: "deploy" };
    const first = await authorize(message);
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
    const second = await require("./pending_writes_plan.cjs").buildPendingWrites({ ...options });
    expect(second.bytes.equals(first.bytes)).toBe(true);
    expect(second.sha256).toBe(first.sha256);
    expect(first.manifest.items[0].dispatch.context_time).toBe("2026-11-15T12:00:00.000Z");
  });

  it("restores original caller identity from the pinned snapshot while retaining approval execution IDs", async () => {
    const config = { tools: { deploy: { repository: "owner/repo", event_type: "deploy" } } };
    options.handlerConfig.dispatch_repository = config;
    const message = { type: "dispatch_repository", tool_name: "deploy" };
    const result = await authorize(message);
    const { writePendingInputSnapshot, restorePendingInputs } = require("./pending_write_inputs.cjs");
    const { gateConfigFromEnv } = require("./pending_writes.cjs");
    const snapshotDirectory = path.join(directory, "snapshot");
    const snapshot = writePendingInputSnapshot({ directory: snapshotDirectory, manifest: result.manifest, bindings: result.bindings, handlerConfig: options.handlerConfig, gateConfig: gateConfigFromEnv(), runnerTemp: directory });
    const restored = restorePendingInputs({ directory: snapshotDirectory, expectedSha256: snapshot.sha256, manifest: result.manifest, runnerTemp: path.join(directory, "restored") });
    vi.stubEnv("GH_AW_PENDING_WRITES_BINDINGS", JSON.stringify(restored.bindings.map(({ value, ...binding }) => binding)));
    vi.stubEnv("GH_AW_PENDING_WRITE_INPUTS", snapshotDirectory);
    vi.stubEnv("GITHUB_RUN_ID", "999");
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
    vi.stubEnv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/approval.yml@main");
    const handler = await require("./dispatch_repository.cjs").main(config);
    expect((await withPendingWrite(message, () => handler(message, {}), 0)).success).toBe(true);
    expect(repositoryDispatch).toHaveBeenCalledWith(
      expect.objectContaining({
        client_payload: expect.objectContaining({ aw_context: expect.objectContaining({ run_id: "123", root_run_id: "123", run_attempt: "1", workflow_id: options.run.workflow_ref, time: "2026-11-15T12:00:00.000Z" }) }),
      })
    );
    expect(process.env.GITHUB_RUN_ID).toBe("999");
    expect(process.env.GITHUB_RUN_ATTEMPT).toBe("2");
  });

  it("refuses altered injected caller context before dispatch", async () => {
    const config = { tools: { deploy: { repository: "owner/repo", event_type: "deploy" } } };
    options.handlerConfig.dispatch_repository = config;
    const message = { type: "dispatch_repository", tool_name: "deploy" };
    const result = await authorize(message);
    const context = JSON.parse(result.bindings.find(binding => binding.id === "dispatch_contexts").value[result.manifest.items[0].output_id]);
    context.run_id = "999";
    await expect(withPendingWrite(message, () => global.github.rest.repos.createDispatchEvent({ owner: "owner", repo: "repo", event_type: "deploy", client_payload: { workflow: "", aw_context: context } }), 0)).rejects.toThrow(
      "dispatch_changed"
    );
    expect(repositoryDispatch).not.toHaveBeenCalled();
  });
});
