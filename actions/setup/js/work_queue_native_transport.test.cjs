import { afterEach, describe, expect, it, vi } from "vitest";
import { getOctokit } from "@actions/github";
import { fetchNativeRun, fetchNativeRunAttempt, authenticateIntentPublisher } from "./work_queue_native.cjs";

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function fixture(endpoint, failure, failures = 1) {
  let attempts = 0;
  const fetch = vi.fn(async (url, options) => {
    const path = new URL(url).pathname;
    expect(options.method).toBe("GET");
    expect(options.signal).toBeInstanceOf(AbortSignal);
    expect(options.signal.aborted).toBe(false);
    const target = path === "/repos/owner/repo" ? "repository" : path.endsWith("/attempts/1") ? "attempt" : "run";
    expect(path).toBe(target === "repository" ? "/repos/owner/repo" : `/repos/owner/repo/actions/runs/42${target === "attempt" ? "/attempts/1" : ""}`);
    if (target === (endpoint === "origin" ? "attempt" : endpoint) && ++attempts <= failures) return failure(options);
    const repository = { full_name: "owner/repo", id: 7 };
    const data =
      target === "repository"
        ? repository
        : {
            id: 42,
            run_attempt: endpoint === "origin" && target === "run" ? 2 : 1,
            repository,
            head_sha: "a".repeat(40),
            path: ".github/workflows/worker.lock.yml",
            event: "workflow_dispatch",
            actor: { id: 11 },
            triggering_actor: { id: 11 },
            created_at: "2026-10-05T00:00:00Z",
          };
    return new Response(JSON.stringify(data), { status: 200, headers: { "content-type": "application/json" } });
  });
  const githubClient = getOctokit("fixture-token", { request: { fetch } });
  const sleepFn = vi.fn(async () => {});
  const read =
    endpoint === "origin"
      ? () =>
          authenticateIntentPublisher({
            githubClient,
            sleepFn,
            role: "dispatcher",
            context: { repo: { owner: "owner", repo: "repo" }, runId: "42", runAttempt: 2, sha: "a".repeat(40), actorId: "11", eventName: "workflow_dispatch" },
            workflowRef: `owner/repo/.github/workflows/worker.lock.yml@${"a".repeat(40)}`,
            intentOrigin: { role: "dispatcher", principal: "11", repository: "owner/repo", workflow: ".github/workflows/worker.lock.yml", run_id: "42", run_attempt: 1 },
          })
      : endpoint === "run"
        ? () => fetchNativeRun(githubClient, "owner/repo", "42", sleepFn)
        : (retryMetadata = true) => fetchNativeRunAttempt(githubClient, "owner/repo", "42", 1, { sleepFn, retryMetadata });
  return { fetch, sleepFn, read };
}

function fakeDeadlines() {
  vi.useFakeTimers();
  return vi.spyOn(AbortSignal, "timeout").mockImplementation(delay => {
    expect(delay).toBe(15000);
    const controller = new AbortController();
    setTimeout(() => controller.abort(new DOMException("fixture timeout", "TimeoutError")), delay);
    return controller.signal;
  });
}

function stalledBody({ signal }, aborts) {
  return new Response(
    new ReadableStream({
      start(controller) {
        signal.addEventListener(
          "abort",
          () => {
            aborts.push(signal.reason);
            controller.error(signal.reason);
          },
          { once: true }
        );
      },
    }),
    { status: 200, headers: { "content-type": "application/json" } }
  );
}

