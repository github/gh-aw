// @ts-check
"use strict";

import { afterEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { resolve, dispatch } = require("./continuation_relay.cjs");
const { checkpointArtifactPrefix } = require("./continuation_request.cjs");

const directories = [];
afterEach(() => {
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

const unexpired = { expired: false, expires_at: "2999-01-01T00:00:00Z" };

function fixture({ metadata = { workflow: "research", max: 3, policy: "workflow-dispatch" } } = {}) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "continuation-relay-"));
  directories.push(tempDir);
  const parent = {
    id: 100,
    run_attempt: 1,
    workflow_id: 42,
    head_branch: "feature",
    head_sha: "a".repeat(40),
    path: ".github/workflows/research.lock.yml",
    html_url: "https://github.com/owner/repo/actions/runs/100",
    created_at: "2026-01-01T00:00:00Z",
    repository: { id: 1, full_name: "owner/repo" },
    head_repository: { id: 1 },
    status: "in_progress",
  };
  const relay = {
    id: 300,
    workflow_id: 77,
    event: "workflow_dispatch",
    path: ".github/workflows/agentic-continuation-relay.yml",
    head_branch: "main",
    display_title: "Continuation relay 100/1",
    repository: { id: 1, full_name: "owner/repo" },
    head_repository: { id: 1 },
    actor: { login: "github-actions[bot]" },
    triggering_actor: { login: "github-actions[bot]" },
  };
  const request = { version: 1, hints: "Resume <step> 3", depth: 1, max: 3, parent_run_id: "100", parent_run_attempt: "1", aw_context: { run_id: "100", episode_id: "ep", continuation: { hints: "leak" } } };
  const parentArtifacts = [
    { id: 11, name: `${checkpointArtifactPrefix("research", parent)}100-1`, ...unexpired },
    { id: 12, name: "continuation-request-1", ...unexpired },
  ];
  const lock = `# gh-aw-metadata: ${JSON.stringify({ schema_version: "v4", continuation: metadata })}\nname: research\n`;
  const state = { parentArtifacts, relayArtifacts: [], relayRuns: [], lock, targetLock: lock, permission: "write" };
  const github = {
    rest: {
      actions: {
        getWorkflowRun: vi.fn(async ({ run_id }) => ({ data: String(run_id) === "300" ? relay : parent })),
        listWorkflowRunArtifacts: vi.fn(),
        listWorkflowRuns: vi.fn(),
        createWorkflowDispatch: vi.fn(async () => ({ status: 200, data: { workflow_run_id: 400 } })),
      },
      repos: {
        getContent: vi.fn(async ({ ref }) => ({ data: ref === "refs/heads/feature" ? state.targetLock : state.lock })),
        getCollaboratorPermissionLevel: vi.fn(async () => ({ data: { permission: state.permission } })),
        getEnvironment: vi.fn(async () => ({ data: { protection_rules: [{ type: "required_reviewers", reviewers: [{ type: "User" }] }] } })),
      },
    },
    paginate: vi.fn(async (method, args) => {
      if (method === github.rest.actions.listWorkflowRuns) return state.relayRuns;
      return String(args.run_id) === "300" ? state.relayArtifacts : state.parentArtifacts;
    }),
  };
  const summary = { addHeading: vi.fn(() => summary), addTable: vi.fn(() => summary), addRaw: vi.fn(() => summary), write: vi.fn(async () => {}) };
  const core = { setOutput: vi.fn(), info: vi.fn(), summary };
  const context = { runId: 300, eventName: "workflow_dispatch", actor: "github-actions[bot]", repo: { owner: "owner", repo: "repo" }, payload: { repository: { default_branch: "main" } } };
  const artifactClient = {
    downloadArtifact: vi.fn(async (_id, { path: destination }) => {
      fs.mkdirSync(destination, { recursive: true });
      fs.writeFileSync(path.join(destination, "continuation-request.json"), JSON.stringify(request));
    }),
  };
  const env = { GH_AW_PARENT_RUN_ID: "100", GH_AW_PARENT_RUN_ATTEMPT: "1", RUNNER_TEMP: tempDir };
  return { github, context, core, artifactClient, env, token: "token", parent, relay, request, state, tempDir, markerFile: path.join(tempDir, "marker.json") };
}

const outputs = core => Object.fromEntries(core.setOutput.mock.calls);

