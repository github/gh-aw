import { describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { buildGateDecision } from "./write_gate_observability.cjs";
import { collectUnifiedSession } from "./unified_session.cjs";
import { renderUnifiedSession } from "./unified_session_render.cjs";
import { compactUnifiedSession } from "./compact_unified_session.cjs";
import { buildFailureMatchCategories } from "./handle_agent_failure.cjs";

const digest = "a".repeat(64);
const item = { output_id: "so_abcdefghijkl", item_sha256: digest, type: "create_issue" };
const manifest = { complete: true, items: [item] };
const base = { GH_AW_WRITE_GATE: "allow", GH_AW_WRITE_GATE_REASON: "policy", GH_AW_PENDING_WRITES_SHA256: digest, GH_AW_JOB_RESULTS: "{}", GH_AW_WRITE_GATE_WRITE_JOB_NAMES: "[]" };
const build = (env = {}, results = [], extra = {}) => buildGateDecision({ env: { ...base, ...env }, results, manifest, actualDigest: digest, resultsComplete: true, ...extra });

describe("compiler-owned gate conclusion evidence", () => {
  it.each([
    ["allow", [{ ...item, status: "success", gate_decision: "apply" }], "verified"],
    ["deny", [{ ...item, status: "skipped", gate_signal: "gate_denied" }], "verified"],
    ["partial", [{ ...item, status: "skipped", gate_decision: "hold", decision_reason: "gate_held" }], "verified"],
    ["", [], "verified"],
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
          "conclusionCheck": "verified",
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
          "conclusionCheck": "verified",
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
          "conclusionCheck": "verified",
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
          "conclusionCheck": "verified",
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

      [1] write_gate/conclusion gate.decision verdict=allow reason=policy manifestDigest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa integrity=verified conclusionCheck=verified counts={   "apply": 0,   "hold": 0,   "review": 0,   "stage": 0,   "drop": 0 } items=[] [untimed]
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

      [1] write_gate/conclusion gate.decision verdict=deny reason=policy manifestDigest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa integrity=verified conclusionCheck=verified counts={   "apply": 0,   "hold": 0,   "review": 0,   "stage": 0,   "drop": 0 } items=[] [untimed]
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

      [1] write_gate/conclusion gate.decision verdict=partial reason=policy manifestDigest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa integrity=verified conclusionCheck=verified counts={   "apply": 0,   "hold": 0,   "review": 0,   "stage": 0,   "drop": 0 } items=[] [untimed]
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

      [1] write_gate/conclusion gate.decision verdict=deny reason=gate_error manifestDigest=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa integrity=verified conclusionCheck=verified counts={   "apply": 0,   "hold": 0,   "review": 0,   "stage": 0,   "drop": 0 } items=[] [untimed]
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
    expect(build({ GH_AW_WRITE_GATE: "deny" }, [], { manifest: { complete: false, items: [] } })).toMatchObject({ integrity: "unknown", conclusionCheck: "unknown" });
  });

  it("never promotes unknown or failed jobs and missing result/digest evidence to verified", () => {
    expect(build({}, [], { resultsComplete: false }).conclusionCheck).toBe("unknown");
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

  it("collects trusted events with bounded summary/compact metadata and leaves ungated collection unchanged", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gate-observability-"));
    try {
      const encoded = JSON.stringify(manifest);
      fs.writeFileSync(path.join(root, "pending-writes.json"), encoded);
      fs.writeFileSync(path.join(root, "safe-output-items.jsonl"), JSON.stringify({ ...item, status: "success", gate_decision: "apply", title: "PRIVATE_TITLE" }) + "\n");
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
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
