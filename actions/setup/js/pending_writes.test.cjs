import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { buildPendingWrites } from "./pending_writes_plan.cjs";
import { canonicalPendingJSON, inputDigest, itemDigest, prepare, verify, assertPendingWritesIntegrity, assertPendingPublication, withPendingWrite, assertPendingCodeFiles, assertPendingTarget } from "./pending_writes.cjs";
import { describeDirectory, sha256 } from "./pending_writes_files.cjs";
import { describePatchFiles, extractPathsFromPatch, checkFileProtectionPostApply } from "./manifest_file_helpers.cjs";

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

  it("fails closed on unsupported publication or dynamic target inputs", async () => {
    options.handlerConfig.create_issue.group = true;
    options.publications = [{ job: "unknown_job", id: "unknown", type: "custom_job" }];
    const result = await prepare(options);
    expect(result.manifest.complete).toBe(false);
    expect(result.manifest.items.every(item => !item.complete)).toBe(true);
    expect(await verify(result.sha256, options)).toEqual({ allow: false, reason: "manifest_incomplete" });
  });

  it("keeps ungated runtimes unchanged", async () => {
    vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "");
    expect(assertPendingWritesIntegrity()).toBeNull();
    expect(() => assertPendingPublication("anything", "anything", null)).not.toThrow();
    expect(await withPendingWrite({}, () => "unchanged")).toBe("unchanged");
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
