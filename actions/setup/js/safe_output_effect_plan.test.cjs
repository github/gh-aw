import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { request as octokitRequest } from "@octokit/request";

const require = createRequire(import.meta.url);
const protocol = require("./safe_output_authorization.cjs");
const gate = require("./safe_output_authorization_gate.cjs");
const plans = require("./safe_output_effect_plan.cjs");
const { withSafeOutputInvocation, authorizeSafeOutputEffectPlan } = require("./safe_output_execution.cjs");
const { loadHandlers, processMessages } = require("./safe_output_handler_manager.cjs");
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { GITHUB_API_VERSION } = require("./constants.cjs");

const root = path.resolve(`.effect-plan-${process.pid}`);
const requirement = { endpoint: "https://authority.example/authorize", audience: "safe-outputs", source: "local" };
const caller = {
  host: "https://github.com",
  repository_id: "123",
  workflow_ref: "github/gh-aw/.github/workflows/test.yml@refs/heads/main",
  workflow_sha: "a".repeat(40),
  workflow_hash: "b".repeat(64),
  run_id: "456",
  run_attempt: "1",
};

function json(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

/**
 * Fake authority: atomically claims each logical invocation once and can
 * require resolved-effect binding. Also serves the OIDC credential endpoint.
 */
function authority({ requireResolved = false, deny = false, beforeDecision = undefined } = {}) {
  const claimed = new Set();
  const requests = [];
  const fetch = vi.fn(async (url, init) => {
    const href = String(url);
    if (href.startsWith("https://token.actions.example/")) {
      const payload = Buffer.from(JSON.stringify({ aud: new URL(href).searchParams.get("audience"), exp: Math.floor(Date.now() / 1000) + 600 })).toString("base64url");
      return json({ value: `e30.${payload}.sig` });
    }
    const body = JSON.parse(init.body);
    const invocation = body.invocation;
    requests.push({ raw: init.body, invocation });
    if (beforeDecision) await beforeDecision(invocation);
    const allow = !deny && !claimed.has(invocation.invocation_id) && (!requireResolved || invocation.scope.binding === "resolved_effect");
    claimed.add(invocation.invocation_id);
    const now = Date.now();
    return json({
      version: invocation.version,
      decision: allow ? "allow" : "deny",
      request_digest: body.request_digest,
      invocation_id: invocation.invocation_id,
      run_attempt: invocation.caller.run_attempt,
      policy_revision: "rev-1",
      decision_id: `decision-${requests.length}`,
      issued_at: new Date(now).toISOString(),
      expires_at: new Date(now + 25000).toISOString(),
    });
  });
  return { fetch, requests };
}

/** Real Octokit request pipeline whose network layer records every wire request. */
function transport(respond) {
  const sent = [];
  const fetch = vi.fn(async (url, init) => {
    sent.push({ url: String(url), method: init.method, headers: { ...init.headers }, body: init.body, redirect: init.redirect });
    return respond(String(url), init, sent.length);
  });
  return { sent, fetch, request: octokitRequest.defaults({ headers: { authorization: "token ghs_test" }, request: { fetch } }) };
}

function invocationState(type, extra = {}) {
  const state = {
    requirement,
    batchDigest: "c".repeat(64),
    recordIndex: 0,
    invocation: { caller, output: { type, name: type, executor: "manager", implementation_digest: "d".repeat(64) }, context: { policy_digest: "e".repeat(64) } },
    authorize: options => gate.authorizeHandlerInvocation(options),
    authorizeEffectPlan: handle => gate.authorizeEffectPlan(handle),
    ...extra,
  };
  state.effectPreparation = plans.createEffectPreparation({ type, route: extra.route || "manager", state });
  return state;
}

const removeLabelSpec = (names = ["bug"]) => ({
  targets: [
    { kind: "repository", repository: "github/gh-aw", repository_id: "99" },
    { kind: "issue", repository: "github/gh-aw", repository_id: "99", number: 7, resource_id: "700" },
  ],
  effects: names.map(name => ({ operation: "issues.removeLabel", path: `/repos/github/gh-aw/issues/7/labels/${encodeURIComponent(name)}` })),
  preconditions: [{ id: "label_identities", enforcement: "observed", observation: names.map((_, index) => index + 1) }],
});

beforeEach(() => {
  vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn(), setSecret: vi.fn() });
  vi.stubGlobal("context", { eventName: "pull_request", repo: { owner: "github", repo: "gh-aw" }, payload: { pull_request: { number: 100 } } });
  for (const [key, value] of Object.entries({
    RUNNER_TEMP: root,
    GITHUB_REPOSITORY_ID: "123",
    GITHUB_WORKFLOW_REF: caller.workflow_ref,
    GITHUB_WORKFLOW_SHA: caller.workflow_sha,
    GH_AW_AUTHORIZATION_WORKFLOW_HASH: caller.workflow_hash,
    GITHUB_RUN_ID: "456",
    GITHUB_RUN_ATTEMPT: "1",
    GH_AW_SAFE_OUTPUTS_STAGED: "false",
    ACTIONS_ID_TOKEN_REQUEST_TOKEN: "bootstrap",
    ACTIONS_ID_TOKEN_REQUEST_URL: "https://token.actions.example/token",
  }))
    vi.stubEnv(key, value);
  fs.mkdirSync(path.join(root, "gh-aw"), { recursive: true });
});

