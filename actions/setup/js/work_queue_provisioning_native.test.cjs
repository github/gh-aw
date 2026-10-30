import { afterEach, describe, expect, it, vi } from "vitest";
import { getOctokit } from "@actions/github";

const { verifyWorkerRoute, verifiedDefaultReference, configuredWorkerDeployment } = require("./work_queue_provisioning.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { createWorkQueueLogger, queueControlErrorCode, queueControlErrorExplanation } = require("./work_queue_logging.cjs");

const profile = defaultPolicy({ repository: "owner/repo", principal: "1001", ref: "a".repeat(40) }).pools.default.profiles.default;
const workflow = `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${"b".repeat(64)}"\n`;
const source = "---\ntools:\n  work-queue:\n    worker: true\n---\n";
const deploymentEndpoints = ["repository", "reference", "source", "contents", "registration"];

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function fixture(failure, endpoint = "contents", failures = 1) {
  let attempts = 0;
  const fetch = vi.fn(async (url, options) => {
    const parsed = new URL(url);
    expect(parsed.origin).toBe("https://api.github.com");
    expect(options.method).toBe("GET");
    expect(options.signal).toBeInstanceOf(AbortSignal);
    expect(options.signal.aborted).toBe(false);
    let target;
    let data;
    if (parsed.pathname.includes("/contents/")) {
      target = parsed.pathname.endsWith(".md") ? "source" : "contents";
      const path = target === "source" ? profile.workflow.replace(/\.lock\.yml$/, ".md") : profile.workflow;
      expect(decodeURIComponent(parsed.pathname)).toBe(`/repos/owner/repo/contents/${path}`);
      expect(parsed.searchParams.get("ref")).toBe(profile.ref);
      data = { type: "file", path, encoding: "base64", content: Buffer.from(target === "source" ? source : workflow).toString("base64") };
    } else if (parsed.pathname === "/repos/owner/repo") {
      target = "repository";
      data = { full_name: "owner/repo", default_branch: "main" };
    } else if (decodeURIComponent(parsed.pathname) === "/repos/owner/repo/git/ref/heads/main") {
      target = "reference";
      data = { ref: "refs/heads/main", object: { sha: profile.ref } };
    } else {
      expect(parsed.pathname).toBe("/repos/owner/repo/actions/workflows/worker.lock.yml");
      target = "registration";
      data = { path: profile.workflow, state: "active" };
    }
    if (endpoint === target && ++attempts <= failures) return failure(options);
    return new Response(JSON.stringify(data), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  });
  const sleepFn = vi.fn(async () => {});
  return { fetch, sleepFn, options: { githubClient: getOctokit("fixture-token", { request: { fetch } }), owner: "owner", repo: "repo", profile, sleepFn } };
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

function stalledBody({ signal }, aborts, reads) {
  const response = new Response(
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
  reads.push(vi.spyOn(response, "text"));
  return response;
}

async function deployment(options) {
  const revision = await verifiedDefaultReference(options);
  return configuredWorkerDeployment(options, revision);
}

describe("native Octokit worker provisioning transport", () => {
  it.each(["contents", "registration"])("retries only the failed %s GET with a fresh fetch deadline", async endpoint => {
    const f = fixture(() => {
      throw new TypeError("private-network-details");
    }, endpoint);
    await verifyWorkerRoute(f.options);
    expect(f.fetch).toHaveBeenCalledTimes(3);
    expect(f.sleepFn).toHaveBeenCalledExactlyOnceWith(1000);
    const signals = f.fetch.mock.calls.map(([, options]) => options.signal);
    expect(new Set(signals).size).toBe(signals.length);
  });

  it("does not mistake Octokit's synthetic 500 for an HTTP server response", async () => {
    const f = fixture(
      () => {
        throw new TypeError("private-network-details");
      },
      "contents",
      3
    );
    let error;
    try {
      await verifyWorkerRoute(f.options);
    } catch (failure) {
      error = failure;
    }
    expect(error).toMatchObject({ status: 500, request: { method: "GET" } });
    expect(error.response).toBeUndefined();
    expect(queueControlErrorCode(error)).toBe("queue_control_github_transport_failed");
    expect(queueControlErrorExplanation(queueControlErrorCode(error))).not.toContain("GitHub returned a server error");
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    createWorkQueueLogger("provisioning").failure("worker.file.read.failed", error, true);
    const output = stderr.mock.calls.map(([text]) => text).join("");
    expect(output).toContain("transport_response_missing=true");
    expect(output).not.toContain("private-network-details");
    expect(f.fetch).toHaveBeenCalledTimes(3);
    expect(f.sleepFn).toHaveBeenCalledTimes(2);
  });

  it("bounds a stalled fetch at 15 seconds per attempt rather than passing an ignored timeout", async () => {
    const signalTimeout = fakeDeadlines();
    const aborts = [];
    const f = fixture(
      ({ signal }) =>
        new Promise((resolve, reject) =>
          signal.addEventListener(
            "abort",
            () => {
              aborts.push(signal.reason);
              reject(signal.reason);
            },
            { once: true }
          )
        ),
      "contents",
      3
    );
    const rejected = expect(verifyWorkerRoute(f.options)).rejects.toMatchObject({ status: 500, cause: { name: "TimeoutError" } });
    await vi.runAllTimersAsync();
    await rejected;
    expect(aborts).toHaveLength(3);
    expect(aborts.every(reason => reason.name === "TimeoutError")).toBe(true);
    expect(signalTimeout).toHaveBeenCalledTimes(3);
    expect(f.fetch).toHaveBeenCalledTimes(3);
    expect(f.sleepFn).toHaveBeenCalledTimes(2);
  });

  it.each(deploymentEndpoints)("recovers a swallowed %s response-body timeout before validating metadata", async endpoint => {
    const signalTimeout = fakeDeadlines();
    const aborts = [];
    const reads = [];
    const f = fixture(options => stalledBody(options, aborts, reads), endpoint);
    const resolved = expect(deployment(f.options)).resolves.toMatchObject({ available: true, profile: { ref: profile.ref } });
    await vi.runAllTimersAsync();
    await resolved;
    expect(aborts).toHaveLength(1);
    expect(reads).toHaveLength(1);
    expect(reads[0]).toHaveBeenCalledOnce();
    await expect(reads[0].mock.results[0].value).rejects.toMatchObject({ name: "TimeoutError" });
    expect(aborts[0].name).toBe("TimeoutError");
    expect(f.sleepFn).toHaveBeenCalledExactlyOnceWith(1000);
    expect(f.fetch).toHaveBeenCalledTimes(deploymentEndpoints.length + 1);
    expect(signalTimeout).toHaveBeenCalledTimes(f.fetch.mock.calls.length);
    const signals = f.fetch.mock.calls.map(([, options]) => options.signal);
    expect(new Set(signals).size).toBe(signals.length);
    expect(f.fetch.mock.calls.every(([, options]) => options.method === "GET")).toBe(true);
  });

  it.each(deploymentEndpoints)("propagates exhausted %s body deadlines instead of recording worker unavailability", async endpoint => {
    const signalTimeout = fakeDeadlines();
    const aborts = [];
    const reads = [];
    const f = fixture(options => stalledBody(options, aborts, reads), endpoint, 3);
    const rejected = expect(deployment(f.options)).rejects.toMatchObject({ code: "ETIMEDOUT", cause: { name: "TimeoutError" } });
    await vi.runAllTimersAsync();
    await rejected;
    expect(aborts).toHaveLength(3);
    expect(reads).toHaveLength(3);
    for (const read of reads) {
      expect(read).toHaveBeenCalledOnce();
      await expect(read.mock.results[0].value).rejects.toMatchObject({ name: "TimeoutError" });
    }
    expect(aborts.every(reason => reason.name === "TimeoutError")).toBe(true);
    expect(f.sleepFn.mock.calls).toEqual([[1000], [1000]]);
    expect(f.fetch).toHaveBeenCalledTimes(deploymentEndpoints.indexOf(endpoint) + 3);
    expect(signalTimeout).toHaveBeenCalledTimes(f.fetch.mock.calls.length);
    const signals = f.fetch.mock.calls.map(([, options]) => options.signal);
    expect(new Set(signals).size).toBe(signals.length);
    expect(f.fetch.mock.calls.every(([, options]) => options.method === "GET")).toBe(true);
  });

  it.each([401, 403, 404, 422, 429])("does not retry HTTP %s or proceed to registration", async status => {
    const f = fixture(() => new Response(JSON.stringify({ message: "denied" }), { status, headers: { "content-type": "application/json" } }), "contents", 3);
    await expect(verifyWorkerRoute(f.options)).rejects.toBeDefined();
    expect(f.fetch).toHaveBeenCalledTimes(1);
    expect(f.sleepFn).not.toHaveBeenCalled();
  });

  it("preserves actual server-response diagnostics after bounded retry exhaustion", async () => {
    const f = fixture(() => new Response(JSON.stringify({ message: "private-body" }), { status: 500, headers: { "content-type": "application/json", "x-github-request-id": "1:2:3:4:5" } }), "contents", 3);
    let error;
    try {
      await verifyWorkerRoute(f.options);
    } catch (failure) {
      error = failure;
    }
    expect(error.response.status).toBe(500);
    expect(queueControlErrorCode(error)).toBe("queue_control_github_api_unavailable");
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    createWorkQueueLogger("provisioning").failure("worker.file.read.failed", error, true);
    expect(stderr.mock.calls.map(([text]) => text).join("")).not.toContain("transport_response_missing");
  });
});
