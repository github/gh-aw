// @ts-check
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import path from "node:path";
import { fixture, mock } from "./work_queue_issues_checks.cjs";
import { main as projectIssues, ownProjectionTargets, projectBatch, journalPath } from "./work_queue_issues.cjs";
import { assertProjectionAuthority } from "./work_queue_issue_contract.cjs";
import { newRequest, replayTransactions, compactTransactions } from "./work_queue_replay.cjs";
import { canonical } from "./work_queue_codec.cjs";
import { parseSettings } from "./work_queue_settings.cjs";

function awIssueFixture(worker = false) {
  const f = fixture({ count: 2, backing: false, worker });
  const policy = f.log[0].operations[0].policy;
  policy.authorization = "aw";
  policy.producers = {};
  delete policy.projectors;
  delete policy.pools.default.profiles.default.principal;
  for (const commit of f.log) {
    for (const operation of commit.operations) {
      if (operation.kind !== "Dispatch") continue;
      if (operation.state === "started") operation.credential_principal = "2002";
      if (operation.state === "bound") {
        operation.run.principal = "2002";
        operation.evidence.principal = "2002";
        commit.actor.principal = "2002";
      }
    }
    if (["policy", "dispatch"].includes(commit.request.kind)) commit.request = newRequest(commit.request.id, commit.request.kind, commit.actor, { operations: commit.operations });
  }
  if (worker) f.origin.principal = "2002";
  return f;
}