afterEach(() => {
  fs.rmSync(root, { recursive: true, force: true });
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("effect plan contract", () => {
  it("publishes the exact initial adapter coverage matrix", () => {
    expect(plans.effectAdapterCoverage()).toEqual({
      merge_pull_request: { adapter: "gh-aw.merge-pull-request@1", routes: ["manager", "claim"] },
      remove_labels: { adapter: "gh-aw.remove-labels@1", routes: ["manager", "claim"] },
    });
    expect(plans.selectEffectAdapter("create_issue", "manager")).toBeNull();
    expect(plans.selectEffectAdapter("merge_pull_request", "standalone")).toBeNull();
  });

  it("freezes a canonical golden vector that fits the existing envelope", async () => {
    const state = invocationState("remove_labels");
    await withSafeOutputInvocation(state, async () => {
      const handle = plans.freezeEffectPlan(state.effectPreparation, removeLabelSpec(["bug", "needs triage"]));
      const described = plans.describeEffectPlan(handle);
      expect(GITHUB_API_VERSION).toBe("2026-03-10");
      expect(handle.plan_digest).toBe("8a9d6e41223b814e7f91199e530c68e669bafeeeda7b3b26924f8a789587a5a2");
      expect(described.refs).toEqual([
        "effect-contract:gh-aw.safe-output-effect-plan.v1",
        "effect-adapter:gh-aw.remove-labels@1",
        "effect-route:manager",
        `effect-plan:sha256:${handle.plan_digest}`,
        "effect-count:2",
        "effect-operation:issues.removeLabel",
        "effect-target:repository:99",
        "effect-target:issue:99#7@700",
        "effect-precondition:label_identities:observed",
      ]);
      expect(Object.isFrozen(handle)).toBe(true);
      expect(JSON.stringify(described)).not.toContain("needs");
    });
  });

  it("rejects raw resolved_effect promotion and forged handles before any authority request", async () => {
    const authorityFetch = authority();
    vi.stubGlobal("fetch", authorityFetch.fetch);
    const forged = Object.freeze({ contract: plans.CONTRACT, adapter: "gh-aw.remove-labels@1", route: "manager", plan_digest: "f".repeat(64), effect_count: 1 });
    expect(() =>
      protocol.prepareInvocation({ caller, output: invocationState("remove_labels").invocation.output, normalizedInputs: {}, batchDigest: "c".repeat(64), recordIndex: 0, scope: { binding: "resolved_effect", refs: [] } })
    ).toThrow("invalid_preparation");
    const unsupported = { ...invocationState("remove_labels"), effectPreparation: undefined };
    await withSafeOutputInvocation(unsupported, async () => {
      await expect(gate.requireAuthorization({ inputs: {}, scope: { binding: "resolved_effect", refs: [] } })).rejects.toThrow("invalid_preparation");
      await expect(authorizeSafeOutputEffectPlan(forged)).rejects.toThrow("trusted prepared invocation context");
    });
    const state = invocationState("remove_labels");
    await withSafeOutputInvocation(state, async () => {
      await expect(authorizeSafeOutputEffectPlan(forged)).rejects.toThrow("not registered");
      // An adapter-selected invocation cannot downgrade to invocation binding.
      await expect(gate.requireAuthorization({ inputs: {} })).rejects.toThrow("invalid_preparation");
      await expect(plans.sendPlannedEffect(forged, 0, { client: {} })).rejects.toThrow("not registered");
      expect(() => plans.freezeEffectPlan({ adapter: "gh-aw.remove-labels@1", route: "manager", type: "remove_labels" }, removeLabelSpec())).toThrow("trusted preparation capability");
    });
    expect(() => plans.freezeEffectPlan(state.effectPreparation, removeLabelSpec())).toThrow("trusted preparation capability");
    expect(authorityFetch.fetch).not.toHaveBeenCalled();
  });

  it("sends only the approved requests once and refuses middleware changes, replays and expiry", async () => {
    const authorityFetch = authority();
    vi.stubGlobal("fetch", authorityFetch.fetch);
    const github = transport(() => new Response(null, { status: 204 }));
    const state = invocationState("remove_labels");
    await withSafeOutputInvocation(state, async () => {
      const handle = plans.freezeEffectPlan(state.effectPreparation, removeLabelSpec(["bug", "wontfix", "later"]));
      await expect(plans.sendPlannedEffect(handle, 0, { client: github })).rejects.toMatchObject({ transmitted: false, reason_code: "plan_not_authorized" });
      const authorization = await authorizeSafeOutputEffectPlan(handle);
      expect(authorization).toMatchObject({ state: "allowed", binding: "resolved_effect" });
      await expect(authorizeSafeOutputEffectPlan(handle)).rejects.toThrow("already requested");

      await plans.sendPlannedEffect(handle, 0, { client: github });
      await expect(plans.sendPlannedEffect(handle, 0, { client: github })).rejects.toMatchObject({ transmitted: false, reason_code: "effect_out_of_order" });

      // SDK middleware that changes a default after approval is blocked before transport.
      const tampered = { request: options => github.request({ ...options, headers: { ...options.headers, "x-http-method-override": "PATCH" } }) };
      tampered.request.endpoint = github.request.endpoint;
      await expect(plans.sendPlannedEffect(handle, 1, { client: tampered })).rejects.toMatchObject({ transmitted: false, reason_code: "request_changed" });

      vi.spyOn(protocol, "assertAuthorizationFresh").mockImplementation(() => {
        throw new protocol.SafeOutputAuthorizationError("invalid_validity");
      });
      await expect(plans.sendPlannedEffect(handle, 2, { client: github })).rejects.toMatchObject({ transmitted: false, reason_code: "authorization_expired" });
      expect(plans.effectPlanOutcome(handle)).toBe("partial");
    });
    expect(github.sent).toEqual([
      {
        url: "https://api.github.com/repos/github/gh-aw/issues/7/labels/bug",
        method: "DELETE",
        headers: expect.objectContaining({ accept: "application/vnd.github+json", "x-github-api-version": GITHUB_API_VERSION }),
        body: undefined,
        redirect: "manual",
      },
    ]);
    expect(authorityFetch.requests).toHaveLength(1);
  });

  it("refuses SDK retries of a transmitted request and reports an indeterminate effect", async () => {
    vi.stubGlobal("fetch", authority().fetch);
    const github = transport(() => {
      throw new TypeError("socket hang up");
    });
    const retrying = {
      request: async options => {
        try {
          return await github.request(options);
        } catch {
          return github.request(options);
        }
      },
    };
    retrying.request.endpoint = github.request.endpoint;
    const state = invocationState("remove_labels");
    await withSafeOutputInvocation(state, async () => {
      const handle = plans.freezeEffectPlan(state.effectPreparation, removeLabelSpec());
      await authorizeSafeOutputEffectPlan(handle);
      await expect(plans.sendPlannedEffect(handle, 0, { client: retrying })).rejects.toMatchObject({ transmitted: true, reason_code: "indeterminate" });
      expect(plans.effectPlanOutcome(handle)).toBe("indeterminate");
    });
    expect(github.sent).toHaveLength(1);
  });

  it("keeps Claim checks on the Claim route and forwards the same frozen request", async () => {
    vi.stubGlobal("fetch", authority().fetch);
    const github = transport(() => new Response(null, { status: 204 }));
    const assignment = {
      version: 3,
      dispatch_id: "d",
      request_id: "r",
      commit_id: "commit",
      policy_epoch: "epoch",
      pool: "default",
      worker_profile: "default",
      claims: [{ handle: "h1", claim_id: "c:h1", work_id: "w:h1", work: {}, result_refs: [] }],
    };
    const claimChecks = [];
    const authorizeClaim = async request => {
      claimChecks.push(request);
      return { claim_handle: request.claim_handle, authorized: true };
    };
    const raw = {
      request: github.request,
      rest: {
        repos: { get: async () => ({ data: { id: 99, full_name: "github/gh-aw" } }) },
        issues: { get: async () => ({ data: { id: 700, number: 7, labels: [] } }) },
      },
    };
    await withClaimExecution({ assignment, claim_handle: "h1", authorize: authorizeClaim }, async () => {
      const client = wrapClaimEffectClient(raw, { claim_handle: "h1", authorize: authorizeClaim });
      const state = invocationState("remove_labels", { route: "claim" });
      state.invocation = { ...state.invocation, context: { ...state.invocation.context, claim_id: "c:h1", delivery_id: "d" } };
      await withSafeOutputInvocation(state, async () => {
        const handle = plans.freezeEffectPlan(state.effectPreparation, removeLabelSpec());
        expect(handle.route).toBe("claim");
        const authorization = await authorizeSafeOutputEffectPlan(handle);
        expect(authorization.binding).toBe("resolved_effect");
        await plans.sendPlannedEffect(handle, 0, { client });
        // A function transport not registered by a trusted plan cannot cross the Claim boundary.
        await expect((async () => client.request({ method: "DELETE", url: "https://api.github.com/repos/github/gh-aw/issues/7/labels/x", request: { fetch: github.fetch } }))()).rejects.toThrow("native request transport");
      });
    });
    expect(claimChecks.length).toBeGreaterThan(0);
    expect(github.sent.map(entry => `${entry.method} ${entry.url}`)).toEqual(["DELETE https://api.github.com/repos/github/gh-aw/issues/7/labels/bug"]);
  });
});

describe("manager resolved-effect execution", () => {
  function mergeFixture() {
    const pr = {
      id: 555,
      node_id: "PR_kwDO",
      number: 100,
      html_url: "https://github.com/github/gh-aw/pull/100",
      state: "open",
      merged: false,
      changed_files: 1,
      draft: false,
      mergeable: true,
      mergeable_state: "clean",
      head: { ref: "feature/test", sha: "abc123" },
      base: { ref: "integration", sha: "base123", repo: { id: 99, full_name: "github/gh-aw" } },
      user: { login: "author" },
      labels: [{ name: "ready" }],
      title: "Private title",
      body: "Private body",
      requested_reviewers: [],
      requested_teams: [],
    };
    const reviewPR = { headRefOid: "abc123", reviewDecision: null, reviewThreads: { pageInfo: { hasNextPage: false, endCursor: null }, nodes: [] } };
    const github = transport(() => json({ merged: true, sha: "merged123", message: "Merged" }));
    const client = {
      request: github.request,
      rest: {
        pulls: {
          get: vi.fn(async () => ({ data: structuredClone(pr) })),
          list: vi.fn().mockResolvedValue({ data: [{ number: 42 }] }),
          listReviews: vi.fn(),
          listFiles: vi.fn(),
          merge: vi.fn(),
        },
        repos: {
          getBranch: vi.fn().mockResolvedValue({ data: { protected: false } }),
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
          getBranchProtection: vi.fn().mockRejectedValue({ status: 404 }),
        },
        checks: { listForRef: vi.fn() },
      },
      graphql: vi.fn(async query => (query.includes("viewer") ? { viewer: { login: "merge-bot" } } : { repository: { pullRequest: structuredClone(reviewPR) } })),
      paginate: vi.fn(async method => (method === client.rest.pulls.listFiles ? [{ filename: "src/index.js", status: "modified" }] : [])),
    };
    vi.stubGlobal("github", client);
    return { pr, reviewPR, github, client };
  }

  const mergeConfig = (extra = {}) => ({ merge_pull_request: { authorization: requirement, allowed_base_branches: ["integration"], target: "*", ...extra } });

  it("authorizes the frozen merge once with resolved_effect and sends exactly that request", async () => {
    const authorityFetch = authority({ requireResolved: true });
    vi.stubGlobal("fetch", authorityFetch.fetch);
    const { github, client } = mergeFixture();
    const handlers = await loadHandlers(mergeConfig());
    const { results } = await processMessages(handlers, [{ type: "merge_pull_request", pull_request_number: 100, commit_title: null }]);
    expect(results[0]).toMatchObject({ success: true, authorization: { state: "allowed", binding: "resolved_effect" } });
    expect(authorityFetch.requests).toHaveLength(1);
    const { invocation, raw } = authorityFetch.requests[0];
    expect(invocation.scope).toMatchObject({ binding: "resolved_effect", refs: expect.arrayContaining(["effect-adapter:gh-aw.merge-pull-request@1", "effect-target:pull_request:99#100@555", "effect-precondition:head_sha:atomic"]) });
    expect(invocation.input_digest).toBe(results[0].result.decision_evidence.effect_plan.plan_digest);
    expect(raw).not.toMatch(/Private|abc123|merge-bot/);
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
    expect(github.sent).toHaveLength(1);
    expect(github.sent[0]).toMatchObject({ method: "PUT", url: "https://api.github.com/repos/github/gh-aw/pulls/100/merge" });
    expect(JSON.parse(github.sent[0].body)).toEqual({ sha: "abc123", merge_method: "merge", commit_title: null });
  });

  it("never downgrades a denied resolved decision or a failed preparation to invocation binding", async () => {
    const denied = authority({ deny: true });
    vi.stubGlobal("fetch", denied.fetch);
    const { github, pr } = mergeFixture();
    let { results } = await processMessages(await loadHandlers(mergeConfig()), [{ type: "merge_pull_request", pull_request_number: 100 }]);
    expect(results[0]).toMatchObject({ success: false, authorization: { state: "denied", binding: "resolved_effect" } });
    expect(denied.requests.map(request => request.invocation.scope.binding)).toEqual(["resolved_effect"]);

    delete pr.base.repo;
    const fresh = authority();
    vi.stubGlobal("fetch", fresh.fetch);
    ({ results } = await processMessages(await loadHandlers(mergeConfig()), [{ type: "merge_pull_request", pull_request_number: 100 }]));
    expect(results[0]).toMatchObject({ success: false, authorization: { state: "not_requested", reason_code: "deferred_effect_plan" } });
    expect(fresh.requests).toHaveLength(0);
    expect(github.sent).toHaveLength(0);
  });

  it.each([
    ["head", fixture => (fixture.pr.head.sha = "def456")],
    ["review", fixture => (fixture.reviewPR.reviewDecision = "CHANGES_REQUESTED")],
  ])("stops when the %s changes while waiting for the authority", async (_name, mutate) => {
    const fixture = mergeFixture();
    vi.stubGlobal("fetch", authority({ beforeDecision: () => mutate(fixture) }).fetch);
    const { results } = await processMessages(await loadHandlers(mergeConfig()), [{ type: "merge_pull_request", pull_request_number: 100 }]);
    expect(results[0]).toMatchObject({ success: false, authorization: { state: "allowed" } });
    expect(fixture.github.sent).toHaveLength(0);
  });

  it("reconciles a lost merge response read-only without replaying it", async () => {
    vi.stubGlobal("fetch", authority().fetch);
    const fixture = mergeFixture();
    fixture.github.fetch.mockImplementation(async (url, init) => {
      fixture.github.sent.push({ url: String(url), method: init.method });
      throw new TypeError("connection reset");
    });
    const { results } = await processMessages(await loadHandlers(mergeConfig()), [{ type: "merge_pull_request", pull_request_number: 100 }]);
    expect(results[0]).toMatchObject({ success: false, result: { operation_outcome: "indeterminate", merged: null } });
    expect(fixture.github.sent).toHaveLength(1);
  });

  it("keeps a merge confirmed when a 2xx body is malformed but read-back confirms it", async () => {
    vi.stubGlobal("fetch", authority().fetch);
    const fixture = mergeFixture();
    fixture.github.fetch.mockImplementation(async (url, init) => {
      fixture.github.sent.push({ url: String(url), method: init.method });
      Object.assign(fixture.pr, { merged: true, merge_commit_sha: "f".repeat(40) });
      return json({ sha: "merged123" });
    });
    const { results } = await processMessages(await loadHandlers(mergeConfig()), [{ type: "merge_pull_request", pull_request_number: 100 }]);
    expect(results[0]).toMatchObject({ success: true });
    expect(fixture.github.sent).toHaveLength(1);
  });

  it("previews staged protected merges without authority, OIDC or mutation", async () => {
    const authorityFetch = authority();
    vi.stubGlobal("fetch", authorityFetch.fetch);
    const { github } = mergeFixture();
    const { results } = await processMessages(await loadHandlers(mergeConfig({ staged: true })), [{ type: "merge_pull_request", pull_request_number: 100 }]);
    expect(results[0]).toMatchObject({ success: true, authorization: { state: "skipped_staged" } });
    expect(authorityFetch.fetch).not.toHaveBeenCalled();
    expect(github.sent).toHaveLength(0);
  });

  function labelFixture(
    labels = [
      { id: 1, name: "bug" },
      { id: 2, name: "wontfix" },
      { id: 3, name: "ready" },
    ],
    respond = () => new Response(null, { status: 204 })
  ) {
    const item = { id: 700, number: 7, title: "Private title", labels: structuredClone(labels) };
    const github = transport((url, init, count) => {
      const name = decodeURIComponent(url.split("/labels/")[1]);
      const response = respond(url, init, count);
      if (response.status < 300) item.labels = item.labels.filter(label => label.name.toLowerCase() !== name.toLowerCase());
      return response;
    });
    const client = {
      request: github.request,
      rest: {
        repos: { get: vi.fn(async () => ({ data: { id: 99, full_name: "github/gh-aw" } })) },
        issues: { get: vi.fn(async () => ({ data: structuredClone(item) })), removeLabel: vi.fn() },
      },
    };
    vi.stubGlobal("github", client);
    return { item, github, client };
  }

  const labelConfig = { remove_labels: { authorization: requirement, target: "*", max: 5 } };

  it("authorizes and sends only the finalized, filtered and deduplicated label removals", async () => {
    const authorityFetch = authority({ requireResolved: true });
    vi.stubGlobal("fetch", authorityFetch.fetch);
    const { github, client } = labelFixture();
    const { results } = await processMessages(await loadHandlers(labelConfig), [{ type: "remove_labels", item_number: 7, labels: ["bug", "BUG", "absent", "wontfix"] }]);
    expect(results[0]).toMatchObject({ success: true, result: { labelsRemoved: ["bug", "wontfix"] }, authorization: { state: "allowed", binding: "resolved_effect" } });
    expect(authorityFetch.requests).toHaveLength(1);
    expect(authorityFetch.requests[0].invocation.scope.refs).toEqual(expect.arrayContaining(["effect-count:2", "effect-target:label:99@1", "effect-target:label:99@2"]));
    expect(authorityFetch.requests[0].raw).not.toMatch(/wontfix|absent|Private/);
    expect(client.rest.issues.removeLabel).not.toHaveBeenCalled();
    expect(github.sent.map(entry => `${entry.method} ${entry.url}`)).toEqual(["DELETE https://api.github.com/repos/github/gh-aw/issues/7/labels/bug", "DELETE https://api.github.com/repos/github/gh-aw/issues/7/labels/wontfix"]);
  });

  function outcomes() {
    return vi.spyOn(protocol, "recordAuthorizationOutcome");
  }

  it("stops remaining removals when the grant expires and preserves the partial result", async () => {
    vi.stubGlobal("fetch", authority().fetch);
    const { github, item } = labelFixture();
    const recorded = outcomes();
    const fresh = protocol.assertAuthorizationFresh;
    let checks = 0;
    vi.spyOn(protocol, "assertAuthorizationFresh").mockImplementation(options => {
      // Manager and gate checks, then two checks (pre-send and wire) for the first removal.
      if (++checks > 3) throw new protocol.SafeOutputAuthorizationError("invalid_validity");
      return fresh(options);
    });
    const { results } = await processMessages(await loadHandlers(labelConfig), [{ type: "remove_labels", item_number: 7, labels: ["bug", "wontfix"] }]);
    expect(results[0]).toMatchObject({ success: false, result: { operation_outcome: "partial" } });
    expect(recorded).toHaveBeenCalledWith(expect.objectContaining({ operationStatus: "partial" }));
    expect(github.sent.map(entry => entry.url)).toEqual(["https://api.github.com/repos/github/gh-aw/issues/7/labels/bug"]);
    expect(item.labels.map(label => label.name)).toEqual(["wontfix", "ready"]);
  });

  it("retains uncertainty for a lost removal response instead of replaying or continuing", async () => {
    vi.stubGlobal("fetch", authority().fetch);
    const recorded = outcomes();
    const { github } = labelFixture(undefined, () => {
      throw new TypeError("connection reset");
    });
    const { results } = await processMessages(await loadHandlers(labelConfig), [{ type: "remove_labels", item_number: 7, labels: ["bug", "wontfix"] }]);
    expect(results[0]).toMatchObject({ success: false, result: { operation_outcome: "indeterminate" } });
    expect(recorded).toHaveBeenCalledWith(expect.objectContaining({ operationStatus: "indeterminate" }));
    expect(github.sent).toHaveLength(1);
  });

  it("does not treat a 404 as absence unless reconciliation proves the label is gone", async () => {
    vi.stubGlobal("fetch", authority().fetch);
    labelFixture(undefined, () => json({ message: "Not Found" }, 404));
    let { results } = await processMessages(await loadHandlers(labelConfig), [{ type: "remove_labels", item_number: 7, labels: ["bug"] }]);
    expect(results[0]).toMatchObject({ success: false, result: { operation_outcome: "indeterminate" } });

    vi.stubGlobal("fetch", authority().fetch);
    const fixture = labelFixture(undefined, () => {
      fixture.item.labels = fixture.item.labels.filter(label => label.name !== "bug");
      return json({ message: "Not Found" }, 404);
    });
    ({ results } = await processMessages(await loadHandlers(labelConfig), [{ type: "remove_labels", item_number: 7, labels: ["bug"] }]));
    expect(results[0]).toMatchObject({ success: true, result: { labelsRemoved: [] } });
  });

  it("stops when a selector now points to a different resource after authorization", async () => {
    const fixture = labelFixture();
    vi.stubGlobal("fetch", authority({ beforeDecision: () => (fixture.item.labels[0].id = 77) }).fetch);
    const recorded = outcomes();
    const { results } = await processMessages(await loadHandlers(labelConfig), [{ type: "remove_labels", item_number: 7, labels: ["bug"] }]);
    expect(results[0]).toMatchObject({ success: false, authorization: { state: "allowed" } });
    expect(recorded).toHaveBeenCalledWith(expect.objectContaining({ operationStatus: "failed" }));
    expect(fixture.github.sent).toHaveLength(0);
  });

  it("keeps identical records distinct and refuses replay of an already-claimed record", async () => {
    const authorityFetch = authority();
    vi.stubGlobal("fetch", authorityFetch.fetch);
    const { github } = labelFixture([
      { id: 1, name: "bug" },
      { id: 2, name: "wontfix" },
    ]);
    const batch = [
      { type: "remove_labels", item_number: 7, labels: ["bug"] },
      { type: "remove_labels", item_number: 7, labels: ["wontfix"] },
    ];
    const handlers = await loadHandlers(labelConfig);
    await processMessages(handlers, batch);
    const ids = authorityFetch.requests.map(request => request.invocation.invocation_id);
    expect(new Set(ids).size).toBe(2);
    github.sent.length = 0;
    const replay = await processMessages(await loadHandlers(labelConfig), batch);
    expect(replay.results.every(record => record.authorization?.state !== "allowed")).toBe(true);
    expect(github.sent).toHaveLength(0);
  });

  it("keeps unsupported custom scripts invocation-bound so a resolved-only authority denies them", async () => {
    const authorityFetch = authority({ requireResolved: true });
    vi.stubGlobal("fetch", authorityFetch.fetch);
    vi.stubEnv("GH_AW_SAFE_OUTPUT_SCRIPTS", JSON.stringify({ custom: "custom.cjs" }));
    const scriptPath = path.join(root, "gh-aw/actions/custom.cjs");
    fs.mkdirSync(path.dirname(scriptPath), { recursive: true });
    fs.writeFileSync(scriptPath, "exports.main = async () => async () => ({ success: true });");
    const { results } = await processMessages(await loadHandlers({ custom: { authorization: requirement } }), [{ type: "custom", scope: "resolved_effect" }]);
    expect(authorityFetch.requests.map(request => request.invocation.scope.binding)).toEqual(["invocation"]);
    expect(results[0]).toMatchObject({ success: false, authorization: { state: "denied", binding: "invocation" } });
  });
});
