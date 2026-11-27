// @ts-check
import { describe, expect, it, vi } from "vitest";
import { getOctokit } from "@actions/github";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { createRateLimitAwareGithub } = require("./github_rate_limit_logger.cjs");

const assignment = { dispatch_id: "d", claims: [{ handle: "h1", claim_id: "c1", work_id: "w1", work: {}, result_refs: [] }] };

describe("native Octokit Claim reads", () => {
  it.each([false, true])("reads through the instrumented client without authorization (signal=%s)", async bounded => {
    const fetch = vi.fn(async (url, options) => {
      expect(String(url)).toBe("https://api.github.com/repos/owner/repo");
      expect(options.method).toBe("GET");
      return new Response(JSON.stringify({ id: 7, full_name: "owner/repo" }), { status: 200, headers: { "content-type": "application/json" } });
    });
    const authorize = vi.fn();
    const source = createRateLimitAwareGithub(getOctokit("fixture-token", { request: { fetch } }));
    await withClaimExecution({ assignment, claim_handle: "h1", authorize, effects: [] }, async () => {
      const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize, ...(bounded ? { signal: new AbortController().signal } : {}) });
      const response = await client.rest.repos.get({ owner: "owner", repo: "repo", headers: { "X-GitHub-Api-Version": "2026-03-10" }, request: { retries: 0, timeout: 15000 } });
      expect(response.data.id).toBe(7);
    });
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(authorize).not.toHaveBeenCalled();
  });
});