describe("native Octokit queue identity metadata transport", () => {
  it.each(["repository", "run", "attempt"])("bounds stalled %s fetches with fresh 15-second deadlines", async endpoint => {
    const deadlines = fakeDeadlines();
    const aborts = [];
    const f = fixture(
      endpoint,
      ({ signal }) =>
        new Promise((resolve, reject) => {
          signal.addEventListener(
            "abort",
            () => {
              aborts.push(signal.reason);
              reject(signal.reason);
            },
            { once: true }
          );
        }),
      3
    );
    const rejected = expect(f.read()).rejects.toMatchObject({ status: 500, cause: { name: "TimeoutError" } });
    await vi.runAllTimersAsync();
    await rejected;
    expect(aborts).toHaveLength(3);
    expect(f.sleepFn.mock.calls).toEqual([[1000], [1000]]);
    expect(f.fetch).toHaveBeenCalledTimes(endpoint === "repository" ? 3 : 4);
    expect(deadlines).toHaveBeenCalledTimes(f.fetch.mock.calls.length);
    expect(new Set(f.fetch.mock.calls.map(([, options]) => options.signal)).size).toBe(f.fetch.mock.calls.length);
  });

  it.each(["repository", "run", "attempt"])("recovers a swallowed %s body timeout instead of treating it as invalid identity", async endpoint => {
    fakeDeadlines();
    const aborts = [];
    const f = fixture(endpoint, options => stalledBody(options, aborts));
    const resolved = expect(f.read()).resolves.toMatchObject({ id: 42, repository: { full_name: "owner/repo", id: 7 } });
    await vi.runAllTimersAsync();
    await resolved;
    expect(aborts).toHaveLength(1);
    expect(f.sleepFn).toHaveBeenCalledExactlyOnceWith(1000);
    expect(f.fetch).toHaveBeenCalledTimes(3);
  });

  it.each(["repository", "run", "attempt"])("propagates exhausted %s body deadlines without accepting empty metadata", async endpoint => {
    fakeDeadlines();
    const aborts = [];
    const f = fixture(endpoint, options => stalledBody(options, aborts), 3);
    const rejected = expect(f.read()).rejects.toMatchObject({ code: "ETIMEDOUT", cause: { name: "TimeoutError" } });
    await vi.runAllTimersAsync();
    await rejected;
    expect(aborts).toHaveLength(3);
    expect(f.sleepFn.mock.calls).toEqual([[1000], [1000]]);
    expect(f.fetch).toHaveBeenCalledTimes(endpoint === "repository" ? 3 : 4);
  });

  it.each(["repository", "attempt"])("retains a single bounded %s read when polling owns the retry budget", async endpoint => {
    fakeDeadlines();
    const aborts = [];
    const f = fixture(endpoint, options => stalledBody(options, aborts), 3);
    const rejected = expect(f.read(false)).rejects.toMatchObject({ code: "ETIMEDOUT" });
    await vi.runAllTimersAsync();
    await rejected;
    expect(aborts).toHaveLength(1);
    expect(f.sleepFn).not.toHaveBeenCalled();
    expect(f.fetch).toHaveBeenCalledTimes(endpoint === "repository" ? 1 : 2);
  });

  it.each([401, 403, 404, 422, 429])("does not retry HTTP %s or grant authority", async status => {
    const f = fixture("attempt", () => new Response(JSON.stringify({ message: "denied" }), { status, headers: { "content-type": "application/json" } }), 3);
    await expect(f.read()).rejects.toMatchObject({ status });
    expect(f.sleepFn).not.toHaveBeenCalled();
    expect(f.fetch).toHaveBeenCalledTimes(2);
  });

  it("authenticates the exact historical intent origin using a bounded single read", async () => {
    const f = fixture(
      "origin",
      () => {
        throw new Error("unexpected failed read");
      },
      0
    );
    await expect(f.read()).resolves.toMatchObject({ authenticated: true, run_id: "42", run_attempt: 1, publisher_attempt: 2 });
    expect(f.sleepFn).not.toHaveBeenCalled();
    expect(f.fetch).toHaveBeenCalledTimes(3);
  });

  it.each(["fetch", "body"])("bounds historical intent-origin %s stalls without adding retries", async stage => {
    fakeDeadlines();
    const aborts = [];
    const f = fixture("origin", options =>
      stage === "body"
        ? stalledBody(options, aborts)
        : new Promise((resolve, reject) => {
            options.signal.addEventListener(
              "abort",
              () => {
                aborts.push(options.signal.reason);
                reject(options.signal.reason);
              },
              { once: true }
            );
          })
    );
    const rejected = expect(f.read()).rejects.toMatchObject(stage === "body" ? { code: "ETIMEDOUT" } : { status: 500, cause: { name: "TimeoutError" } });
    await vi.runAllTimersAsync();
    await rejected;
    expect(aborts).toHaveLength(1);
    expect(f.sleepFn).not.toHaveBeenCalled();
    expect(f.fetch).toHaveBeenCalledTimes(3);
  });
});
