import { describe, it, expect, vi, afterEach } from "vitest";
const { getStalePullRequestReason } = require("./pr_lifecycle.cjs");

describe("PR lifecycle guard", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
  });

  const runtime = state => ({
    eventName: "pull_request",
    repo: { owner: "owner", repo: "repo" },
    payload: { pull_request: { number: 42, state } },
  });
  const client = state => ({ rest: { pulls: { get: vi.fn().mockResolvedValue({ data: { state } }) } } });

  it("skips work triggered open when the live PR is closed", async () => {
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    const github = client("closed");
    expect(await getStalePullRequestReason(github, runtime("open"))).toContain("#42 closed after");
    expect(github.rest.pulls.get).toHaveBeenCalledWith({ owner: "owner", repo: "repo", pull_number: 42 });
    expect(log).not.toHaveBeenCalled();
  });

  it("rechecks the state rather than caching an earlier open response", async () => {
    const github = client("open");
    expect(await getStalePullRequestReason(github, runtime("open"))).toBeNull();
    github.rest.pulls.get.mockResolvedValueOnce({ data: { state: "closed" } });
    expect(await getStalePullRequestReason(github, runtime("open"))).not.toBeNull();
  });

  it("preserves workflows intentionally triggered on a closed or merged PR", async () => {
    const github = client("closed");
    expect(await getStalePullRequestReason(github, runtime("closed"))).toBeNull();
    expect(github.rest.pulls.get).not.toHaveBeenCalled();
  });

  it("supports comments made while the PR was open", async () => {
    const context = runtime("open");
    context.payload = { issue: { number: 42, state: "open", pull_request: {} } };
    expect(await getStalePullRequestReason(client("closed"), context)).not.toBeNull();
  });

  it("does not guess a lifecycle for unrelated or unknown trigger context", async () => {
    const github = client("closed");
    for (const context of [undefined, {}, { payload: {} }, runtime(undefined)]) {
      expect(await getStalePullRequestReason(github, context)).toBeNull();
    }
    expect(github.rest.pulls.get).not.toHaveBeenCalled();
  });

  it.each([401, 403, 404, 429, 500])("keeps HTTP %d visible instead of treating it as closure", async status => {
    const github = client("closed");
    const error = Object.assign(new Error("state unavailable"), { status });
    github.rest.pulls.get.mockRejectedValueOnce(error);
    await expect(getStalePullRequestReason(github, runtime("open"))).rejects.toBe(error);
  });

  it("rejects an unknown live state", async () => {
    await expect(getStalePullRequestReason(client(undefined), runtime("open"))).rejects.toThrow("Cannot determine current state");
  });

  it("keeps staged previews available without API reads and logs a preview notice", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    const github = client("closed");
    expect(await getStalePullRequestReason(github, runtime("open"))).toBeNull();
    expect(github.rest.pulls.get).not.toHaveBeenCalled();
    expect(log).toHaveBeenCalledExactlyOnceWith("🎭 Staged Mode Preview — skipping live pull request lifecycle check");
  });

  it("does not log staged previews when no open PR applies", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    const github = client("closed");
    for (const context of [undefined, {}, { payload: {} }, runtime("closed"), runtime(undefined)]) {
      expect(await getStalePullRequestReason(github, context)).toBeNull();
    }
    expect(github.rest.pulls.get).not.toHaveBeenCalled();
    expect(log).not.toHaveBeenCalled();
  });
});
