// @ts-check
import { describe, expect, it, vi } from "vitest";
import {
  API_VERSION,
  immutableRef,
  nativeId,
  nativeAttempt,
  hasDispatchToken,
  dispatchResponse,
  fetchNativeRunAttempt,
  postQueueDispatch,
  validateNativeRun,
  authenticatePublisher,
  authenticateIntentPublisher,
} from "./work_queue_native.cjs";
import { requestForIntent } from "./work_queue_intents.cjs";

const ref = "a".repeat(40);
const expected = { repository: "owner/repo", repository_id: "7", workflow: ".github/workflows/worker.lock.yml", workflow_id: "9", ref, principal_id: "11", dispatch_id: "d1" };
const run = {
  id: "9007199254740993",
  run_attempt: 1,
  repository: { full_name: "owner/repo", id: 7 },
  workflow_id: 9,
  path: expected.workflow,
  event: "workflow_dispatch",
  head_sha: ref,
  actor: { id: 11 },
  triggering_actor: { id: 11 },
  display_title: "queue d1",
  status: "in_progress",
  conclusion: null,
};

describe("authenticated native queue runs", () => {
  const nativeClient = (actions, repository = run.repository) => ({
    rest: { repos: { get: vi.fn().mockResolvedValue({ status: 200, data: repository }) }, actions },
  });
  it("matches the 256-digit positive canonical ID boundary without numeric coercion", () => {
    const id = "9".repeat(256);
    expect(nativeId(id)).toBe(id);
    for (const invalid of ["9".repeat(257), "0", "01", "+1", "-1", "1.0", 1.5, Number.MAX_SAFE_INTEGER + 1]) expect(() => nativeId(invalid)).toThrow();
  });

  it("authenticates new dispatcher intent origins on bounded later attempts without granting worker rerun authority", async () => {
    const context = { repo: { owner: "owner", repo: "repo" }, runId: run.id, runAttempt: 2, actorId: "11", sha: ref, eventName: "workflow_dispatch", payload: { repository: { id: 7 } } };
    const options = {
      context,
      workflowRef: `${expected.repository}/${expected.workflow}@${ref}`,
      githubClient: nativeClient({ getWorkflowRun: vi.fn().mockResolvedValue({ status: 200, data: { ...run, run_attempt: 2, created_at: "2026-10-05T00:00:00Z" } }) }),
    };
    expect(await authenticatePublisher({ ...options, role: "dispatcher" })).toMatchObject({ authenticated: true, role: "dispatcher", run_attempt: 2, run_id: run.id, principal: "11" });
    await expect(authenticatePublisher({ ...options, role: "worker" })).rejects.toThrow(/rerun/);
    await expect(authenticatePublisher({ ...options, role: "dispatcher", context: { ...context, runAttempt: 3 } })).rejects.toThrow(/rerun/);
    expect(nativeAttempt("4096")).toBe(4096);
    for (const value of ["4097", "1".repeat(256), 0, "01"]) expect(() => nativeAttempt(value)).toThrow();
  });

  it("uses the actual repository API identity for native proofs and publisher Actors regardless of context casing", async () => {
    const canonicalRepository = { full_name: "Owner/Repo", id: 7 };
    const githubClient = nativeClient({ getWorkflowRun: vi.fn().mockResolvedValue({ status: 200, data: run }), getWorkflowRunAttempt: vi.fn().mockResolvedValue({ status: 200, data: run }) }, canonicalRepository);
    const context = { repo: { owner: "OWNER", repo: "REPO" }, runId: run.id, runAttempt: 1, actorId: "11", sha: ref, eventName: "workflow_dispatch", payload: { repository: { id: 7 } } };
    const authenticated = await authenticatePublisher({ role: "worker", context, workflowRef: `owner/repo/${expected.workflow}@${ref}`, githubClient });
    expect(authenticated.repository).toBe(canonicalRepository.full_name);
    expect(authenticated.native_run.repository).toEqual(canonicalRepository);
    const proof = validateNativeRun(await fetchNativeRunAttempt(githubClient, "OWNER/REPO", run.id), expected);
    expect(proof.repository).toBe(canonicalRepository.full_name);
    expect(githubClient.rest.repos.get.mock.calls[0][0]).toMatchObject({ owner: "OWNER", repo: "REPO", headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } });
    expect(githubClient.rest.actions.getWorkflowRun.mock.calls[0][0]).toMatchObject({ owner: "Owner", repo: "Repo" });
  });

  it("requires a successful actual repository read and matching lossless run repository ID before granting authority", async () => {
    const context = { repo: { owner: "owner", repo: "repo" }, runId: run.id, runAttempt: 1, actorId: "11", sha: ref, eventName: "workflow_dispatch" };
    const options = { role: "worker", context, workflowRef: `${expected.repository}/${expected.workflow}@${ref}` };
    const missing = { rest: { actions: { getWorkflowRun: vi.fn().mockResolvedValue({ status: 200, data: run }) } } };
    await expect(authenticatePublisher({ ...options, githubClient: missing })).rejects.toThrow(/repository_api_missing/);
    expect(missing.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
    for (const identity of [
      { full_name: "foreign/repo", id: 7 },
      { full_name: "owner/repo", id: 8 },
      { full_name: "owner/repo", id: Number.MAX_SAFE_INTEGER + 1 },
    ]) {
      const githubClient = nativeClient({ getWorkflowRun: vi.fn().mockResolvedValue({ status: 200, data: run }) }, identity);
      await expect(authenticatePublisher({ ...options, githubClient })).rejects.toThrow();
    }
    const githubClient = nativeClient({ getWorkflowRun: vi.fn().mockResolvedValue({ status: 200, data: run }) });
    for (const status of [undefined, 403, 404, 500]) {
      githubClient.rest.repos.get.mockResolvedValue({ status, data: run.repository });
      await expect(authenticatePublisher({ ...options, githubClient })).rejects.toThrow(/repository_identity_mismatch/);
    }
    expect(githubClient.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
  });

  it("recovers a protected original intent origin through its exact native attempt without changing request identity", async () => {
    const context = { repo: { owner: "owner", repo: "repo" }, runId: run.id, runAttempt: 2, actorId: "11", sha: ref, eventName: "workflow_dispatch", payload: { repository: { id: 7 } } };
    const origin = { role: "dispatcher", principal: "11", repository: "owner/repo", workflow: expected.workflow, run_id: run.id, run_attempt: 1 };
    const getAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...run, created_at: "2026-10-05T00:00:00Z" } });
    const options = {
      role: "dispatcher",
      context,
      workflowRef: `${expected.repository}/${expected.workflow}@${ref}`,
      intentOrigin: JSON.stringify(origin),
      githubClient: nativeClient({ getWorkflowRun: vi.fn().mockResolvedValue({ status: 200, data: { ...run, run_attempt: 2, created_at: "2026-10-05T00:00:00Z" } }), getWorkflowRunAttempt: getAttempt }),
    };
    const recovered = await authenticateIntentPublisher(options);
    expect(recovered).toMatchObject({ authenticated: true, run_attempt: 1, publisher_attempt: 2 });
    expect(getAttempt.mock.calls[0][0]).toMatchObject({ run_id: run.id, attempt_number: 1, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0 } });
    const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 1000 };
    expect(requestForIntent(recovered, "same-serialized-intent", "dispatch_next", parameters)).toEqual(requestForIntent({ ...origin, authenticated: true, roles: ["dispatcher"] }, "same-serialized-intent", "dispatch_next", parameters));
    await expect(authenticateIntentPublisher({ ...options, intentOrigin: undefined })).rejects.toThrow(/origin_required/);
    for (const changed of [
      { ...origin, principal: "12" },
      { ...origin, run_attempt: 3 },
      { ...origin, repository: "foreign/repo" },
      { ...origin, workflow: ".github/workflows/other.lock.yml" },
      { ...origin, role: "administrator" },
      { ...origin, run_id: "43" },
      { ...origin, actor: origin },
    ])
      await expect(authenticateIntentPublisher({ ...options, intentOrigin: changed })).rejects.toThrow();
    getAttempt.mockResolvedValue({ status: 200, data: { ...run, run_attempt: 2 } });
    await expect(authenticateIntentPublisher(options)).rejects.toThrow(/rerun/);
    getAttempt.mockResolvedValue({ status: 403, data: run });
    await expect(authenticateIntentPublisher(options)).rejects.toThrow(/read_failed/);
    await expect(authenticateIntentPublisher({ ...options, role: "worker" })).rejects.toThrow(/rerun/);
  });

  it("accepts genuinely new protected later-attempt origins without reinterpreting them as original attempts", async () => {
    const context = { repo: { owner: "owner", repo: "repo" }, runId: run.id, runAttempt: 2, actorId: "11", sha: ref, eventName: "workflow_dispatch", payload: { repository: { id: 7 } } };
    const origin = { role: "dispatcher", principal: "11", repository: "owner/repo", workflow: expected.workflow, run_id: run.id, run_attempt: 2 };
    const options = {
      role: "dispatcher",
      context,
      workflowRef: `${expected.repository}/${expected.workflow}@${ref}`,
      intentOrigin: origin,
      githubClient: nativeClient({ getWorkflowRun: vi.fn().mockResolvedValue({ status: 200, data: { ...run, run_attempt: 2, created_at: "2026-10-05T00:00:00Z" } }) }),
    };
    expect(await authenticateIntentPublisher(options)).toMatchObject({ authenticated: true, run_attempt: 2, publisher_attempt: 2 });
  });

  it("preserves lossless IDs and refuses rounded or noncanonical IDs", () => {
    expect(nativeId("9007199254740993")).toBe("9007199254740993");
    expect(nativeId("1".repeat(256))).toHaveLength(256);
    for (const id of [9007199254740993, 0, -1, "01", "1e3", null, "", "1".repeat(257)]) expect(() => nativeId(id)).toThrow();
  });

  it("requires exact repository, workflow, immutable ref, event and principal", () => {
    expect(validateNativeRun(run, expected)).toMatchObject({ run_id: run.id, run_attempt: 1, terminal: false });
    for (const override of [
      { run_attempt: 2 },
      { repository: { full_name: "elsewhere/repo", id: 7 } },
      { repository: { full_name: "owner/repo", id: 8 } },
      { workflow_id: 10 },
      { path: ".github/workflows/other.yml" },
      { event: "repository_dispatch" },
      { head_sha: "b".repeat(40) },
      { actor: { id: 12 } },
      { triggering_actor: { id: 12 } },
      { display_title: "queue forged" },
    ])
      expect(() => validateNativeRun({ ...run, ...override }, expected)).toThrow();
    expect(validateNativeRun({ ...run, status: "completed", conclusion: "cancelled" }, expected).terminal).toBe(true);
    expect(validateNativeRun({ ...run, status: "completed" }, expected).terminal).toBe(false);
    expect(validateNativeRun({ ...run, display_title: "custom worker d1 diagnostic suffix" }, expected).run_id).toBe(run.id);
  });

  it("accepts canonical SHA-1 and SHA-256 immutable revisions, never branches or truncated digests", () => {
    for (const length of [40, 64]) {
      const revision = "a".repeat(length);
      expect(immutableRef(revision)).toBe(revision);
      expect(validateNativeRun({ ...run, head_sha: revision }, { ...expected, ref: revision }).run_id).toBe(run.id);
    }
    for (const revision of ["main", "refs/heads/main", "a".repeat(39), "a".repeat(63), "A".repeat(64)]) expect(() => immutableRef(revision)).toThrow();
  });

  it("requires a complete dispatch identity token, not a sibling prefix or embedded substring", () => {
    for (const title of ["queue d10", "queue notd1", "queue _d1", "queue d1_suffix", "queue d1suffix"]) {
      expect(() => validateNativeRun({ ...run, display_title: title }, expected)).toThrow(/run_correlation_mismatch/);
    }
    for (const title of ["gh-aw work-queue d1", "queue (d1)", "diagnostic: d1, ready", "d1"]) expect(validateNativeRun({ ...run, display_title: title }, expected).run_id).toBe(run.id);
    const dispatchId = `d_${"a".repeat(64)}_1`;
    for (const title of [`gh-aw work-queue ${dispatchId}`, `custom worker (${dispatchId}) diagnostic suffix`, `prefix:[${dispatchId}]:suffix`]) {
      expect(validateNativeRun({ ...run, display_title: title }, { ...expected, dispatch_id: dispatchId }).run_id).toBe(run.id);
    }
    for (const title of [`gh-aw work-queue ${dispatchId}0`, `${dispatchId}_foreign`, `prefix_${dispatchId}`]) {
      expect(() => validateNativeRun({ ...run, display_title: title }, { ...expected, dispatch_id: dispatchId })).toThrow(/run_correlation_mismatch/);
    }
  });

  it("escapes dispatch tokens literally and never treats regex characters or adjacent identifier characters as correlation", () => {
    const dispatchId = "d.[x](y)+$^|\\token";
    expect(hasDispatchToken(`diagnostic (${dispatchId}), ready`, dispatchId)).toBe(true);
    for (const title of ["unrelated", "dQxyyyyytoken", `${dispatchId}suffix`, `prefix${dispatchId}`, `${dispatchId}_suffix`]) expect(hasDispatchToken(title, dispatchId)).toBe(false);
    for (const [title, token] of [
      [null, "d1"],
      ["d1", null],
      ["d1", ""],
      ["D1", "d1"],
    ])
      expect(hasDispatchToken(title, token)).toBe(false);
  });

  it("requires the pinned modern response and exact URLs", () => {
    const response = { status: 200, data: { workflow_run_id: run.id, run_url: `https://api.github.com/repos/owner/repo/actions/runs/${run.id}`, html_url: `https://github.com/owner/repo/actions/runs/${run.id}` } };
    expect(dispatchResponse(response, expected).run_id).toBe(run.id);
    for (const change of [{ status: 204 }, { data: { ...response.data, run_url: "https://evil.example/run" } }, { data: { ...response.data, html_url: `${response.data.html_url}?token=bad` } }, { data: { workflow_run_id: run.id } }]) {
      expect(() => dispatchResponse({ ...response, ...change }, expected)).toThrow();
    }
  });

  it("sends one POST with retries disabled and never tries old API fallback", async () => {
    const post = vi.fn().mockRejectedValue(Object.assign(new Error("return_run_details unsupported"), { status: 422 }));
    await expect(postQueueDispatch({ rest: { actions: { createWorkflowDispatch: post } } }, expected, {})).rejects.toThrow();
    expect(post).toHaveBeenCalledTimes(1);
    expect(post.mock.calls[0][0]).toMatchObject({ ref, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, retryCount: 0 } });
    expect(post.mock.calls[0][0]).not.toHaveProperty("return_run_details");
  });

  it("reads exact attempt-one terminal evidence without authorizing a later rerun", async () => {
    const getAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...run, status: "completed", conclusion: "cancelled" } });
    const proof = validateNativeRun(await fetchNativeRunAttempt(nativeClient({ getWorkflowRunAttempt: getAttempt }), expected.repository, run.id), expected);
    expect(proof).toMatchObject({ run_attempt: 1, terminal: true });
    expect(getAttempt.mock.calls[0][0]).toMatchObject({ run_id: run.id, attempt_number: 1, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0 } });
    expect(() => validateNativeRun({ ...run, run_attempt: 2 }, expected)).toThrow(/rerun/);
  });
});
