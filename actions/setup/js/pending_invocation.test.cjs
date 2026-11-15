import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { buildPendingWrites } = require("./pending_writes_plan.cjs");
const { writePendingInputSnapshot, restorePendingInputs } = require("./pending_write_inputs.cjs");
const { gateConfigFromEnv } = require("./pending_writes.cjs");
const { assertPendingPublication, withPendingPublication } = require("./pending_writes.cjs");
const { describeDirectory } = require("./pending_writes_files.cjs");
const { buildWriteDecisionInventory, writeDecisionInventory } = require("./write_gate_inventory.cjs");

describe("frozen native publication invocation", () => {
  it("restores original limits and remaps path env through input bindings", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "pending-invocation-"));
    try {
      vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, eventName: "issues", payload: { issue: { number: 1 } } });
      vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
      vi.stubEnv("GITHUB_REPOSITORY", "owner/repo");
      const original = path.join(directory, "original");
      const root = path.join(original, "gh-aw");
      const memory = path.join(root, "memory");
      fs.mkdirSync(memory, { recursive: true });
      const output = path.join(root, "agent_output.json");
      fs.writeFileSync(output, '{"items":[{"type":"create_issue","title":"a","body":"b"}]}');
      fs.writeFileSync(path.join(memory, "notes.json"), '{"kept":true}');
      const handlerConfig = { create_issue: {} };
      const options = {
        agentOutputPath: output,
        handlerConfig,
        context: global.context,
        run: { repository: "owner/repo", workflow_ref: "owner/repo/.github/workflows/test.md@main", workflow_sha: "a".repeat(40), run_id: "1", run_attempt: "1", event: "issues" },
        bindings: [{ id: "repo_memory:default", kind: "directory", path: memory }],
        publications: [
          {
            job: "push_repo_memory",
            id: "default",
            type: "repo_memory",
            binding_id: "repo_memory:default",
            invocation: {
              module: "push_repo_memory.cjs",
              env: { ARTIFACT_DIR: memory, MEMORY_ID: "default", BRANCH_NAME: "memory/default", MAX_FILE_COUNT: "17", ALLOWED_EXTENSIONS: '[".json"]', FILE_GLOB_FILTER: "*.json" },
              input_binding_ids: ["repo_memory:default"],
              path_env: { ARTIFACT_DIR: "repo_memory:default" },
            },
          },
        ],
      };
      const result = await buildPendingWrites(options);
      const snapshotRoot = path.join(directory, "snapshot");
      const snapshot = writePendingInputSnapshot({ directory: snapshotRoot, manifest: result.manifest, bindings: result.bindings, handlerConfig, gateConfig: gateConfigFromEnv(), runnerTemp: original });
      const restored = restorePendingInputs({ directory: snapshotRoot, expectedSha256: snapshot.sha256, manifest: result.manifest, runnerTemp: path.join(directory, "new-runner") });
      const invocation = restored.publication_invocations[0];
      expect(invocation.module).toBe("push_repo_memory.cjs");
      expect(invocation.env.MAX_FILE_COUNT).toBe("17");
      expect(invocation.env.ALLOWED_EXTENSIONS).toBe('[".json"]');
      expect(invocation.env.FILE_GLOB_FILTER).toBe("*.json");
      expect(invocation.env.MEMORY_ID).toBe("default");
      expect(invocation.env.ARTIFACT_DIR).toBe(path.join(directory, "new-runner", "gh-aw", "memory"));
      expect(result.bytes.toString()).not.toContain('"MAX_FILE_COUNT":"17"');
      expect(result.manifest.items.find(item => item.target.job === "push_repo_memory").publication.invocation.env).toContainEqual(expect.objectContaining({ name: "MAX_FILE_COUNT", length: 2 }));
      const manifestPath = path.join(directory, "pending-writes.json");
      fs.writeFileSync(manifestPath, result.bytes);
      const decisions = path.join(directory, "decisions.json");
      const inventory = buildWriteDecisionInventory(result.manifest, result.sha256, { decision: "allow", reason: "checked" }, () => {});
      vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "true");
      vi.stubEnv("GH_AW_PENDING_WRITES", manifestPath);
      vi.stubEnv("GH_AW_PENDING_WRITES_SHA256", result.sha256);
      vi.stubEnv("GH_AW_PENDING_WRITES_BINDINGS", JSON.stringify(restored.bindings));
      vi.stubEnv("GH_AW_WRITE_GATE_DECISIONS", decisions);
      vi.stubEnv("GH_AW_WRITE_GATE_DECISIONS_SHA256", writeDecisionInventory(decisions, inventory));
      for (const [name, value] of Object.entries(invocation.env)) vi.stubEnv(name, value);
      const supplied = () => ({ id: "repo_memory:default", kind: "directory", files: describeDirectory(invocation.env.ARTIFACT_DIR) });
      expect(() => assertPendingPublication("push_repo_memory", "default", supplied())).not.toThrow();
      const persist = vi.fn();
      vi.stubEnv("MAX_FILE_COUNT", "170");
      await expect(withPendingPublication("push_repo_memory", "default", supplied, persist)).rejects.toThrow("invocation_changed");
      expect(persist).not.toHaveBeenCalled();
      vi.stubEnv("MAX_FILE_COUNT", "17");
      fs.writeFileSync(path.join(invocation.env.ARTIFACT_DIR, "notes.json"), '{"kept":false}');
      await expect(withPendingPublication("push_repo_memory", "default", supplied, persist)).rejects.toThrow("inputs_changed");
      expect(persist).not.toHaveBeenCalled();
    } finally {
      vi.unstubAllEnvs();
      vi.unstubAllGlobals();
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("restores ledger transactions at the bound generated input path", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "pending-ledger-invocation-"));
    try {
      vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, eventName: "issues", payload: { issue: { number: 1 } } });
      vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
      vi.stubEnv("GITHUB_REPOSITORY", "owner/repo");
      const original = path.join(directory, "original");
      const root = path.join(original, "gh-aw");
      fs.mkdirSync(root, { recursive: true });
      const output = path.join(root, "agent_output.json");
      const transactions = path.join(root, "ledger", "transactions.json");
      fs.writeFileSync(output, JSON.stringify({ items: [{ type: "ledger_append", ledger: "metrics", record: { metric: "metric-1", value: 2 } }] }));
      const handlerConfig = { ledger_append: { ledgers: [{ name: "metrics", max_record_kb: 32, max_patch_kb: 100 }] } };
      const inputID = "generated:push_ledger_changes:transactions";
      const result = await buildPendingWrites({
        agentOutputPath: output,
        handlerConfig,
        context: global.context,
        run: { repository: "owner/repo", workflow_ref: "owner/repo/.github/workflows/test.md@main", workflow_sha: "a".repeat(40), run_id: "1", run_attempt: "1", event: "issues" },
        bindings: [],
        publications: [
          {
            job: "push_ledger_changes",
            id: "transactions",
            type: "ledger",
            kind: "ledger",
            path: transactions,
            invocation: {
              module: "push_ledger_changes.cjs",
              env: { GH_AW_LEDGER_TRANSACTIONS: transactions, GH_AW_LEDGER_CONFIG: JSON.stringify(handlerConfig.ledger_append) },
              input_binding_ids: ["agent_output", inputID],
              path_env: { GH_AW_LEDGER_TRANSACTIONS: inputID },
            },
          },
        ],
      });
      const snapshotRoot = path.join(directory, "snapshot");
      const snapshot = writePendingInputSnapshot({
        directory: snapshotRoot,
        manifest: result.manifest,
        bindings: result.bindings,
        publicationInputs: result.publicationInputs,
        handlerConfig,
        gateConfig: gateConfigFromEnv(),
        runnerTemp: original,
      });
      const restored = restorePendingInputs({ directory: snapshotRoot, expectedSha256: snapshot.sha256, manifest: result.manifest, runnerTemp: path.join(directory, "restored") });
      const invocation = restored.publication_invocations[0];
      expect(invocation.env.GH_AW_LEDGER_TRANSACTIONS).toBe(path.join(directory, "restored", "gh-aw", "ledger", "transactions.json"));
      expect(restored.generatedBindings).toEqual([{ id: inputID, kind: "file", path: invocation.env.GH_AW_LEDGER_TRANSACTIONS }]);
      expect(JSON.parse(fs.readFileSync(invocation.env.GH_AW_LEDGER_TRANSACTIONS, "utf8"))).toEqual(result.publicationInputs[0].value);
      expect(invocation.env.GH_AW_LEDGER_CONFIG).toBe(JSON.stringify(handlerConfig.ledger_append));
    } finally {
      vi.unstubAllEnvs();
      vi.unstubAllGlobals();
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});