describe("continuation relay resolve", () => {
  it("validates the parent and exposes only trusted routing outputs", async () => {
    const options = fixture();
    await resolve(options);
    expect(outputs(options.core)).toEqual({ target_workflow: "research.lock.yml", ref: "feature", depth: "1", max: "3", approval_environment: "" });
    expect(options.github.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ path: ".github/workflows/research.lock.yml", ref: "a".repeat(40) }));
    expect(options.artifactClient.downloadArtifact).toHaveBeenCalledWith(12, expect.objectContaining({ findBy: { token: "token", repositoryOwner: "owner", repositoryName: "repo" } }));
    expect(JSON.stringify(options.core.summary.addRaw.mock.calls)).toContain("Resume &lt;step&gt; 3");
  });

  it.each([
    ["non-integer run id", options => (options.env.GH_AW_PARENT_RUN_ID = "1; rm"), "parent_run_id"],
    ["missing attempt", options => ((options.env.GH_AW_PARENT_RUN_ATTEMPT = "3"), (options.relay.display_title = "Continuation relay 100/3")), "has no attempt 3"],
    ["fork parent", options => (options.parent.head_repository.id = 2), "fork"],
    ["other repository", options => (options.parent.repository.full_name = "other/repo"), "this repository"],
    ["non-compiled workflow", options => (options.parent.path = ".github/workflows/ci.yml"), "not a compiled agentic workflow"],
    ["non-durable workflow", options => (options.state.lock = `# gh-aw-metadata: {"schema_version":"v4"}\n`), "does not enable"],
    ["unknown policy", options => (options.state.lock = `# gh-aw-metadata: {"continuation":{"workflow":"research","max":3,"policy":"schedule"}}\n`), "not supported"],
    ["missing request", options => options.state.parentArtifacts.splice(1, 1), "no unexpired continuation-request-1"],
    ["expired checkpoint", options => (options.state.parentArtifacts[0].expired = true), "no unexpired durable checkpoint"],
    ["depth above compiled max", options => (options.request.depth = 4), "exceeds the configured maximum of 3"],
    ["mismatched request", options => (options.request.parent_run_attempt = "2"), "does not belong"],
    ["duplicate relay", options => options.state.relayRuns.push({ id: 299, display_title: "Continuation relay 100/1", status: "in_progress" }), "already handled by relay run 299"],
    ["completed relay", options => options.state.relayRuns.push({ id: 298, display_title: "Continuation relay 100/1", status: "completed", conclusion: "success" }), "already handled"],
    ["relay rerun after dispatch", options => options.state.relayArtifacts.push({ name: "continuation-dispatched" }), "already dispatched"],
    ["non-dispatch event", options => (options.context.eventName = "push"), "only runs on workflow_dispatch"],
    ["relay from another branch", options => (options.relay.head_branch = "attacker"), "must run from the default branch"],
    ["relay copy at another path", options => (options.relay.path = ".github/workflows/copy.yml"), "must come from .github/workflows/agentic-continuation-relay.yml"],
    ["relay from a fork", options => (options.relay.head_repository.id = 9), "not a fork"],
    ["relay run name not bound to inputs", options => (options.relay.display_title = "Continuation relay 100/2"), "not bound to run 100 attempt 1"],
    ["foreign bot dispatcher", options => (options.relay.actor.login = "renovate[bot]"), "is not allowed to dispatch the relay"],
    ["read-only dispatcher", options => ((options.relay.actor.login = "reader"), (options.state.permission = "read")), "needs write access"],
    ["read-only rerunner", options => ((options.relay.triggering_actor.login = "reader"), (options.state.permission = "none")), "needs write access"],
    ["permission lookup failure", options => ((options.relay.actor.login = "user"), options.github.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(new Error("boom"))), "could not verify the repository permission"],
    ["branch no longer durable", options => (options.state.targetLock = `# gh-aw-metadata: {"schema_version":"v4"}\n`), "does not enable"],
    ["branch lowered max", options => ((options.request.depth = 2), (options.state.targetLock = `# gh-aw-metadata: {"continuation":{"workflow":"research","max":1,"policy":"workflow-dispatch"}}\n`)), "maximum of 1 now compiled"],
    ["branch renamed workflow", options => (options.state.targetLock = `# gh-aw-metadata: {"continuation":{"workflow":"other","max":3,"policy":"workflow-dispatch"}}\n`), "is now workflow"],
    [
      "branch changed approval",
      options => {
        options.state.lock = `# gh-aw-metadata: {"continuation":{"workflow":"research","max":3,"policy":"workflow-dispatch","approval":"a"}}\n`;
        options.state.targetLock = `# gh-aw-metadata: {"continuation":{"workflow":"research","max":3,"policy":"workflow-dispatch","approval":"b"}}\n`;
      },
      "approval environment changed",
    ],
  ])("fails closed for a %s", async (_name, mutate, message) => {
    const options = fixture();
    mutate(options);
    await expect(resolve(options)).rejects.toThrow(message);
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it("lets a user with write access dispatch the relay manually", async () => {
    const options = fixture();
    options.relay.actor.login = options.relay.triggering_actor.login = options.context.actor = "maintainer";
    await resolve(options);
    expect(options.github.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({ owner: "owner", repo: "repo", username: "maintainer" });
    expect(outputs(options.core).target_workflow).toBe("research.lock.yml");
  });

  it("enforces the stricter branch maximum and adopts a newly added approval gate", async () => {
    const options = fixture();
    options.state.targetLock = `# gh-aw-metadata: {"continuation":{"workflow":"research","max":2,"policy":"workflow-dispatch","approval":"continuation-review"}}\n`;
    await resolve(options);
    expect(outputs(options.core)).toMatchObject({ max: "2", approval_environment: "continuation-review" });
  });

  it("ignores failed duplicate relays so a relay can be retried", async () => {
    const options = fixture();
    options.state.relayRuns.push({ id: 299, display_title: "Continuation relay 100/1", status: "completed", conclusion: "failure" });
    await expect(resolve(options)).resolves.toBeUndefined();
  });

  it("requests approval through a protected environment", async () => {
    const options = fixture({ metadata: { workflow: "research", max: 3, policy: "workflow-dispatch", approval: "continuation-review" } });
    await resolve(options);
    expect(options.github.rest.repos.getEnvironment).toHaveBeenCalledWith({ owner: "owner", repo: "repo", environment_name: "continuation-review" });
    expect(outputs(options.core).approval_environment).toBe("continuation-review");
  });

  it.each([
    ["missing", options => options.github.rest.repos.getEnvironment.mockRejectedValue(Object.assign(new Error("Not Found"), { status: 404 })), "does not exist"],
    ["unprotected", options => options.github.rest.repos.getEnvironment.mockResolvedValue({ data: { protection_rules: [{ type: "wait_timer" }] } }), "no required reviewers"],
  ])("fails closed when the approval environment is %s", async (_name, mutate, message) => {
    const options = fixture({ metadata: { workflow: "research", max: 3, policy: "workflow-dispatch", approval: "continuation-review" } });
    mutate(options);
    await expect(resolve(options)).rejects.toThrow(message);
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });
});

describe("continuation relay dispatch", () => {
  it("dispatches the parent workflow on its branch with lineage but without hints", async () => {
    const options = fixture();
    await dispatch(options);
    const call = options.github.rest.actions.createWorkflowDispatch.mock.calls[0][0];
    expect(call).toMatchObject({ owner: "owner", repo: "repo", workflow_id: "research.lock.yml", ref: "refs/heads/feature" });
    const awContext = JSON.parse(call.inputs.aw_context);
    expect(awContext).toMatchObject({ repo: "owner/repo", run_id: "100", run_attempt: "1", relay_run_id: "300", episode_id: "ep", continuation: { depth: 1, max: 3 } });
    expect(call.inputs.aw_context).not.toContain("Resume");
    expect(call.inputs.aw_context).not.toContain("leak");
    expect(JSON.parse(fs.readFileSync(options.markerFile, "utf8"))).toEqual({ parent_run_id: "100", parent_run_attempt: "1", child_run_id: "400" });
    expect(options.core.setOutput).toHaveBeenCalledWith("child_run_id", "400");
  });

  it("refuses to dispatch an approval-gated continuation that was not approved", async () => {
    const options = fixture({ metadata: { workflow: "research", max: 3, policy: "workflow-dispatch", approval: "continuation-review" } });
    options.env.GH_AW_APPROVAL_RESULT = "skipped";
    await expect(dispatch(options)).rejects.toThrow("requires approval");
    expect(options.github.rest.actions.createWorkflowDispatch).not.toHaveBeenCalled();
  });

  it("dispatches an approved continuation", async () => {
    const options = fixture({ metadata: { workflow: "research", max: 3, policy: "workflow-dispatch", approval: "continuation-review" } });
    options.env.GH_AW_APPROVAL_RESULT = "success";
    await dispatch(options);
    expect(options.github.rest.actions.createWorkflowDispatch).toHaveBeenCalledTimes(1);
  });

  it("re-checks checkpoint expiry after approval", async () => {
    const options = fixture();
    options.state.parentArtifacts[0].expires_at = "2000-01-01T00:00:00Z";
    await expect(dispatch(options)).rejects.toThrow("no unexpired durable checkpoint");
    expect(options.github.rest.actions.createWorkflowDispatch).not.toHaveBeenCalled();
  });
});
