import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");

describe("work_queue_test_helpers", () => {
  it("builds a Work operation with defaults and overrides", () => {
    const node = h.work("a");
    expect(node).toMatchObject({ kind: "Work", graph_id: "graph", node_key: "a", pool: "default", worker_profile: "worker", depends_on: [] });
    expect(h.work("a", { graph_id: "other", priority: 1 })).toMatchObject({ graph_id: "other", priority: 1 });
    expect(h.work("a", { graph_id: "other" }).work_id).not.toBe(node.work_id);
  });

  it("builds worker actors", () => {
    expect(h.workerActor()).toEqual({ role: "worker", repository: h.repository, workflow: h.WORKER_WORKFLOW, run_id: "200", run_attempt: 1 });
    expect(h.workerActor("7", 2).run_attempt).toBe(2);
  });

  it("builds workflow runs with overrides", () => {
    const run = h.workflowRun("5", { event: "push" });
    expect(run).toMatchObject({ id: 5, path: h.WORKER_WORKFLOW, event: "push", head_sha: h.WORKER_REVISION });
  });

  it("merges dispatch parameter overrides", () => {
    expect(h.dispatchParameters({ max_claims: 1 })).toMatchObject({ pool: "default", max_claims: 1, max_dispatches: 16, profiles: h.PROFILES });
  });

  it("serializes a log as canonical JSONL", () => {
    const log = h.submit([], [h.work("a")]);
    const lines = h.ledgerText(log).split("\n");
    expect(lines.pop()).toBe("");
    expect(lines).toHaveLength(log.length);
    expect(JSON.parse(lines[0])).toEqual(JSON.parse(JSON.stringify(log[0])));
  });

  it("submits, dispatches and launches work", () => {
    let log = h.submit([], [h.work("a")]);
    const { log: dispatched, decision } = h.dispatchNext(log);
    expect(dispatched.length).toBe(log.length + 1);
    expect(decision.operations.length).toBeGreaterThan(0);
  });

  it("returns the log unchanged when there is nothing to dispatch", () => {
    const { log, decision } = h.dispatchNext([]);
    expect(log).toEqual([]);
    expect(decision.operations).toEqual([]);
  });

  it("fakeActions creates and serves runs", async () => {
    const { actions, runs, posts } = h.fakeActions();
    const created = await actions.createWorkflowDispatch({ workflow_id: "worker.lock.yml" });
    expect(created.data.workflow_run_id).toBe(200);
    expect(posts).toHaveLength(1);
    expect(runs.get("200").path).toBe(".github/workflows/worker.lock.yml");
    expect((await actions.getWorkflowRun({ run_id: 200 })).data.id).toBe(200);
    await expect(actions.getWorkflowRun({ run_id: 999 })).rejects.toMatchObject({ status: 404 });
  });

  it("fakeQueueGitHub installs and reads the queue branch", async () => {
    const fake = h.fakeQueueGitHub("hello");
    expect(fake.contents()).toBe("hello");
    fake.install("world");
    expect(fake.contents()).toBe("world");
    const { rest } = fake.githubClient;
    await expect(rest.git.getRef({ ref: "heads/missing" })).rejects.toMatchObject({ status: 404 });
    expect((await rest.git.getRef({ ref: "heads/main" })).data.object.sha).toBe("a".repeat(40));
  });

  it("fakeQueueGitHub rejects forced and non-fast-forward updates", async () => {
    const fake = h.fakeQueueGitHub("one");
    const { git } = fake.githubClient.rest;
    const head = fake.refs.get("work-queue");
    const unrelated = fake.install("two", []);
    fake.refs.set("work-queue", head);
    await expect(git.updateRef({ ref: "heads/work-queue", sha: unrelated, force: true })).rejects.toThrow(/force/);
    await expect(git.updateRef({ ref: "heads/work-queue", sha: unrelated, force: false })).rejects.toMatchObject({ status: 422 });
  });

  it("fakeQueueGitHub loses one update response when ambiguousOnce is set", async () => {
    const fake = h.fakeQueueGitHub("one");
    const { git } = fake.githubClient.rest;
    const child = fake.commits.size ? [...fake.commits.keys()][0] : "";
    const next = (await git.createCommit({ tree: fake.commits.get(child).tree, parents: [child] })).data.sha;
    fake.state.ambiguousOnce = true;
    await expect(git.updateRef({ ref: "heads/work-queue", sha: next, force: false })).rejects.toMatchObject({ status: 502 });
    expect(fake.refs.get("work-queue")).toBe(next);
    expect(fake.state.ambiguousOnce).toBe(false);
  });
});
