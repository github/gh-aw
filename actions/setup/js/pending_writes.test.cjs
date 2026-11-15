import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { buildPendingWrites } from "./pending_writes_plan.cjs";
import { describeDirectory, sha256 } from "./pending_writes_files.cjs";
import { describePatchFiles, extractPathsFromPatch, checkFileProtectionPostApply } from "./manifest_file_helpers.cjs";
const require = createRequire(import.meta.url);
const {
  canonicalPendingJSON,
  inputDigest,
  itemDigest,
  prepare,
  verify,
  assertPendingWritesIntegrity,
  assertPendingPublication,
  withPendingWrite,
  withPendingReview,
  assertPendingCodeFiles,
  assertPendingTarget,
} = require("./pending_writes.cjs");

describe("handler-derived pending writes", () => {
  let directory;
  let options;
  const run = { repository: "owner/repo", workflow_ref: "owner/repo/.github/workflows/test.md@refs/heads/main", workflow_sha: "a".repeat(40), run_id: "123", run_attempt: "2", event: "issues" };
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "pending-writes-test-"));
    vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, eventName: "issues", payload: { issue: { number: 42 } } });
    vi.stubGlobal("core", { info: vi.fn(), warning: vi.fn(), error: vi.fn(), debug: vi.fn() });
    vi.stubEnv("GITHUB_REPOSITORY", "owner/repo");
    options = {
      agentOutputPath: path.join(directory, "agent_output.json"),
      manifestPath: path.join(directory, "pending-writes.json"),
      handlerConfig: { create_issue: { max: 10 }, add_comment: { target: "triggering" } },
      run,
      context: global.context,
      bindings: [],
      publications: [],
    };
    fs.writeFileSync(options.agentOutputPath, JSON.stringify({ items: [{ type: "create_issue", title: "Secret title", body: "Private body" }] }));
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  async function authorize() {
    const result = await prepare(options);
    vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "true");
    vi.stubEnv("GH_AW_PENDING_WRITES", options.manifestPath);
    vi.stubEnv("GH_AW_PENDING_WRITES_SHA256", result.sha256);
    vi.stubEnv("GH_AW_PENDING_WRITES_BINDINGS", JSON.stringify(result.bindings));
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify(options.handlerConfig));
    vi.stubEnv("GH_AW_PENDING_WRITES_SPEC", JSON.stringify({ publications: options.publications || [] }));
    const { buildWriteDecisionInventory, writeDecisionInventory } = require("./write_gate_inventory.cjs");
    const decisions = path.join(directory, "write-decisions.normalized.json");
    const inventory = buildWriteDecisionInventory(result.manifest, result.sha256, { decision: "allow", reason: "clear" }, () => {});
    vi.stubEnv("GH_AW_WRITE_GATE_DECISIONS", decisions);
    vi.stubEnv("GH_AW_WRITE_GATE_DECISIONS_SHA256", writeDecisionInventory(decisions, inventory));
    return result;
  }

  it("produces byte-identical canonical manifests and digests, never raw text or credentials", async () => {
    options.handlerConfig.create_issue["github-token"] = "not-a-public-token";
    vi.stubEnv("GH_TOKEN", "not-a-public-token");
    const first = await buildPendingWrites(options);
    options.handlerConfig = { add_comment: { target: "triggering" }, create_issue: { "github-token": "changed-token", max: 10 } };
    const second = await buildPendingWrites(options);
    expect(first.bytes.equals(second.bytes)).toBe(true);
    expect(first.sha256).toBe(second.sha256);
    expect(first.bytes.toString()).not.toMatch(/Secret title|Private body|not-a-public-token|changed-token/);
    expect(first.manifest.items[0].free_text).toContainEqual({ field: "/title", length: 12, sha256: sha256("Secret title") });
    expect(first.manifest.items[0].item_sha256).toBe(itemDigest(first.manifest.items[0]));
    expect(first.manifest.complete).toBe(true);
    const Ajv2020 = require("ajv/dist/2020");
    const validate = new Ajv2020().compile(require("./pending_writes.schema.json"));
    expect(validate(first.manifest), JSON.stringify(validate.errors)).toBe(true);
    expect(canonicalPendingJSON({ z: 1.5, a: { b: true, a: null } })).toBe('{"a":{"a":null,"b":true},"z":1.5}');
  });

  it("refuses a gate edit of downloaded output and of the manifest itself", async () => {
    const result = await authorize();
    expect((await verify(result.sha256, options)).allow).toBe(true);
    expect(assertPendingWritesIntegrity().items).toHaveLength(1);
    fs.writeFileSync(options.agentOutputPath, JSON.stringify({ items: [{ type: "create_issue", title: "changed", body: "changed" }] }));
    expect(await verify(result.sha256, options)).toEqual({ allow: false, reason: "inputs_changed" });
    expect(() => assertPendingWritesIntegrity()).toThrow("inputs_changed");
    fs.writeFileSync(options.manifestPath, "{}");
    expect(await verify(result.sha256, options)).toEqual({ allow: false, reason: "inputs_changed" });
  });

  it("rejects configuration changes, malformed JSON and symlink substitution", async () => {
    await authorize();
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify({ create_issue: { max: 100 } }));
    expect(() => assertPendingWritesIntegrity()).toThrow("inputs_changed");
    fs.writeFileSync(options.agentOutputPath, '{"items":[],"items":[]}');
    await expect(buildPendingWrites(options)).rejects.toThrow();
    fs.unlinkSync(options.agentOutputPath);
    fs.writeFileSync(path.join(directory, "replacement"), '{"items":[]}');
    fs.symlinkSync(path.join(directory, "replacement"), options.agentOutputPath);
    await expect(buildPendingWrites(options)).rejects.toThrow();
  });

  it.each(["GH_AW_WRITE_GATE_REVIEW_REPO", "GH_AW_WRITE_GATE_ALLOWED_REPOS"])("refuses a review write when %s changes after scope initialization", async name => {
    vi.stubEnv("GH_AW_WRITE_GATE_REVIEW_REPO", "owner/reviews");
    vi.stubEnv("GH_AW_WRITE_GATE_ALLOWED_REPOS", "owner/reviews,owner/repo");
    const result = await authorize();
    const create = vi.fn();
    const client = require("./pending_writes_client.cjs").guardPendingWriteClient({ rest: { issues: { create } } });
    await expect(
      withPendingReview(result.manifest.items[0], "owner/reviews", () => {
        vi.stubEnv(name, "other/reviews");
        return client.rest.issues.create({ owner: "owner", repo: "reviews", title: "review", body: "details" });
      })
    ).rejects.toThrow("inputs_changed");
    expect(create).not.toHaveBeenCalled();
  });

  it("resolves cross-repository and triggering targets through the handlers' resolvers", async () => {
    options.handlerConfig.create_issue = { "target-repo": "*", max: 10 };
    fs.writeFileSync(
      options.agentOutputPath,
      JSON.stringify({
        items: [
          { type: "create_issue", repo: "other/project", title: "a", body: "b" },
          { type: "add_comment", body: "reply" },
        ],
      })
    );
    const result = await authorize();
    expect(result.manifest.items.map(item => item.target)).toEqual([{ repository: "other/project" }, { repository: "owner/repo", number: 42, kind: "issue" }]);
    const message = JSON.parse(fs.readFileSync(options.agentOutputPath, "utf8")).items[1];
    await withPendingWrite(
      message,
      () => {
        expect(() => assertPendingTarget({ number: 42 })).not.toThrow();
        expect(() => assertPendingTarget({ number: 43 })).toThrow("target_changed");
      },
      1
    );
  });

  it("includes hard temporary-ID dependencies and marks invalid graphs incomplete", async () => {
    const producer = { type: "create_issue", title: "a", body: "b", temporary_id: "aw_abc" };
    const consumer = { type: "add_comment", body: "about #aw_abc" };
    fs.writeFileSync(options.agentOutputPath, JSON.stringify({ items: [producer, consumer] }));
    const result = await buildPendingWrites(options);
    expect(result.manifest.items[1].depends_on).toContain(result.manifest.items[0].output_id);
    fs.writeFileSync(options.agentOutputPath, JSON.stringify({ items: [{ ...producer, output_id: "so_aaaaaaaaaaaa", depends_on: ["so_aaaaaaaaaaaa"] }] }));
    expect((await buildPendingWrites(options)).manifest.complete).toBe(false);
  });

  it("checks later-job inventories and writes nothing when supplied bytes change", async () => {
    const memoryDir = path.join(directory, "memory");
    fs.mkdirSync(memoryDir);
    fs.writeFileSync(path.join(memoryDir, "notes.txt"), "memory content");
    options.bindings = [{ id: "memory:default", kind: "directory", path: memoryDir }];
    options.publications = [{ job: "push_repo_memory", id: "default", type: "repo_memory", binding_id: "memory:default", branch: "memory/default" }];
    const result = await authorize();
    const supplied = result.manifest.inputs.find(input => input.id === "memory:default");
    const write = vi.fn();
    assertPendingPublication("push_repo_memory", "default", supplied);
    write();
    expect(write).toHaveBeenCalledOnce();
    fs.writeFileSync(path.join(memoryDir, "notes.txt"), "changed bytes");
    expect(() => assertPendingPublication("push_repo_memory", "default", { ...supplied, files: describeDirectory(memoryDir) })).toThrow("inputs_changed");
    expect(() => assertPendingWritesIntegrity()).toThrow("inputs_changed");
    expect(write).toHaveBeenCalledOnce();
  });

  it.each(["hold", "review", "stage", "drop"])("does not run a %s secondary publication in a partial batch", async decision => {
    const memoryDir = path.join(directory, "memory");
    fs.mkdirSync(memoryDir);
    fs.writeFileSync(path.join(memoryDir, "notes.txt"), "unchanged");
    options.bindings = [{ id: "memory:default", kind: "directory", path: memoryDir }];
    options.publications = [{ job: "push_repo_memory", id: "default", type: "repo_memory", binding_id: "memory:default" }];
    const result = await authorize();
    const file = process.env.GH_AW_WRITE_GATE_DECISIONS;
    const inventory = JSON.parse(fs.readFileSync(file, "utf8"));
    inventory.decision = "partial";
    inventory.items.find(item => item.output_id.startsWith("job_")).gate_decision = decision;
    fs.writeFileSync(file, JSON.stringify(inventory));
    vi.stubEnv("GH_AW_WRITE_GATE_DECISIONS_SHA256", sha256(fs.readFileSync(file)));
    const write = vi.fn();
    const { withPendingPublication } = require("./pending_writes.cjs");
    await expect(withPendingPublication("push_repo_memory", "default", () => result.manifest.inputs.find(input => input.id === "memory:default"), write)).rejects.toThrow("item_not_authorized");
    expect(write).not.toHaveBeenCalled();
  });

  it("keeps native staging as a veto after an apply authorization", async () => {
    options.staged = true;
    options.publications = [{ job: "custom", id: "custom", type: "custom_job", binding_id: "agent_output" }];
    const result = await authorize();
    expect(() => assertPendingPublication("custom", "custom", result.manifest.inputs[0])).toThrow("native_policy_veto");
  });

  it("normalizes ledger records with the same helper as ledger_append", async () => {
    options.handlerConfig.ledger_append = { ledgers: [{ name: "metrics", max_record_kb: 32, max_patch_kb: 100 }] };
    fs.writeFileSync(options.agentOutputPath, JSON.stringify({ items: [{ type: "ledger_append", ledger: "metrics", record: { message: "private record", value: 1.5 } }] }));
    options.publications = [{ job: "push_ledger_changes", id: "transactions", type: "ledger", kind: "ledger" }];
    const result = await authorize();
    expect(result.manifest.complete).toBe(true);
    expect(result.bytes.toString()).not.toContain("private record");
    const expected = require("./pending_writes_plan.cjs").describeLedgerTransaction(JSON.parse(fs.readFileSync(options.agentOutputPath, "utf8")).items, options.handlerConfig, run);
    expect(() => assertPendingPublication("push_ledger_changes", "transactions", expected)).not.toThrow();
    expected.ledgers.metrics.appends[0].record.value = 2;
    expect(() => assertPendingPublication("push_ledger_changes", "transactions", expected)).toThrow("inputs_changed");
  });

  it("uses the call handler's exact payload and blocks changed reusable-workflow inputs", async () => {
    options.handlerConfig = { call_workflow: { workflows: ["worker"], max: 1 } };
    const message = { type: "call_workflow", workflow_name: "worker", inputs: { value: "selected", aw_context: "{}" } };
    fs.writeFileSync(options.agentOutputPath, JSON.stringify({ items: [message] }));
    options.publications = [{ job: "call-worker", id: "worker", worker: "worker", type: "call_workflow", kind: "call" }];
    const result = await authorize();
    expect(result.manifest.complete).toBe(true);
    expect(result.manifest.items[0].target.job).toBe("call-worker");
    const { buildCallWorkflowPayload } = require("./call_workflow.cjs");
    vi.stubEnv("GH_AW_CALL_WORKFLOW_NAME", "worker");
    vi.stubEnv("GH_AW_CALL_WORKFLOW_PAYLOAD", buildCallWorkflowPayload(message));
    const { checkPublication } = require("./pending_writes_runtime.cjs");
    expect(() => checkPublication({ job: "call-worker", id: "worker", kind: "call" })).not.toThrow();
    vi.stubEnv("GH_AW_CALL_WORKFLOW_PAYLOAD", JSON.stringify({ value: "unapproved", aw_context: "{}" }));
    expect(() => checkPublication({ job: "call-worker", id: "worker", kind: "call" })).toThrow("inputs_changed");
  });

  it("rejects an upload selecting a file outside its bound staging inventory", async () => {
    const reports = path.join(directory, "reports");
    fs.mkdirSync(reports);
    fs.writeFileSync(path.join(reports, "coverage.json"), "{}");
    options.bindings = [{ id: "coverage", kind: "directory", path: reports }];
    options.publications = [{ job: "upload_code_coverage", id: "coverage", type: "upload_code_coverage", binding_id: "coverage" }];
    await authorize();
    const { checkPublication } = require("./pending_writes_runtime.cjs");
    const spec = { job: "upload_code_coverage", id: "coverage", kind: "directory", binding_id: "coverage", path: reports };
    vi.stubEnv("GH_AW_PENDING_SELECTED_PATH", path.join(reports, "coverage.json"));
    expect(() => checkPublication(spec)).not.toThrow();
    vi.stubEnv("GH_AW_PENDING_SELECTED_PATH", path.join(reports, "..", "unreviewed.json"));
    expect(() => checkPublication(spec)).toThrow("target_changed");
  });

  it("fails closed on unsupported publication or dynamic target inputs", async () => {
    options.handlerConfig.create_issue.group = true;
    options.publications = [{ job: "unknown_job", id: "unknown", type: "custom_job" }];
    const result = await prepare(options);
    expect(result.manifest.complete).toBe(false);
    expect(result.manifest.items.every(item => !item.complete)).toBe(true);
    expect(await verify(result.sha256, options)).toMatchObject({ allow: true, reason: "manifest_incomplete" });
    vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "true");
    vi.stubEnv("GH_AW_PENDING_WRITES", options.manifestPath);
    vi.stubEnv("GH_AW_PENDING_WRITES_SHA256", result.sha256);
    vi.stubEnv("GH_AW_PENDING_WRITES_BINDINGS", JSON.stringify(result.bindings));
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify(options.handlerConfig));
    await expect(withPendingWrite(JSON.parse(fs.readFileSync(options.agentOutputPath, "utf8")).items[0], () => "not allowed", 0)).rejects.toThrow("item_incomplete");
  });

  it("keeps ungated runtimes unchanged", async () => {
    vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "");
    expect(assertPendingWritesIntegrity()).toBeNull();
    expect(() => assertPendingPublication("anything", "anything", null)).not.toThrow();
    expect(await withPendingWrite({}, () => "unchanged")).toBe("unchanged");
  });

  it.each(["owner/repo", "other/project"])("binds the published tree in %s and refuses same-path different-content before any push", async repository => {
    const repo = path.join(directory, "repo");
    fs.mkdirSync(repo);
    const git = args => execFileSync("git", ["-c", "commit.gpgsign=false", ...args], { cwd: repo, encoding: "utf8" });
    git(["init", "--quiet"]);
    git(["config", "user.name", "test"]);
    git(["config", "user.email", "test@example.com"]);
    fs.writeFileSync(path.join(repo, "file.txt"), "base\n");
    git(["add", "file.txt"]);
    git(["commit", "--quiet", "-m", "base"]);
    const base = git(["rev-parse", "HEAD"]).trim();
    const branch = `pending-test-${path.basename(directory)}`;
    git(["checkout", "--quiet", "-b", branch]);
    fs.writeFileSync(path.join(repo, "file.txt"), "approved\n");
    git(["add", "file.txt"]);
    git(["commit", "--quiet", "-m", "approved"]);
    const { getPatchPathForBranchInRepo } = require("./git_patch_utils.cjs");
    const patchPath = getPatchPathForBranchInRepo(branch, repository);
    fs.mkdirSync(path.dirname(patchPath), { recursive: true });
    try {
      fs.writeFileSync(patchPath, git(["format-patch", "--stdout", `${base}..HEAD`]));
      const message = { type: "create_pull_request", repo: repository, branch, title: "a", body: "b" };
      fs.writeFileSync(options.agentOutputPath, JSON.stringify({ items: [message] }));
      options.handlerConfig = { create_pull_request: { "target-repo": "*", base_branch: "main", preserve_branch_name: true, protected_files_policy: "allowed" } };
      options.localRepository = repo;
      vi.stubGlobal("github", { rest: { repos: { getBranch: async () => ({ data: { commit: { sha: base } } }) } } });
      if (repository !== "owner/repo") {
        options.handlerConfig.create_pull_request["github-token"] = "existing-scoped-read-token";
        const scopedClient = {
          rest: {
            repos: {
              getBranch: vi.fn(async request => {
                expect(`${request.owner}/${request.repo}`).toBe(repository);
                return { data: { commit: { sha: base } } };
              }),
            },
          },
        };
        vi.stubGlobal(
          "getOctokit",
          vi.fn(() => scopedClient)
        );
        global.github.rest.repos.getBranch = vi.fn(async () => {
          throw new Error("default token cannot read this target");
        });
      }
      const result = await authorize();
      expect(result.manifest.complete).toBe(true);
      expect(result.manifest.items[0].code.tree_sha256).toMatch(/^[0-9a-f]{64}$/);
      const { assertPendingCodePublication } = require("./pending_writes.cjs");
      const [owner, repoName] = repository.split("/");
      await withPendingWrite(message, () => assertPendingCodePublication({ owner, repo: repoName, branch, baseRef: base, cwd: repo }), 0);
      git(["remote", "add", "origin", "https://github.com/not-authorized/other.git"]);
      const push = vi.fn();
      vi.stubGlobal("exec", { getExecOutput: push, exec: push });
      const { pushSignedCommits } = require("./push_signed_commits.cjs");
      await expect(withPendingWrite(message, () => pushSignedCommits({ githubClient: global.github, owner, repo: repoName, branch, baseRef: base, cwd: repo, signedCommits: false }), 0)).rejects.toThrow("target_changed");
      expect(push).not.toHaveBeenCalled();
      fs.writeFileSync(path.join(repo, "file.txt"), "not approved\n");
      git(["add", "file.txt"]);
      git(["commit", "--quiet", "-m", "unreviewed edit"]);
      await expect(withPendingWrite(message, () => pushSignedCommits({ githubClient: global.github, owner, repo: repoName, branch, baseRef: base, cwd: repo, signedCommits: false }), 0)).rejects.toThrow("content_changed");
      expect(push).not.toHaveBeenCalled();
    } finally {
      fs.unlinkSync(patchPath);
    }
  });

  it("refuses publication without an active item and constrains trusted review writes", async () => {
    vi.stubEnv("GH_AW_WRITE_GATE_REVIEW_REPO", "policy/reviews");
    const result = await authorize();
    expect(() => assertPendingTarget({ repository: "owner/repo" })).toThrow("item_missing");
    const { guardPendingWriteClient } = require("./pending_writes_client.cjs");
    const create = vi.fn(async () => ({ data: { number: 1 } }));
    const guarded = guardPendingWriteClient({ rest: { issues: { create } } });
    await expect(guarded.rest.issues.create({ owner: "owner", repo: "repo", title: "a" })).rejects.toThrow("item_missing");
    const { withPendingReview } = require("./pending_writes.cjs");
    await withPendingReview(result.manifest.items[0], "policy/reviews", () => guarded.rest.issues.create({ owner: "policy", repo: "reviews", title: "a" }));
    await expect(withPendingReview(result.manifest.items[0], "policy/reviews", () => guarded.rest.issues.create({ owner: "policy", repo: "reviews", title: "a", assignees: ["copilot"] }))).rejects.toThrow("review_scope_invalid");
    await expect(withPendingReview(result.manifest.items[0], "wrong/repository", () => guarded.rest.issues.create({ owner: "wrong", repo: "repository", title: "a" }))).rejects.toThrow("review_scope_invalid");
    expect(create).toHaveBeenCalledOnce();
    vi.stubEnv("GH_AW_WRITE_GATE_ALLOWED_REPOS", "different/repo");
    await expect(withPendingReview(result.manifest.items[0], "policy/reviews", () => guarded.rest.issues.create({ owner: "policy", repo: "reviews", title: "a" }))).rejects.toThrow("inputs_changed");
    expect(create).toHaveBeenCalledOnce();
  });

  it("freezes original data and gate configuration and rejects edited replay inputs", async () => {
    const { writePendingInputSnapshot, restorePendingInputs, verifyPendingInputSnapshot } = require("./pending_write_inputs.cjs");
    const { gateConfigFromEnv } = require("./pending_writes.cjs");
    const sourceRoot = path.join(directory, "source");
    const sourceDirectory = path.join(sourceRoot, "gh-aw");
    fs.mkdirSync(sourceDirectory, { recursive: true });
    fs.copyFileSync(options.agentOutputPath, path.join(sourceDirectory, "agent_output.json"));
    options.agentOutputPath = path.join(sourceDirectory, "agent_output.json");
    vi.stubEnv("GH_AW_WRITE_GATE_REVIEW_REPO", "policy/reviews");
    vi.stubEnv("GH_AW_WRITE_GATE_ALLOWED_REPOS", "policy/reviews,other/project");
    vi.stubEnv("GH_AW_WRITE_GATE_APPROVAL_ENVIRONMENT", "original-reviewers");
    const result = await buildPendingWrites(options);
    const snapshotDirectory = path.join(directory, "snapshot");
    const snapshot = writePendingInputSnapshot({
      directory: snapshotDirectory,
      manifest: result.manifest,
      bindings: result.bindings,
      handlerConfig: options.handlerConfig,
      gateConfig: gateConfigFromEnv(),
      runnerTemp: sourceRoot,
    });
    expect(verifyPendingInputSnapshot({ directory: snapshotDirectory, expectedSha256: snapshot.sha256, manifest: result.manifest }).run).toEqual(result.manifest.run);
    vi.stubEnv("GH_AW_WRITE_GATE_APPROVAL_ENVIRONMENT", "changed-reviewers");
    const restored = restorePendingInputs({ directory: snapshotDirectory, expectedSha256: snapshot.sha256, manifest: result.manifest, runnerTemp: path.join(directory, "restored") });
    expect(restored.gateConfig).toEqual({ review_repo: "policy/reviews", allowed_repos: "policy/reviews,other/project", approval_environment: "original-reviewers" });
    expect(fs.readFileSync(restored.agentOutputPath)).toEqual(fs.readFileSync(options.agentOutputPath));
    fs.writeFileSync(path.join(snapshotDirectory, "inputs", "0"), '{"items":[]}');
    expect(() => restorePendingInputs({ directory: snapshotDirectory, expectedSha256: snapshot.sha256, manifest: result.manifest, runnerTemp: path.join(directory, "not-restored") })).toThrow("snapshot_changed");
    expect(fs.existsSync(path.join(directory, "not-restored"))).toBe(false);
  });

  it("fails closed when the one available cross-repository read credential is unavailable", async () => {
    const branch = `pending-unreadable-${path.basename(directory)}`;
    const { getPatchPathForBranchInRepo } = require("./git_patch_utils.cjs");
    const patchPath = getPatchPathForBranchInRepo(branch, "private/project");
    fs.mkdirSync(path.dirname(patchPath), { recursive: true });
    fs.writeFileSync(patchPath, "diff --git a/a b/a\n");
    try {
      const getBranch = vi.fn(async () => {
        throw Object.assign(new Error("inaccessible"), { status: 403 });
      });
      const factory = vi.fn(() => ({ rest: { repos: { getBranch } } }));
      vi.stubGlobal("getOctokit", factory);
      options.handlerConfig = { create_pull_request: { "target-repo": "*", "github-token": "existing-scoped-read-token", base_branch: "main" } };
      fs.writeFileSync(options.agentOutputPath, JSON.stringify({ items: [{ type: "create_pull_request", repo: "private/project", branch, title: "a", body: "b" }] }));
      const result = await buildPendingWrites(options);
      expect(result.manifest.items[0]).toMatchObject({ complete: false, reason_category: "read_unavailable" });
      expect(factory).toHaveBeenCalledExactlyOnceWith("existing-scoped-read-token", { credentialSource: "pat" });
      expect(getBranch).toHaveBeenCalledOnce();
      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("no future write-App token"));
    } finally {
      fs.unlinkSync(patchPath);
    }
  });

  it("does not persist credential text from the original event context in replay metadata", async () => {
    const { writePendingInputSnapshot } = require("./pending_write_inputs.cjs");
    const { gateConfigFromEnv } = require("./pending_writes.cjs");
    const sourceRoot = path.join(directory, "source");
    fs.mkdirSync(path.join(sourceRoot, "gh-aw"), { recursive: true });
    fs.copyFileSync(options.agentOutputPath, path.join(sourceRoot, "gh-aw", "agent_output.json"));
    options.agentOutputPath = path.join(sourceRoot, "gh-aw", "agent_output.json");
    global.context.payload.issue.body = ["ghp_", "a".repeat(36)].join("");
    const result = await buildPendingWrites(options);
    const destination = path.join(directory, "snapshot");
    expect(() => writePendingInputSnapshot({ directory: destination, manifest: result.manifest, bindings: result.bindings, handlerConfig: options.handlerConfig, gateConfig: gateConfigFromEnv(), runnerTemp: sourceRoot })).toThrow(
      "snapshot_secret"
    );
    expect(fs.existsSync(path.join(destination, "pending-write-inputs.json"))).toBe(false);
  });
});