describe("AW Issue projection without projector enrollment", () => {
  let prompts;
  let issues;
  beforeEach(() => {
    prompts = process.env.GH_AW_PROMPTS_DIR;
    issues = process.env.GH_AW_WORK_QUEUE_ISSUES;
    process.env.GH_AW_PROMPTS_DIR = path.resolve(import.meta.dirname, "../md");
  });
  afterEach(() => {
    if (prompts === undefined) delete process.env.GH_AW_PROMPTS_DIR;
    else process.env.GH_AW_PROMPTS_DIR = prompts;
    if (issues === undefined) delete process.env.GH_AW_WORK_QUEUE_ISSUES;
    else process.env.GH_AW_WORK_QUEUE_ISSUES = issues;
  });

  it("uses global aw.json issues/label configuration for native-authenticated admission and original Claim hooks", async () => {
    process.env.GH_AW_WORK_QUEUE_ISSUES = JSON.stringify(parseSettings({ issues: { label: "queue work" } }).issues);
    for (const worker of [false, true]) {
      const f = awIssueFixture(worker);
      const m = mock(f);
      const send = m.options.githubClient.graphql;
      m.options.githubClient.graphql = async (query, variables) => {
        const result = await send(query, variables);
        if (query.includes("WorkQueueIssueTarget")) result.repository.label = m.labels.get(variables.label) || null;
        return result;
      };
      const options = {
        ...m.options,
        trustedContext: undefined,
        context: { ...m.options.context, runId: f.origin.run_id, runAttempt: 1, sha: f.origin.ref, eventName: "workflow_dispatch" },
        workflowRef: `owner/repo/${f.origin.workflow}@${f.origin.ref}`,
      };
      delete options.issues;
      expect(await projectIssues(options)).toMatchObject({ pending: [] });
      expect(m.calls.some(call => call[0] === "authentication-run")).toBe(true);
      expect(m.state().policy).not.toHaveProperty("projectors");
      expect(m.issues.size).toBe(2);
      expect(m.comments.size).toBe(worker ? 4 : 2);
      for (const issue of m.issues.values()) {
        expect(issue.labels.nodes.map(label => label.name)).toEqual(expect.arrayContaining(["queue work", worker ? "queue-work:running" : "queue-work:queued"]));
        expect(issue.body).toContain("queue work");
      }
      expect(await projectIssues(options)).toMatchObject({ pending: [] });
      expect(m.issues.size).toBe(2);
    }
  });

  it("globally disables Issue projection without authenticating or mutating anything", async () => {
    process.env.GH_AW_WORK_QUEUE_ISSUES = "false";
    const m = mock(awIssueFixture());
    const options = { ...m.options };
    delete options.issues;
    expect(await projectIssues(options)).toEqual({ disabled: true });
    expect(m.calls).toEqual([]);
  });

  it.each([
    ["initiating-principal", "run_principal_mismatch"],
    ["triggering-principal", "run_principal_mismatch"],
    ["correlation", "run_correlation_mismatch"],
    ["event", "run_event_mismatch"],
    ["revision", "run_ref_mismatch"],
  ])("refuses native worker %s drift before Issue mutation despite an otherwise matching originating context", async (failure, message) => {
    const f = awIssueFixture(true);
    const m = mock(f);
    if (failure === "initiating-principal") m.nativeRun.actor.id = 1001;
    if (failure === "triggering-principal") m.nativeRun.triggering_actor = { id: 1001 };
    if (failure === "correlation") m.nativeRun.display_title = "unrelated run";
    if (failure === "event") m.nativeRun.event = "schedule";
    if (failure === "revision") m.nativeRun.head_sha = "a".repeat(40);
    const options = {
      ...m.options,
      trustedContext: undefined,
      context: { ...m.options.context, runId: f.origin.run_id, runAttempt: 1, sha: m.nativeRun.head_sha, eventName: m.nativeRun.event },
      workflowRef: `owner/repo/${f.origin.workflow}@${m.nativeRun.head_sha}`,
    };
    const before = canonical(m.state().transactions);
    const result = await projectIssues(options);
    expect(result.pending).toEqual([{ reason: message }]);
    expect(m.calls.some(call => call[0] === "mutation" || call[0] === "lock")).toBe(false);
    expect(m.issues.size).toBe(0);
    expect(m.journals.size).toBe(0);
    expect(canonical(m.state().transactions)).toBe(before);
  });

  it("creates and synchronizes tracking Issues for a trusted producer's own admissions without installed projector rules", async () => {
    const f = awIssueFixture();
    const m = mock(f);
    expect(await projectIssues(m.options)).toMatchObject({ pending: [] });
    expect(m.issues.size).toBe(2);
    expect(m.comments.size).toBe(2);
    expect(m.state().policy).not.toHaveProperty("projectors");
    for (const node of f.nodes) {
      expect(m.state().works.get(node.work_id).issue_link).toBeDefined();
      expect(m.state().works.get(node.work_id).issue_summary).toBeDefined();
    }
    expect(await projectIssues(m.options)).toMatchObject({ pending: [] });
    expect(m.issues.size).toBe(2);
  });

  it("creates original worker Claim comments using the actual credential-bound principal, not projector enrollment", async () => {
    const f = awIssueFixture(true);
    const m = mock(f);
    expect(await projectIssues(m.options)).toMatchObject({ pending: [] });
    expect(m.issues.size).toBe(2);
    expect(m.comments.size).toBe(4);
    for (const member of f.assignment.claims) {
      expect(m.state().claims.get(member.claim_id).issue_comment).toBeDefined();
      expect(m.state().works.get(member.work_id).issue_summary).toBeDefined();
      expect(m.state().dispatches.get(f.assignment.dispatch_id).profile).not.toHaveProperty("principal");
    }
    for (const change of [{ principal: "1001" }, { run_id: "999" }]) {
      expect(() => ownProjectionTargets(m.state(), { ...f.origin, ...change }, f.origin.ref, f.assignment)).toThrow(/original authenticated Claims/);
    }
    expect(() => ownProjectionTargets(m.state(), { ...f.origin, run_attempt: 2 }, f.origin.ref, f.assignment)).toThrow(/only native attempt 1/);
    const actor = { ...f.origin, role: "projector" };
    expect(() => assertProjectionAuthority(m.state(), actor, f.nodes[0].work_id, f.origin.ref, "owner/repo", f.assignment.claims[1].claim_id)).toThrow(/original authenticated Claims/);
  });

  it("does not authorize another run's admissions or unrelated Work without an original bound Claim", () => {
    const f = awIssueFixture();
    const state = replayTransactions(f.log);
    const actor = { ...f.origin, role: "projector" };
    for (const change of [{ principal: "33" }, { workflow: ".github/workflows/other.lock.yml" }, { run_id: "999" }, { run_attempt: 2 }]) {
      expect(() => assertProjectionAuthority(state, { ...actor, ...change }, f.nodes[0].work_id, f.origin.ref, "owner/repo")).toThrow(/checked admissions/);
    }
    expect(() => assertProjectionAuthority(state, actor, f.nodes[0].work_id, f.origin.ref, "foreign/repository")).toThrow(/installed projector authority/);
    expect(() => assertProjectionAuthority(state, actor, f.nodes[0].work_id, "main", "owner/repo")).toThrow(/immutable compiled/);
    expect(() => assertProjectionAuthority(state, { ...actor, workflow: "ordinary.yml" }, f.nodes[0].work_id, f.origin.ref, "owner/repo")).toThrow(/immutable compiled/);
  });

  it("retains exact-target enrollment requirements for pre-existing backing Issues rather than acquiring them from AW admission", () => {
    const f = awIssueFixture();
    const backingIssue = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "999", number: "99" };
    f.log[1].operations[0].backing_issue = backingIssue;
    f.log[1].request = newRequest(f.log[1].request.id, "submit", f.log[1].actor, { nodes: f.log[1].operations });
    expect(() => replayTransactions(f.log)).toThrow(/installed projector exact-target grant/);
  });

  it("rejects caller-supplied existing Issue targets before any native mutation without checked bindings or creation receipts", async () => {
    const f = awIssueFixture();
    const m = mock(f);
    const initial = await m.options.readCheckedQueue();
    const targets = ownProjectionTargets(initial.state, f.origin, f.origin.ref, f.assignment).targets;
    targets[0].resource = fixture().nodes[0].backing_issue;
    await expect(projectBatch(m.options, initial, f.origin, f.assignment, { label: "work" }, targets)).rejects.toThrow(/targets changed/);
    expect(m.calls.some(call => call[0] === "mutation")).toBe(false);
    expect(m.state().works.get(f.nodes[0].work_id).issue_link).toBeUndefined();
  });

  it("requires lossless native receipt evidence before binding an Issue and retains ambiguous creation fencing", async () => {
    const f = awIssueFixture();
    const m = mock(f);
    const send = m.options.githubClient.graphql;
    let creates = 0;
    m.options.githubClient.graphql = async (query, variables) => {
      const result = await send(query, variables);
      if (query.includes("WorkQueueIssueProjection") && query.includes("createIssue")) {
        creates++;
        delete result.m0.issue.databaseId;
      }
      return result;
    };
    const result = await projectIssues(m.options);
    expect(result.pending.some(item => /lossless native identity/.test(item.reason))).toBe(true);
    expect(creates).toBe(1);
    const work = m.state().works.get(f.nodes[0].work_id);
    expect(work.issue_link).toBeUndefined();
    expect(m.journals.get(journalPath(work.work_id)).create.resource).toBeUndefined();
    expect(m.state().works.get(f.nodes[1].work_id).issue_link).toBeDefined();
    expect((await projectIssues(m.options)).pending.length).toBeGreaterThan(0);
    expect(creates).toBe(1);
  });

  it("preserves own admission and original Claim projection authority through checkpoints", async () => {
    for (const worker of [false, true]) {
      const f = awIssueFixture(worker);
      const m = mock(f);
      expect(await projectIssues(m.options)).toMatchObject({ pending: [] });
      const before = m.state();
      const actor = { role: "administrator", principal: "1001", repository: "owner/repo" };
      const checkpoint = compactTransactions(before.transactions, "a".repeat(40), actor, 100);
      const restored = replayTransactions(checkpoint);
      expect(canonical(ownProjectionTargets(restored, f.origin, f.origin.ref, f.assignment))).toBe(canonical(ownProjectionTargets(before, f.origin, f.origin.ref, f.assignment)));
      m.setLog(checkpoint);
      expect(await projectIssues(m.options)).toMatchObject({ pending: [] });
      expect(m.issues.size).toBe(2);
    }
  });
});
