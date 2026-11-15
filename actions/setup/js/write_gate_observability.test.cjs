import { describe, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { buildGateDecision, checkConclusion } from "./write_gate_observability.cjs";
import { evaluateItemDecision } from "./write_gate_decisions.cjs";
import { collectUnifiedSession } from "./unified_session.cjs";
import { renderUnifiedSession } from "./unified_session_render.cjs";
import { compactUnifiedSession } from "./compact_unified_session.cjs";
import { buildFailureMatchCategories } from "./handle_agent_failure.cjs";
import { canonicalPendingJSON, itemDigest, SCHEMA } from "./pending_writes.cjs";
import { writeDecisionInventory } from "./write_gate_inventory.cjs";

const digest = "a".repeat(64);
const item = { output_id: "so_abcdefghijkl", item_sha256: digest, type: "create_issue", complete: true };
const manifest = { complete: true, items: [item] };
const base = { GH_AW_WRITE_GATE: "allow", GH_AW_WRITE_GATE_REASON: "policy", GH_AW_PENDING_WRITES_SHA256: digest, GH_AW_JOB_RESULTS: "{}", GH_AW_WRITE_GATE_WRITE_JOB_NAMES: "[]" };
const build = (env = {}, results = [], extra = {}) => buildGateDecision({ env: { ...base, ...env }, results, manifest, actualDigest: digest, resultsComplete: true, ...extra });

describe("compiler-owned gate conclusion evidence", () => {
  it.each([
    ["allow", [{ ...item, status: "success", gate_decision: "apply" }], "verified"],
    ["deny", [{ ...item, status: "skipped", gate_signal: "gate_denied" }], "verified"],
    ["partial", [{ ...item, status: "skipped", gate_decision: "hold", decision_reason: "gate_held" }], "verified"],
    ["", [], "unknown"],
  ])("records %s without changing enforcement", (verdict, results, check) => {
    const data = build({ GH_AW_WRITE_GATE: verdict }, results);
    expect(data.verdict).toBe(verdict || "deny");
    expect(data.reason).toBe(verdict ? "policy" : "gate_error");
    expect(data.conclusionCheck).toBe(check);
  });

  it("matches golden gate events and summaries for allow, deny, partial and gate error", () => {
    const decisions = ["allow", "deny", "partial", ""].map(verdict => build({ GH_AW_WRITE_GATE: verdict }));
    expect(decisions).toMatchInlineSnapshot(`
      [
        {
          "conclusionCheck": "unknown",
          "counts": {
            "apply": 0,
            "drop": 0,
            "hold": 0,
            "review": 0,
            "stage": 0,
          },
          "integrity": "verified",
          "items": [],
          "manifestDigest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "reason": "policy",
          "verdict": "allow",
        },
        {
          "conclusionCheck": "unknown",
          "counts": {
            "apply": 0,
            "drop": 0,
            "hold": 0,
            "review": 0,
            "stage": 0,
          },
          "integrity": "verified",
          "items": [],
          "manifestDigest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "reason": "policy",
          "verdict": "deny",
        },
        {
          "conclusionCheck": "unknown",
          "counts": {
            "apply": 0,
            "drop": 0,
            "hold": 0,
            "review": 0,
            "stage": 0,
          },
          "integrity": "verified",
          "items": [],
          "manifestDigest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "reason": "policy",
          "verdict": "partial",
        },
        {
          "conclusionCheck": "unknown",
          "counts": {
            "apply": 0,
            "drop": 0,
            "hold": 0,
            "review": 0,
            "stage": 0,
          },
          "integrity": "verified",
          "items": [],
          "manifestDigest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "reason": "gate_error",
          "verdict": "deny",
        },
      ]
    `);
    expect(
      decisions.map(data =>
        renderUnifiedSession([{ type: "gate.decision", data, provenance: { component: "write_gate", phase: "conclusion", path: "usage/aw_session.jsonl", index: 0 } }], { markdown: true, maxBytes: 100000, maxLineBytes: 10000 })
      )
    ).toMatchInlineSnapshot(`
      [
        "<details><summary>Unified session</summary>

      <details><summary>Session overview</summary>

      \`\`\`text
      Records: 1 (write_gate=1)
      \`\`\`

      </details>

      ### Conversation

      \`\`\`text
      No agent conversation recorded.
      \`\`\`

      <details><summary>Full timeline</summary>

      \`\`\`text
      Chronological trace (user prompts omitted; untimed observations follow):

      [1] write_gate/conclusion gate.decision verdict=allow reason=policy manifestDigest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa integrity=verified conclusionCheck=unknown counts={   "apply": 0,   "hold": 0,   "review": 0,   "stage": 0,   "drop": 0 } items=[] [untimed]
        path=usage/aw_session.jsonl
      \`\`\`

      </details>



      </details>",
        "<details><summary>Unified session</summary>

      <details><summary>Session overview</summary>

      \`\`\`text
      Records: 1 (write_gate=1)
      \`\`\`

      </details>

      ### Conversation

      \`\`\`text
      No agent conversation recorded.
      \`\`\`

      <details><summary>Full timeline</summary>

      \`\`\`text
      Chronological trace (user prompts omitted; untimed observations follow):

      [1] write_gate/conclusion gate.decision verdict=deny reason=policy manifestDigest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa integrity=verified conclusionCheck=unknown counts={   "apply": 0,   "hold": 0,   "review": 0,   "stage": 0,   "drop": 0 } items=[] [untimed]
        path=usage/aw_session.jsonl
      \`\`\`

      </details>



      </details>",
        "<details><summary>Unified session</summary>

      <details><summary>Session overview</summary>

      \`\`\`text
      Records: 1 (write_gate=1)
      \`\`\`

      </details>

      ### Conversation

      \`\`\`text
      No agent conversation recorded.
      \`\`\`

      <details><summary>Full timeline</summary>

      \`\`\`text
      Chronological trace (user prompts omitted; untimed observations follow):

      [1] write_gate/conclusion gate.decision verdict=partial reason=policy manifestDigest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa integrity=verified conclusionCheck=unknown counts={   "apply": 0,   "hold": 0,   "review": 0,   "stage": 0,   "drop": 0 } items=[] [untimed]
        path=usage/aw_session.jsonl
      \`\`\`

      </details>



      </details>",
        "<details><summary>Unified session</summary>

      <details><summary>Session overview</summary>

      \`\`\`text
      Records: 1 (write_gate=1)
      \`\`\`

      </details>

      ### Conversation

      \`\`\`text
      No agent conversation recorded.
      \`\`\`

      <details><summary>Full timeline</summary>

      \`\`\`text
      Chronological trace (user prompts omitted; untimed observations follow):

      [1] write_gate/conclusion gate.decision verdict=deny reason=gate_error manifestDigest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa integrity=verified conclusionCheck=unknown counts={   "apply": 0,   "hold": 0,   "review": 0,   "stage": 0,   "drop": 0 } items=[] [untimed]
        path=usage/aw_session.jsonl
      \`\`\`

      </details>



      </details>",
      ]
    `);
  });

  it("catches a handler ignoring deny and a changed applied digest", () => {
    expect(build({ GH_AW_WRITE_GATE: "deny" }, [{ ...item, status: "success" }])).toMatchObject({ verdict: "deny", failureCategory: "write_gate_violation", conclusionCheck: "failed" });
    expect(build({ GH_AW_WRITE_GATE: "deny" }, [{ ...item, status: "success", gate_signal: "gate_denied" }]).conclusionCheck).toBe("failed");
    expect(build({}, [{ ...item, item_sha256: "b".repeat(64), status: "success" }]).conclusionCheck).toBe("failed");
    expect(build({}, [], { actualDigest: "b".repeat(64) })).toMatchObject({ integrity: "failed", conclusionCheck: "failed" });
    expect(buildFailureMatchCategories({ hasWriteGateViolation: true })).toEqual(["write_gate_violation"]);
    expect(build({ GH_AW_WRITE_GATE: "deny" }, [], { manifest: { complete: false, items: [] } })).toMatchObject({ integrity: "verified", conclusionCheck: "verified" });
  });

  it("verifies supported partial writes without authorizing incomplete plan entries", () => {
    const unsupported = { ...item, output_id: "so_000000000000", complete: false };
    const pending = { complete: false, items: [item, unsupported] };
    const decisions = {
      schema: "gh-aw/write-decisions/v1",
      manifest_sha256: digest,
      items: [
        { ...item, gate_decision: "apply" },
        { ...unsupported, gate_decision: "drop" },
      ],
    };
    const env = { GH_AW_WRITE_GATE: "partial", GH_AW_WRITE_GATE_DECISIONS_SHA256: digest };
    const results = [
      { ...item, status: "success", gate_decision: "apply" },
      { ...unsupported, status: "skipped", gate_decision: "drop" },
    ];
    const extra = { manifest: pending, decisions, decisionsDigest: digest };
    expect(build(env, results, extra)).toMatchObject({ verdict: "partial", integrity: "verified", conclusionCheck: "verified" });
    expect(
      build(
        env,
        [
          { ...item, status: "success", gate_decision: "apply" },
          { ...unsupported, status: "success", gate_decision: "apply" },
        ],
        extra
      )
    ).toMatchObject({ verdict: "partial", conclusionCheck: "failed", failureCategory: "write_gate_violation" });
  });

  it("never promotes unknown or failed jobs and missing result/digest evidence to verified", () => {
    expect(build({}, [], { resultsComplete: false }).conclusionCheck).toBe("unknown");
    expect(build({}, []).conclusionCheck).toBe("unknown");
    expect(build({}, [], { actualDigest: undefined }).integrity).toBe("unknown");
    expect(build({}, [{ status: "success" }]).conclusionCheck).toBe("unknown");
    expect(build({ GH_AW_WRITE_GATE_WRITE_JOB_NAMES: '["upload_assets"]' }).conclusionCheck).toBe("unknown");
    expect(build({ GH_AW_WRITE_GATE: "deny", GH_AW_WRITE_GATE_WRITE_JOB_NAMES: '["upload_assets"]', GH_AW_JOB_RESULTS: '{"upload_assets":{"result":"success"}}' }).conclusionCheck).toBe("failed");
  });

  it("checks partial writes against independent digest-bound decisions instead of handler echoes", () => {
    const decisions = { schema: "gh-aw/write-decisions/v1", manifest_sha256: digest, items: [{ ...item, gate_decision: "hold", decision_reason: "review_required" }] };
    const extra = { decisions, decisionsDigest: digest };
    const env = { GH_AW_WRITE_GATE: "partial", GH_AW_WRITE_GATE_DECISIONS_SHA256: digest };
    const result = { ...item, status: "success", gate_decision: "apply" };
    expect(build(env, [result], extra)).toMatchObject({ verdict: "partial", conclusionCheck: "failed", counts: { hold: 1, apply: 0 } });
    decisions.items[0].gate_decision = "apply";
    expect(build(env, [result], extra).conclusionCheck).toBe("verified");
    expect(build(env, [result], { ...extra, decisionsDigest: "b".repeat(64) }).conclusionCheck).toBe("failed");
    expect(build(env, [result]).conclusionCheck).toBe("unknown");
    decisions.items[0].gate_decision = "review";
    expect(build(env, [{ ...result, type: "create_pull_request" }], extra).conclusionCheck).toBe("failed");
  });

  it("accepts hold-to-review narrowing while rejecting broader or non-review effects", () => {
    const decisions = { schema: "gh-aw/write-decisions/v1", manifest_sha256: digest, items: [{ ...item, gate_decision: "hold", decision_reason: "approval_required" }] };
    const env = { GH_AW_WRITE_GATE: "partial", GH_AW_WRITE_GATE_DECISIONS_SHA256: digest };
    const extra = { decisions, decisionsDigest: digest };
    const selection = evaluateItemDecision({
      gate: "partial",
      entry: "hold",
      outcome: "review",
      detection: "pass",
      manifestMatches: true,
      staged: false,
      reviewRepoUsable: true,
      dependenciesApplied: true,
      replay: "run",
      approverRecorded: false,
    });
    expect(selection).toEqual({ decision: "review", reason: "gate_existing_restriction" });
    const result = { ...item, type: "create_issue", status: "success", gate_decision: selection.decision, decision_reason: selection.reason, review_publication: true, url: "https://github.com/acme/repo/issues/90" };
    expect(build(env, [result], extra)).toMatchObject({ verdict: "partial", conclusionCheck: "verified", counts: { hold: 1, review: 0 } });
    expect(build(env, [{ ...result, gate_decision: "apply" }], extra).conclusionCheck).toBe("failed");
    expect(build(env, [{ ...result, type: "create_pull_request" }], extra).conclusionCheck).toBe("failed");
    expect(build(env, [{ ...result, review_publication: undefined }], extra).conclusionCheck).toBe("unknown");
    expect(build(env, [{ ...result, url: undefined }], extra).conclusionCheck).toBe("unknown");
    decisions.items[0].gate_decision = "stage";
    expect(build(env, [result], extra).conclusionCheck).toBe("failed");
    decisions.items[0].gate_decision = "drop";
    expect(build(env, [result], extra).conclusionCheck).toBe("failed");
  });

  it("does not confuse an authorized partial secondary job with a policy violation", () => {
    const jobItem = { output_id: "job_aaaaaaaaaaaaaaaa", item_sha256: digest, target: { job: "upload_assets" } };
    const decisions = { schema: "gh-aw/write-decisions/v1", manifest_sha256: digest, items: [{ ...jobItem, gate_decision: "apply" }] };
    const env = { GH_AW_WRITE_GATE: "partial", GH_AW_WRITE_GATE_DECISIONS_SHA256: digest, GH_AW_WRITE_GATE_WRITE_JOB_NAMES: '["upload_assets"]', GH_AW_JOB_RESULTS: '{"upload_assets":{"result":"success"}}' };
    const extra = { manifest: { complete: true, items: [jobItem] }, decisions, decisionsDigest: digest };
    expect(build(env, [], extra)).toMatchObject({ verdict: "partial", conclusionCheck: "unknown" });
    decisions.items[0].gate_decision = "hold";
    expect(build(env, [], extra)).toMatchObject({ verdict: "partial", conclusionCheck: "failed", failureCategory: "write_gate_violation" });
    expect(build({ ...env, GH_AW_WRITE_GATE_DECISIONS_SHA256: undefined }, [], { ...extra, decisions: undefined, decisionsDigest: undefined }).conclusionCheck).toBe("unknown");
  });

  it("checks partial memory publication against input receipts and existing effect outputs", () => {
    const jobItem = { output_id: "job_aaaaaaaaaaaaaaaa", item_sha256: digest, target: { job: "push_repo_memory", branch: "memory/default" }, publication: { id: "default" } };
    const decisions = { schema: "gh-aw/write-decisions/v1", manifest_sha256: digest, items: [{ ...jobItem, gate_decision: "apply" }] };
    const proof = { push_repo_memory: { pending_writes_verified: "true", pending_writes_item_sha256: JSON.stringify({ [jobItem.output_id]: digest }) } };
    const env = {
      GH_AW_WRITE_GATE: "partial",
      GH_AW_WRITE_GATE_DECISIONS_SHA256: digest,
      GH_AW_WRITE_GATE_WRITE_JOB_NAMES: '["push_repo_memory"]',
      GH_AW_JOB_RESULTS: '{"push_repo_memory":{"result":"success"}}',
      GH_AW_WRITE_GATE_PUBLICATION_RESULTS: JSON.stringify(proof),
      GH_AW_MEMORY_SAVE_RESULTS: JSON.stringify({ default: { jobResult: "success", outcome: "success", status: "saved", branch: "memory/default", commit: "b".repeat(40) } }),
    };
    const extra = { manifest: { complete: true, items: [jobItem] }, decisions, decisionsDigest: digest };
    expect(build(env, [], extra).conclusionCheck).toBe("verified");
    decisions.items[0].gate_decision = "review";
    expect(build(env, [], extra).conclusionCheck).toBe("failed");
    expect(build({ ...env, GH_AW_JOB_RESULTS: '{"push_repo_memory":{"result":"failure"}}' }, [], extra).conclusionCheck).toBe("failed");
    decisions.items[0].gate_decision = "apply";
    expect(build({ ...env, GH_AW_MEMORY_SAVE_RESULTS: undefined }, [], extra).conclusionCheck).toBe("unknown");
    proof.push_repo_memory.pending_writes_item_sha256 = JSON.stringify({ [jobItem.output_id]: "c".repeat(64) });
    expect(build({ ...env, GH_AW_WRITE_GATE_PUBLICATION_RESULTS: JSON.stringify(proof) }, [], extra).conclusionCheck).toBe("failed");
  });

  it("collects trusted events with bounded summary/compact metadata and leaves ungated collection unchanged", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gate-observability-"));
    try {
      const collectorItem = { ...item, input_sha256: digest, complete: true };
      collectorItem.item_sha256 = itemDigest(collectorItem);
      const encoded = canonicalPendingJSON({ schema: SCHEMA, ...manifest, inputs: [], items: [collectorItem] });
      fs.writeFileSync(path.join(root, "pending-writes.json"), encoded);
      fs.writeFileSync(path.join(root, "safe-output-items.jsonl"), JSON.stringify({ ...collectorItem, status: "success", gate_decision: "apply", title: "PRIVATE_TITLE" }) + "\n");
      const options = { rootDir: root, warn: () => {}, dailyAIC: {}, admission: {} };
      const ungated = collectUnifiedSession({ ...options, writeGate: {} }).events;
      expect(ungated.some(event => event.type === "gate.decision")).toBe(false);
      const events = collectUnifiedSession({ ...options, writeGate: { ...base, GH_AW_PENDING_WRITES_SHA256: createHash("sha256").update(encoded).digest("hex") } }).events;
      const gate = events.find(event => event.type === "gate.decision");
      expect(gate.provenance).toMatchObject({ component: "write_gate", phase: "conclusion" });
      expect(gate.data).toMatchObject({ verdict: "allow", counts: { apply: 1 }, conclusionCheck: "verified" });
      const summary = renderUnifiedSession(events, { markdown: true, maxBytes: 100000, maxLineBytes: 10000 });
      expect(summary).toContain("conclusionCheck=verified");
      expect(summary).not.toContain("PRIVATE_TITLE");
      const compact = compactUnifiedSession(events).session;
      expect(compact).toContain('"counts":{"apply":1,"hold":0,"review":0,"stage":0,"drop":0}');
      expect(compact).not.toContain("so_abcdefghijkl");
      fs.writeFileSync(path.join(root, "safe-output-items.jsonl"), JSON.stringify({ ...collectorItem, status: "success", gate_signal: "gate_denied" }) + "\n");
      const core = { warning: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() };
      checkConclusion({ rootDir: root, env: { ...base, GH_AW_WRITE_GATE: "deny" }, core });
      expect(core.setFailed).toHaveBeenCalledWith("ERR_WRITE_GATE: Conclusion detected an unauthorized write or publication digest mismatch.");
      expect(JSON.parse(fs.readFileSync(path.join(root, "failure_categories.json"), "utf8"))).toEqual(["write_gate_violation"]);
      const inventoryFile = path.join(root, "write-decisions.normalized.json");
      const manifestSha256 = createHash("sha256").update(encoded).digest("hex");
      const inventorySha256 = writeDecisionInventory(inventoryFile, {
        schema: "gh-aw/write-decisions/v1",
        manifest_sha256: manifestSha256,
        decision: "partial",
        items: [{ output_id: collectorItem.output_id, input_sha256: collectorItem.input_sha256, item_sha256: collectorItem.item_sha256, gate_decision: "hold", decision_reason: "approval_required" }],
      });
      fs.writeFileSync(path.join(root, "safe-output-items.jsonl"), JSON.stringify({ ...collectorItem, status: "success", gate_decision: "apply" }) + "\n");
      const writeGate = { ...base, GH_AW_WRITE_GATE: "partial", GH_AW_PENDING_WRITES_SHA256: manifestSha256, GH_AW_WRITE_GATE_DECISIONS_SHA256: inventorySha256 };
      const checked = collectUnifiedSession({ ...options, writeGate }).events.find(event => event.type === "gate.decision").data;
      expect(checked).toMatchObject({ conclusionCheck: "failed", counts: { apply: 0, hold: 1 } });
      fs.appendFileSync(inventoryFile, "\n");
      expect(collectUnifiedSession({ ...options, writeGate }).events.find(event => event.type === "gate.decision").data.conclusionCheck).toBe("failed");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