describe("pending code files and protected-files parity", () => {
  const patch = `diff --git a/old.txt b/new.txt
similarity index 100%
rename from old.txt
rename to new.txt
diff --git a/deleted.txt b/deleted.txt
deleted file mode 100644
--- a/deleted.txt
+++ /dev/null
@@ -1 +0,0 @@
-deleted
diff --git a/image.png b/image.png
index aaaaaaa..bbbbbbb 100644
GIT binary patch
literal 4
abc
diff --git a/script.sh b/script.sh
old mode 100644
new mode 100755
diff --git a/submodule b/submodule
index aaaaaaa..bbbbbbb 160000
--- a/submodule
+++ b/submodule
@@ -1 +1 @@
-Subproject commit aaaaaaa
+Subproject commit bbbbbbb
`;
  it("describes renames, deletions, binary, modes and submodules without a second path parser", () => {
    const result = describePatchFiles(patch);
    expect(result.evaluated_paths).toEqual(extractPathsFromPatch(patch));
    expect(result.files[0]).toMatchObject({ path: "new.txt", previous_path: "old.txt", status: "renamed" });
    expect(result.files[1]).toMatchObject({ path: "deleted.txt", status: "deleted", deletions: 1 });
    expect(result.files[2].binary).toBe(true);
    expect(result.files[3]).toMatchObject({ mode_changed: true, old_mode: "100644", new_mode: "100755" });
    expect(result.files[4].submodule).toBe(true);
    expect(checkFileProtectionPostApply(result.evaluated_paths, { allowed_files: ["not-present"] }).files).toEqual(result.evaluated_paths);
  });
  it("rejects unparseable path headers instead of hiding files", () => {
    expect(() => describePatchFiles("diff --git no-parseable-path\n")).toThrow("unparseable");
  });
  it("will not authorize a post-apply file absent from the reviewed list", () => {
    vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "true");
    expect(() => assertPendingCodeFiles(["unexpected.txt"])).toThrow("code_plan_incomplete");
    vi.unstubAllEnvs();
  });
});
