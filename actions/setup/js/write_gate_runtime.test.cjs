// @ts-check

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, it, beforeEach, afterEach, expect, vi } from "vitest";
import { canonicalPendingJSON, inputDigest, itemDigest, gateConfigFromEnv, pendingContext, pendingEnvironment } from "./pending_writes.cjs";
import { readInput, sha256 } from "./pending_writes_files.cjs";
import { publishWriteDecisionInventory } from "./write_gate_runtime.cjs";
import { processMessages } from "./safe_output_handler_manager.cjs";
import { createManifestLogger } from "./safe_output_manifest.cjs";
import { DECISIONS } from "./write_gate_decisions.cjs";
import { createIssue } from "./create_issue.cjs";
import { guardPendingWriteClient } from "./pending_writes_client.cjs";
import { useExecutionProvenanceFixture } from "./test_execution_provenance.js";

useExecutionProvenanceFixture();

describe("pinned per-item manager enforcement", () => {
  let directory;
  let agentOutput;
  let config;
  let handler;
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "write-gate-runtime-"));
    agentOutput = path.join(directory, "agent_output.json");
    config = {};
    vi.stubGlobal("core", {
      info: vi.fn(),
      debug: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      exportVariable: vi.fn((name, value) => vi.stubEnv(name, value)),
    });
    vi.stubGlobal("context", { repo: { owner: "test-owner", repo: "test-repo" }, eventName: "issues", payload: { issue: { number: 42 } }, runId: 12345, serverUrl: "https://github.com" });
    vi.stubGlobal("github", {
      rest: { repos: { get: vi.fn().mockResolvedValue({ data: { has_issues: true } }) }, issues: { create: vi.fn().mockResolvedValue({ data: { number: 90, html_url: "https://github.com/review-owner/review-repo/issues/90" } }) } },
    });
    handler = vi.fn().mockResolvedValue({ success: true, number: 10, repo: "test-owner/test-repo", url: "https://github.com/test-owner/test-repo/issues/10" });
    vi.stubEnv("GH_AW_WRITE_GATE_REVIEW_REPO", "review-owner/review-repo");
    vi.stubEnv("GH_AW_WRITE_GATE_ALLOWED_REPOS", "review-owner/review-repo");
    vi.stubEnv("GH_AW_WORKFLOW_NAME", "Gate test");
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  // A compiler-bound fixture isolates the actual dispatcher from target-planner
  // support. Target-planner completeness has its own pending_writes tests.
  function authorize(messages, entries, { detection = { enabled: false, mode: "disabled", conclusion: "success" }, staged = false } = {}) {
    const source = { items: messages };
    fs.writeFileSync(agentOutput, JSON.stringify(source));
    for (const message of messages) config[message.type] ||= {};
    const items = messages.map((message, index) => {
      const item = {
        output_id: message.output_id || `so_${inputDigest([index, message]).slice(0, 12)}`,
        type: message.type,
        target: { repository: "test-owner/test-repo" },
        depends_on: message.depends_on || [],
        settings: { staged, sha256: inputDigest(config[message.type]) },
        free_text: [],
        input_sha256: inputDigest(message),
        complete: true,
        reason_category: "ready",
      };
      return { ...item, item_sha256: itemDigest(item) };
    });
    const file = readInput(agentOutput);
    const manifest = {
      schema: "gh-aw/pending-writes/v1",
      run: {},
      detection,
      complete: true,
      inputs: [
        { id: "agent_output", kind: "file", length: file.length, sha256: file.sha256, mode: file.mode },
        { id: "handler_config", kind: "value", sha256: inputDigest(config) },
        { id: "gate_config", kind: "value", sha256: inputDigest(gateConfigFromEnv()) },
        { id: "context", kind: "value", sha256: inputDigest(pendingContext()) },
        { id: "environment", kind: "value", sha256: inputDigest(pendingEnvironment()) },
        { id: "publication_invocations", kind: "value", sha256: inputDigest([]) },
      ],
      items,
    };
    const bytes = canonicalPendingJSON(manifest);
    const manifestFile = path.join(directory, "pending-writes.json");
    fs.writeFileSync(manifestFile, bytes);
    vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "true");
    vi.stubEnv("GH_AW_PENDING_WRITES", manifestFile);
    vi.stubEnv("GH_AW_PENDING_WRITES_SHA256", sha256(bytes));
    vi.stubEnv(
      "GH_AW_PENDING_WRITES_BINDINGS",
      JSON.stringify([
        { id: "agent_output", kind: "file", path: agentOutput },
        { id: "handler_config", kind: "value", value: config },
        { id: "gate_config", kind: "value" },
        { id: "context", kind: "value" },
        { id: "environment", kind: "value" },
        { id: "publication_invocations", kind: "value" },
      ])
    );
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify(config));
    const verdictFile = path.join(directory, "write-decisions.json");
    fs.writeFileSync(verdictFile, JSON.stringify({ decision: "partial", reason: "items_checked", items: entries || items.map(item => ({ output_id: item.output_id, decision: "apply", reason: "clear" })) }));
    vi.stubEnv("GH_AW_WRITE_DECISIONS", verdictFile);
    publishWriteDecisionInventory();
    return manifest;
  }

  it.each(["create_pull_request", "create_issue", "add_comment", "add_labels", "dispatch_workflow", "custom_output", "upload_asset"])("enforces every decision for %s before its handler or later delegation", async type => {
    const messages = DECISIONS.map((decision, index) => ({ type, output_id: `so_${String(index).padStart(12, "0")}`, title: decision, body: decision }));
    authorize(
      messages,
      messages.map((message, index) => ({ output_id: message.output_id, decision: DECISIONS[index], reason: `gate_${DECISIONS[index]}` }))
    );
    const result = await processMessages(new Map([[type, handler]]), messages);
    expect(handler).toHaveBeenCalledTimes(1);
    expect(global.github.rest.issues.create).toHaveBeenCalledTimes(1);
    expect(result.results.map(item => item.gate_decision)).toEqual(DECISIONS);
    expect(result.results.filter(item => item.skipped)).toHaveLength(3);
    expect(result.results.every(item => item.output_id && /^[a-f0-9]{64}$/.test(item.item_sha256))).toBe(true);
  });

  it("records applied, reviewed and suppressed item decisions in the item manifest", async () => {
    const messages = DECISIONS.map((decision, index) => ({ type: "create_issue", output_id: `so_${String(index).padStart(12, "0")}`, title: decision, body: decision }));
    authorize(
      messages,
      messages.map((message, index) => ({ output_id: message.output_id, decision: DECISIONS[index], reason: `gate_${DECISIONS[index]}` }))
    );
    const manifestFile = path.join(directory, "safe-output-items.jsonl");
    await processMessages(new Map([["create_issue", handler]]), messages, createManifestLogger(manifestFile));
    const rows = fs.readFileSync(manifestFile, "utf8").trim().split("\n").map(JSON.parse);
    expect(rows.map(row => row.gate_decision)).toEqual(DECISIONS);
    expect(rows.filter(row => row.skipped).map(row => row.reason)).toEqual(["gate_held", "gate_staged", "gate_dropped"]);
    expect(rows.every(row => row.output_id && row.item_sha256 && row.decision_reason)).toBe(true);
    expect(rows.find(row => row.gate_decision === "review").url).toBe("https://github.com/review-owner/review-repo/issues/90");
  });

  it("drops missing entries and rejects altered/missing normalized files before invocation", async () => {
    const messages = [{ type: "create_issue", title: "item", body: "item" }];
    authorize(messages, []);
    const result = await processMessages(new Map([["create_issue", handler]]), messages);
    expect(handler).not.toHaveBeenCalled();
    expect(result.results[0]).toMatchObject({ skipped: true, gate_decision: "drop", decision_reason: "gate_item_missing" });
    fs.appendFileSync(process.env.GH_AW_WRITE_GATE_DECISIONS, "\n");
    await expect(processMessages(new Map([["create_issue", handler]]), messages)).rejects.toThrow("digest mismatch");
    fs.unlinkSync(process.env.GH_AW_WRITE_GATE_DECISIONS);
    await expect(processMessages(new Map([["create_issue", handler]]), messages)).rejects.toThrow();
    expect(handler).not.toHaveBeenCalled();
  });

  it("does not release depends-on consumers of held or reviewed producers", async () => {
    const messages = [
      { type: "create_issue", output_id: "so_aaaaaaaaaaaa", title: "producer", body: "producer" },
      { type: "add_comment", output_id: "so_bbbbbbbbbbbb", depends_on: ["so_aaaaaaaaaaaa"], body: "consumer" },
    ];
    authorize(messages, [
      { output_id: messages[0].output_id, decision: "hold", reason: "approval_required" },
      { output_id: messages[1].output_id, decision: "apply", reason: "clear" },
    ]);
    const result = await processMessages(
      new Map([
        ["create_issue", handler],
        ["add_comment", handler],
      ]),
      messages
    );
    expect(handler).not.toHaveBeenCalled();
    expect(result.results[1]).toMatchObject({ gate_decision: "drop", decision_reason: "gate_dependency_unapplied", errorCode: "safe_output_dependency_failed" });
  });

  it("staging and failed detection override gate apply, and warn veto blocks labels", async () => {
    const messages = [{ type: "add_labels", labels: ["test"] }];
    authorize(messages, undefined, { detection: { enabled: true, mode: "warn", conclusion: "warning" } });
    const result = await processMessages(new Map([["add_labels", handler]]), messages);
    expect(result.results[0]).toMatchObject({ skipped: true, gate_decision: "drop" });
    expect(handler).not.toHaveBeenCalled();
  });

  it("rechecks source integrity before a deferred retry, not just its first invocation", async () => {
    const messages = [{ type: "create_issue", title: "deferred", body: "deferred" }];
    authorize(messages);
    handler.mockImplementationOnce(async () => {
      fs.writeFileSync(agentOutput, JSON.stringify({ items: [] }));
      return { deferred: true, success: false };
    });
    const result = await processMessages(new Map([["create_issue", handler]]), messages);
    expect(handler).toHaveBeenCalledTimes(1);
    expect(result.results[0].success).toBe(false);
    expect(result.results[0].error).toContain("inputs_changed");
  });

  it.each(["apply", "hold", "stage", "drop"])("custom createIssue automatically honors %s and keeps an active guarded item scope", async decision => {
    const messages = [{ type: "create_issue", output_id: "so_aaaaaaaaaaaa", title: "item", body: "item" }];
    authorize(messages, [{ output_id: messages[0].output_id, decision, reason: "checked" }]);
    const api = global.github.rest.issues.create;
    vi.stubGlobal("github", guardPendingWriteClient(global.github));
    const result = await createIssue({ title: "item", body: "item", output_id: messages[0].output_id });
    expect(result.staged).toBe(decision !== "apply");
    expect(api).toHaveBeenCalledTimes(decision === "apply" ? 1 : 0);
    if (decision !== "apply") expect(result.gate_decision).toBe(decision);
  });

  it("custom createIssue fails closed without an item identity or with a changed target", async () => {
    const messages = [{ type: "create_issue", output_id: "so_aaaaaaaaaaaa", title: "item", body: "item" }];
    authorize(messages);
    await expect(createIssue({ title: "item", body: "item" })).rejects.toThrow("requires output_id");
    await expect(createIssue({ title: "item", body: "item", output_id: messages[0].output_id, owner: "other", repo: "repo" })).rejects.toThrow("target");
    expect(global.github.rest.issues.create).not.toHaveBeenCalled();
  });
});
