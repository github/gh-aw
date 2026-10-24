// @ts-check
import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";

const require = createRequire(import.meta.url);
const { queueFixture, REPOSITORY, WORKFLOW, REF } = require("./work_queue_lifecycle.test_helpers.cjs");
const { withClaimExecution, closeClaimEffectChannel, recordClaimEffect } = require("./work_queue_claim_scope.cjs");
const { authorizeWorkerClaim, finalizeWorkerResults } = require("./finish_work_queue_claim.cjs");
const { readDeliveryControlInventory, createClaimDeliveryVerifier, verifyBuiltinDeliveryOutput } = require("./work_queue_delivery.cjs");
const { captureDiscussionDelivery, preparePullRequestDelivery, capturePullRequestDelivery } = require("./work_queue_native_creation_delivery.cjs");
const { settleClaimDelivery } = require("./safe_output_handler_manager.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { addDiscussionComment, closeDiscussionAsOutdated } = require("./close_older_discussions.cjs");
const { MAX_CLOSE_COUNT } = require("./close_older_entities.cjs");

const TREE = "b".repeat(40);
const COMMIT = "c".repeat(40);
const BASE = "d".repeat(40);
const EFFECTIVE_BASE = "e".repeat(40);
let oldEnv;
let oldCore;

beforeEach(() => {
  oldEnv = { ...process.env };
  oldCore = global.core;
  global.core = Object.fromEntries(["info", "debug", "warning", "error", "setOutput", "setFailed"].map(key => [key, vi.fn()]));
  process.env.GITHUB_REPOSITORY = REPOSITORY;
  process.env.GITHUB_RUN_ID = "42";
  process.env.GITHUB_RUN_ATTEMPT = "1";
});

afterEach(() => {
  for (const key of Object.keys(process.env)) if (!Object.hasOwn(oldEnv, key)) delete process.env[key];
  Object.assign(process.env, oldEnv);
  global.core = oldCore;
  vi.restoreAllMocks();
});

function fixture(type) {
  const queue = queueFixture({
    bound: true,
    count: 1,
    workDefaults: {
      payload: {
        effect_contract: { version: 1, outputs: [{ type, min: 1, max: 1 }] },
        resource_scope: {
          version: 1,
          resources: [
            { host: "github.com", repository: REPOSITORY, repository_id: "7" },
            { host: "github.com", repository: REPOSITORY, repository_id: "7", ref: "refs/heads/approved" },
          ],
        },
      },
    },
  });
  const assignment = queue.assignment;
  if (!assignment) throw new Error("Missing test assignment");
  const member = assignment.claims[0];
  queue.append("finish", { dispatch_id: assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...queue.workerActor, dispatch_id: assignment.dispatch_id, claim_handle: member.handle });
  const runtime = { assignment, context: queue.workerContext, githubClient: queue.githubClient, workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`, readWorkQueueLog: queue.readWorkQueueLog };
  const authorize = vi.fn(request => authorizeWorkerClaim({ ...runtime, ...request, githubClient: queue.githubClient }));
  process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({ [type]: {} });
  const effects = [];
  const message = { type, claim_handle: member.handle, title: "Requested title", body: "Requested body", ...(type === "create_pull_request" ? { branch: "approved" } : {}) };
  const result = { success: true, repo: REPOSITORY, number: 19, id: "190", url: `https://github.com/${REPOSITORY}/${type === "create_discussion" ? "discussions" : "pull"}/19` };
  const discussion = {
    __typename: "Discussion",
    id: "D_19",
    number: 19,
    url: result.url,
    title: "Final title",
    body: "Final body",
    repository: { id: "R_7", databaseId: 7, nameWithOwner: REPOSITORY },
    category: { id: "C_1" },
    labels: { nodes: [], pageInfo: { hasNextPage: false } },
  };
  const pull = {
    id: 190,
    number: 19,
    html_url: result.url,
    title: "Final title",
    body: "Final body",
    state: "open",
    draft: true,
    labels: [],
    assignees: [],
    head: { sha: COMMIT, ref: "approved", repo: { id: 7, full_name: REPOSITORY } },
    base: { sha: BASE, ref: "main", repo: { id: 7, full_name: REPOSITORY } },
  };
  const client = queue.githubClient;
  Object.assign(client.rest.git, {
    getRef: vi.fn(async () => ({ data: { ref: "refs/heads/approved", object: { type: "commit", sha: COMMIT } } })),
    getCommit: vi.fn(async () => ({ data: { sha: COMMIT, tree: { sha: TREE }, parents: [{ sha: BASE }] } })),
    getTree: vi.fn(async () => ({ data: { sha: TREE, tree: [], truncated: false } })),
  });
  Object.assign(client.rest.repos, { compareCommits: vi.fn(async () => ({ data: { base_commit: { sha: BASE }, merge_base_commit: { sha: BASE }, status: "ahead", ahead_by: 1, behind_by: 0 } })) });
  Object.assign(client.rest, { pulls: { get: vi.fn(async () => ({ data: pull })), create: vi.fn(async () => ({ data: pull })) } });
  client.graphql = vi.fn(async () => ({ node: discussion }));
  const execute = callback => withClaimExecution({ assignment, claim_handle: member.handle, effects, authorize }, callback);
  const capture = async ({ baseBranch = "main", transportBaseBranch = baseBranch, cleanup } = {}) => {
    if (type === "create_discussion") {
      recordClaimEffect({ repository: REPOSITORY, kind: "graphql", id: "discussion-effect", outcome: "succeeded" });
      return captureDiscussionDelivery(result, message, {
        repository: REPOSITORY,
        repositoryNodeId: "R_7",
        id: discussion.id,
        number: 19,
        url: result.url,
        category: "C_1",
        title: "Final title",
        body: "Final body",
        labels: [],
        effectIds: ["discussion-effect"],
        ...(cleanup === undefined ? {} : { cleanup }),
      });
    }
    const wrapped = wrapClaimEffectClient(client, { claim_handle: member.handle, authorize, context: runtime.context });
    await wrapped.rest.pulls.create({ owner: "owner", repo: "repo", title: pull.title, body: pull.body, head: "approved", base: baseBranch, draft: true });
    const exec = {
      getExecOutput: vi.fn(async (_command, args) => {
        const revision = args.at(-1);
        const base = baseBranch !== "main" && revision === `refs/remotes/origin/${baseBranch}^{commit}` ? EFFECTIVE_BASE : BASE;
        return { exitCode: 0, stdout: revision === "HEAD^{tree}" ? TREE : base };
      }),
    };
    const prepared = await preparePullRequestDelivery(exec, baseBranch, transportBaseBranch);
    return capturePullRequestDelivery(
      result,
      message,
      { prepared, repository: REPOSITORY, headRepository: REPOSITORY, id: "190", number: 19, url: result.url, branch: "approved", baseBranch, transportBaseBranch, title: pull.title, body: pull.body, draft: true, labels: [], assignees: [] },
      wrapped
    );
  };
  const settle = () =>
    settleClaimDelivery({ assignment }, [message], [{ messageIndex: 0, success: true, result }], {
      authorize,
      github: client,
      context: runtime.context,
      effects,
      readControlInventory: () => readDeliveryControlInventory({ ...runtime, claim_handle: member.handle }),
    });
  const finalize = () =>
    finalizeWorkerResults({
      ...runtime,
      publishWorkQueueRequest: queue.publishWorkQueueRequest,
      now: queue.at,
      verifyEffects: createClaimDeliveryVerifier({ assignment, recheck: () => execute(settle) }),
    });
  return { queue, assignment, member, message, result, discussion, pull, client, effects, capture, settle, execute, finalize, authorize, context: runtime.context };
}

describe("native creation Claim delivery", () => {
  it.each([2 * MAX_CLOSE_COUNT, 2 * MAX_CLOSE_COUNT + 1])("bounds cleanup readbacks for %i receipts from older discussions", async receiptCount => {
    const f = fixture("create_discussion");
    const count = Math.ceil(receiptCount / 2);
    const discussions = Array.from({ length: count }, (_, index) => ({
      __typename: "Discussion",
      id: `D_old_${index}`,
      number: index + 1,
      closed: true,
      stateReason: "OUTDATED",
      repository: { id: "R_7", databaseId: 7, nameWithOwner: REPOSITORY },
    }));
    const comments = discussions.map(discussion => ({ __typename: "DiscussionComment", id: `DC_${discussion.id}`, body: "Superseded by this run", discussion }));
    f.client.graphql.mockImplementation(async (query, variables) => {
      if (query.includes("WorkQueueEffectTargets")) return { nodes: discussions.filter(discussion => variables.ids.includes(discussion.id)) };
      if (query.includes("addDiscussionComment")) return { addDiscussionComment: { comment: comments.find(comment => comment.discussion.id === variables.dId) } };
      if (query.includes("closeDiscussion")) return { closeDiscussion: { discussion: discussions.find(discussion => discussion.id === variables.dId) } };
      if (query.includes("ClaimDiscussionCleanup")) return { node: [...discussions, ...comments].find(resource => resource.id === variables.id) };
      return { node: f.discussion };
    });
    await f.execute(async () => {
      const wrapped = wrapClaimEffectClient(f.client, { claim_handle: f.member.handle, authorize: f.authorize, context: f.context });
      const cleanup = [];
      for (const [index, discussion] of discussions.entries()) {
        await addDiscussionComment(wrapped, "owner", "repo", discussion.id, comments[index].body, cleanup);
        await closeDiscussionAsOutdated(wrapped, "owner", "repo", discussion.id, cleanup);
      }
      expect(cleanup).toHaveLength(2 * count);
      for (const entry of cleanup) {
        expect(entry).toMatchObject({ type: expect.stringMatching(/^(comment|close)$/), id: expect.any(String), discussionId: expect.any(String), effectId: expect.stringMatching(/^[a-f0-9]{64}$/) });
      }
      await f.capture({ cleanup: cleanup.slice(0, receiptCount) });
      const repositoryReads = vi.spyOn(f.client.rest.repos, "get");
      const proof = await verifyBuiltinDeliveryOutput({ claim: f.member, message: f.message, result: f.result, github: f.client, effects: f.effects });
      expect(proof.verified).toBe(receiptCount === 2 * MAX_CLOSE_COUNT);
      expect(repositoryReads).toHaveBeenCalledTimes(receiptCount === 2 * MAX_CLOSE_COUNT ? 1 : 0);
      repositoryReads.mockRestore();
      if (proof.verified) {
        expect(proof.authority_resources).toHaveLength(count + 1);
        expect(proof.authority_resources.map(target => target.number)).toEqual(["19", ...discussions.map(discussion => String(discussion.number))]);
        expect(proof.effect_resources).toHaveLength(receiptCount + 1);
      }
      f.authorize.mockClear();
      expect((await f.settle()).verification).toBe(receiptCount === 2 * MAX_CLOSE_COUNT ? "verified" : "unknown");
      const resourceChecks = f.authorize.mock.calls.map(([request]) => request.resource).filter(Boolean);
      expect(resourceChecks).toHaveLength(receiptCount === 2 * MAX_CLOSE_COUNT ? 2 * (count + 1) : 0);
      for (const number of ["19", ...discussions.map(discussion => String(discussion.number))]) {
        expect(resourceChecks.filter(target => target.number === number)).toHaveLength(receiptCount === 2 * MAX_CLOSE_COUNT ? 2 : 0);
      }
    });
    expect(f.client.graphql.mock.calls.filter(([query]) => query.includes("ClaimDiscussionCleanup"))).toHaveLength(receiptCount === 2 * MAX_CLOSE_COUNT ? 2 * receiptCount : 0);
  });

  it.each([null, {}, "invalid", false, 0])("rejects a malformed cleanup receipt container (%j) before readback", async cleanup => {
    const f = fixture("create_discussion");
    await f.execute(async () => {
      await f.capture({ cleanup });
      expect((await f.settle()).verification).toBe("unknown");
    });
    expect(f.client.graphql).not.toHaveBeenCalled();
  });

  it.each([
    null,
    "invalid",
    false,
    0,
    [],
    {},
    { type: "invalid" },
    ...["id", "discussionId"].flatMap(field => [undefined, null, 7, "", "x".repeat(257), "private\nid"].map(value => ({ type: "close", id: "D_old", discussionId: "D_old", effectId: "a".repeat(64), [field]: value }))),
    ...[undefined, null, 7, "", "a".repeat(63), "a".repeat(65), "A".repeat(64), "g".repeat(64)].map(effectId => ({ type: "close", id: "D_old", discussionId: "D_old", effectId })),
    { type: "comment", id: "DC_old", discussionId: "D_old", effectId: "a".repeat(64) },
  ])("rejects malformed private cleanup entries before readback: %j", async entry => {
    const f = fixture("create_discussion");
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    delete process.env.DEBUG;
    await f.execute(async () => {
      await f.capture({ cleanup: [{ type: "close", id: "D_old", discussionId: "D_old", effectId: "a".repeat(64) }, entry] });
      expect((await f.settle()).verification).toBe("unknown");
    });
    expect(f.client.graphql).not.toHaveBeenCalled();
    const output = stderr.mock.calls.map(call => call[0]).join("");
    expect(output).toContain("discussion.cleanup.readback.mismatch receipt_valid=false");
    expect(output).not.toContain("creation.readback.unverified");
    expect(output).not.toContain("private");
  });

  it.each([
    ["verified"],
    ["missing_receipts"],
    ["comment_changed"],
    ["reopened"],
    ["reason_changed"],
    ["foreign_repository"],
    ["repository_node_changed"],
    ["repository_id_changed"],
    ["repository_id_missing"],
    ["duplicate_foreign_repository"],
    ["duplicate_invalid_number"],
    ...[undefined, null, 0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1, "", "007", "invalid"].map(number => ["invalid_number", number]),
    ["unavailable"],
  ])("replays Refiner's close-older effects with %s evidence (%j)", async (evidence, number) => {
    const f = fixture("create_discussion");
    const stderr = evidence === "comment_changed" ? vi.spyOn(process.stderr, "write").mockReturnValue(true) : null;
    if (stderr) delete process.env.DEBUG;
    const oldDiscussion = {
      __typename: "Discussion",
      id: "D_old",
      number: 18,
      closed: true,
      stateReason: "OUTDATED",
      repository: { id: "R_7", databaseId: 7, nameWithOwner: REPOSITORY },
    };
    const comment = { __typename: "DiscussionComment", id: "DC_old", body: "Superseded by this run", discussion: oldDiscussion };
    f.client.graphql.mockImplementation(async (query, variables) => {
      if (query.includes("WorkQueueEffectTargets")) return { nodes: [oldDiscussion] };
      if (query.includes("addDiscussionComment")) return { addDiscussionComment: { comment: { id: comment.id, url: "https://github.com/owner/repo/discussions/18#discussioncomment-1" } } };
      if (query.includes("closeDiscussion")) return { closeDiscussion: { discussion: { id: oldDiscussion.id, url: "https://github.com/owner/repo/discussions/18" } } };
      if (query.includes("ClaimDiscussionCleanup")) {
        if (evidence === "unavailable") throw new Error("private readback failure");
        if (variables.id === oldDiscussion.id && evidence === "duplicate_foreign_repository") return { node: { ...oldDiscussion, repository: { ...oldDiscussion.repository, nameWithOwner: "foreign/repo" } } };
        if (variables.id === oldDiscussion.id && evidence === "duplicate_invalid_number") return { node: { ...oldDiscussion, number: 0 } };
        return { node: variables.id === comment.id ? comment : oldDiscussion };
      }
      return { node: f.discussion };
    });
    await f.execute(async () => {
      const wrapped = wrapClaimEffectClient(f.client, { claim_handle: f.member.handle, authorize: f.authorize, context: f.context });
      const cleanup = [];
      await addDiscussionComment(wrapped, "owner", "repo", oldDiscussion.id, comment.body, cleanup);
      await closeDiscussionAsOutdated(wrapped, "owner", "repo", oldDiscussion.id, cleanup);
      expect(cleanup).toHaveLength(2);
      await f.capture({ cleanup: evidence === "missing_receipts" ? [] : cleanup });
      if (evidence === "comment_changed") comment.body = "Changed after publication";
      if (evidence === "reopened") oldDiscussion.closed = false;
      if (evidence === "reason_changed") oldDiscussion.stateReason = "RESOLVED";
      if (evidence === "foreign_repository") oldDiscussion.repository.nameWithOwner = "foreign/repo";
      if (evidence === "repository_node_changed") oldDiscussion.repository.id = "R_8";
      if (evidence === "repository_id_changed") oldDiscussion.repository.databaseId = 8;
      if (evidence === "repository_id_missing") delete oldDiscussion.repository.databaseId;
      if (evidence === "invalid_number") oldDiscussion.number = number;
      const delivery = await f.settle();
      expect(delivery.verification).toBe(evidence === "verified" ? "verified" : "unknown");
      if (evidence === "missing_receipts") expect(delivery.reason).toContain("auxiliary or ambiguous effect");
    });
    const mutationCount = f.client.graphql.mock.calls.filter(([query]) => /\bmutation\b/.test(query)).length;
    expect(mutationCount).toBe(2);
    if (stderr) {
      const output = stderr.mock.calls.map(call => call[0]).join("");
      expect(output).toContain("discussion.cleanup.readback.mismatch comment=true resource_present=true");
      expect(output).not.toContain("Superseded by this run");
      expect(output).not.toContain("Changed after publication");
    }
    if (evidence === "verified") {
      const result = await f.finalize();
      expect(result.claims.h1).toMatchObject({ state: "result", effects: "partial" });
      expect(f.queue.state.works.get(f.member.work_id).barrier).toBe("verified");
      expect(f.client.graphql.mock.calls.filter(([query]) => /\bmutation\b/.test(query))).toHaveLength(2);
      expect((await f.finalize()).claims.h1.state).toBe("result");
      expect(f.queue.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "Result")).toHaveLength(1);
    } else {
      expect(f.queue.state.works.get(f.member.work_id).barrier).toBe("pending");
      expect(f.queue.transactions.flatMap(commit => commit.operations).some(operation => operation.kind === "Result")).toBe(false);
    }
  });

  it("recovers a transient discussion read without replaying the successful creation", async () => {
    const f = fixture("create_discussion");
    await f.execute(async () => {
      await f.capture();
      f.client.graphql.mockRejectedValueOnce({ status: 500 }).mockResolvedValue({ node: f.discussion });
      expect((await f.settle()).verification).toBe("verified");
    });
    expect(f.client.graphql).toHaveBeenCalledTimes(2);
    expect(f.client.graphql.mock.calls.every(([query]) => !/\bmutation\b/.test(query))).toBe(true);
  });

  it.each([
    ["labels_matches", "labels", { nodes: [{ name: "private-label" }], pageInfo: { hasNextPage: false } }],
    ["url_matches", "url", "https://github.com/private-repository/discussions/987654321"],
  ])("diagnoses independent Discussion %s failures without values", async (flag, field, value) => {
    const f = fixture("create_discussion");
    process.env.DEBUG = "work-queue:native_delivery";
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    await f.execute(async () => {
      await f.capture();
      f.discussion[field] = value;
      expect((await f.settle()).verification).toBe("unknown");
    });
    const output = stderr.mock.calls.map(call => call[0]).join("");
    const mismatch = output.split("\n").find(line => line.includes("discussion.readback.mismatch"));
    expect(mismatch).toContain(`${flag}=false`);
    for (const other of ["discussion_present", "identity_matches", "repository_matches", "category_matches", "title_matches", "body_matches", "labels_complete", "labels_matches", "url_matches"]) {
      if (other !== flag) expect(mismatch).toContain(`${other}=true`);
    }
    expect(output).not.toContain("creation.readback.unverified");
    for (const secret of ["private-label", "private-repository", "987654321", REPOSITORY, f.discussion.id, f.member.claim_id, f.member.work_id, f.assignment.dispatch_id, "Final title", "Final body"]) {
      expect(output).not.toContain(secret);
    }
  });

  it.each([
    ["labels_matches", ["labels"], [{ name: "private-label" }]],
    ["assignees_matches", ["assignees"], [{ login: "private-assignee" }]],
    ["url_matches", ["html_url"], "https://github.com/private-repository/pull/987654321"],
    ["repository_id_matches", ["base", "repo", "id"], 987654321],
    ["head_repository_id_matches", ["head", "repo", "id"], "987654321"],
    ["repository_id_matches", ["base", "repo", "id"], undefined],
    ["head_repository_id_matches", ["head", "repo", "id"], undefined],
    ["repository_id_matches", ["base", "repo", "id"], "private-invalid-identity"],
    ["head_repository_id_matches", ["head", "repo", "id"], "private-invalid-identity"],
    ["repository_id_matches", ["base", "repo", "id"], null],
    ["repository_id_matches", ["base", "repo", "id"], {}],
    ["repository_id_matches", ["base", "repo", "id"], []],
    ["repository_id_matches", ["base", "repo", "id"], 0],
    ["repository_id_matches", ["base", "repo", "id"], -1],
    ["repository_id_matches", ["base", "repo", "id"], 1.5],
    ["repository_id_matches", ["base", "repo", "id"], Number.MAX_SAFE_INTEGER + 1],
    ["repository_id_matches", ["base", "repo", "id"], "007"],
    ["identity_matches", ["id"], undefined],
  ])("diagnoses independent PR %s failures at %j without throwing or logging values: %j", async (flag, fields, value) => {
    const f = fixture("create_pull_request");
    process.env.DEBUG = "work-queue:native_delivery";
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    await f.execute(async () => {
      await f.capture();
      const parent = fields.slice(0, -1).reduce((object, field) => object[field], f.pull);
      if (value === undefined) delete parent[fields.at(-1)];
      else parent[fields.at(-1)] = value;
      expect((await f.settle()).verification).toBe("unknown");
    });
    const output = stderr.mock.calls.map(call => call[0]).join("");
    const mismatch = output.split("\n").find(line => line.includes("pull_request.readback.mismatch"));
    expect(mismatch).toContain(`${flag}=false`);
    for (const other of [
      "resource_present",
      "identity_matches",
      "title_matches",
      "body_matches",
      "open",
      "draft_matches",
      "head_matches",
      "base_matches",
      "labels_matches",
      "assignees_matches",
      "url_matches",
      "repository_id_matches",
      "head_repository_id_matches",
    ]) {
      if (other !== flag) expect(mismatch).toContain(`${other}=true`);
    }
    expect(output).not.toContain("creation.readback.unverified");
    for (const secret of [
      "private-label",
      "private-assignee",
      "private-repository",
      "private-invalid-identity",
      "987654321",
      REPOSITORY,
      f.member.claim_id,
      f.member.work_id,
      f.assignment.dispatch_id,
      COMMIT,
      TREE,
      "Final title",
      "Final body",
    ]) {
      expect(output).not.toContain(secret);
    }
  });

  describe.each([
    ["create_discussion", "labels", "labels_matches"],
    ["create_pull_request", "labels", "labels_matches"],
    ["create_pull_request", "assignees", "assignees_matches"],
  ])("%s malformed %s diagnostics", (type, field, flag) => {
    it.each([{}, [null], [undefined], [{ name: { toString: null } }], [{ login: ["private-invalid-name"] }]])("does not introduce exceptions after an earlier rejection: %j", async value => {
      const f = fixture(type);
      process.env.DEBUG = "work-queue:native_delivery";
      const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
      await f.execute(async () => {
        await f.capture();
        const resource = type === "create_discussion" ? f.discussion : f.pull;
        resource.body = "private-body-mismatch";
        resource[field] = type === "create_discussion" ? { nodes: value, pageInfo: { hasNextPage: false } } : value;
        expect((await f.settle()).verification).toBe("unknown");
      });
      const output = stderr.mock.calls.map(call => call[0]).join("");
      expect(output).toContain(`${type === "create_discussion" ? "discussion" : "pull_request"}.readback.mismatch`);
      expect(output).toContain(`${flag}=false`);
      expect(output).toContain("body_matches=false");
      expect(output).not.toContain("creation.readback.unverified");
      expect(output).not.toContain("private-");
    });
  });

  it.each(["create_discussion", "create_pull_request"])("diagnoses %s content mismatches without logging identities or content", async type => {
    const f = fixture(type);
    process.env.DEBUG = "work-queue:delivery,work-queue:native_delivery";
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    const secret = "private-delivery-content-and-token";
    await f.execute(async () => {
      await f.capture();
      if (type === "create_discussion") f.discussion.body = secret;
      else f.pull.body = secret;
      const delivery = await f.settle();
      expect(delivery.verification).toBe("unknown");
      expect(delivery.disposition).not.toBe("none");
    });
    const output = stderr.mock.calls.map(call => call[0]).join("");
    expect(output).toContain(type === "create_discussion" ? "discussion.readback.mismatch" : "pull_request.readback.mismatch");
    expect(output).toContain("body_matches=false");
    expect(output).toContain("delivery.output.proof_invalid index=0");
    for (const value of [secret, REPOSITORY, f.member.claim_id, f.member.work_id, f.assignment.dispatch_id, COMMIT, TREE, "Final body", "Requested body"]) expect(output).not.toContain(value);
  });

  it("logs sanitized readback HTTP failures without provider bodies", async () => {
    const f = fixture("create_pull_request");
    process.env.DEBUG = "work-queue:native_delivery";
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    const secret = "private-provider-body-and-credentials";
    await f.execute(async () => {
      await f.capture();
      f.client.rest.git.getCommit.mockRejectedValue(Object.assign(new Error(secret), { status: 403, response: { data: { message: secret }, headers: { authorization: secret } } }));
      expect((await f.settle()).verification).toBe("unknown");
    });
    const output = stderr.mock.calls.map(call => call[0]).join("");
    expect(output).toContain("creation.readback.unverified failed=true http_status=403");
    expect(output).not.toContain(secret);
  });

  it("always reports exhausted verification budgets without exposing receipts or replaying effects", async () => {
    const f = fixture("create_pull_request");
    process.env.DEBUG = "";
    process.env.ACTIONS_RUNNER_DEBUG = "";
    process.env.RUNNER_DEBUG = "";
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    await f.execute(async () => {
      await f.capture();
      f.pull.body = "private-changed-body";
    });
    f.client.rest.actions.getWorkflowRun = async () => ({ status: 200, data: { ...f.queue.nativeRun(), status: "completed", conclusion: "success" } });
    const finalized = await f.finalize();
    expect(finalized.claims.h1.state).toBe("delivery_failed");
    expect(f.client.rest.pulls.create).toHaveBeenCalledTimes(1);
    const output = stderr.mock.calls.map(call => call[0]).join("");
    expect(output).toContain("delivery.failure.persisted attempts=");
    expect(output).toContain("contract_supported=true receipt_present=false");
    expect(output).toContain("DEBUG=work-queue:claims,work-queue:delivery,work-queue:native_delivery");
    for (const value of ["private-changed-body", REPOSITORY, f.member.claim_id, f.assignment.dispatch_id, COMMIT, TREE]) expect(output).not.toContain(value);
  });

  it("pins the committed nonempty patch tree at the real PR handler preparation boundary", async () => {
    const root = fs.mkdtempSync(path.join(process.cwd(), ".native-pr-boundary-"));
    const checkout = path.join(root, "checkout");
    const runtime = path.join(root, "runtime");
    fs.mkdirSync(checkout);
    fs.mkdirSync(runtime);
    const git = args => {
      const result = spawnSync("git", args, { cwd: checkout, encoding: "utf8", env: { ...process.env, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" } });
      if (result.status !== 0) throw new Error(`Local fixture git ${args[0]} failed: ${result.stderr}`);
      return result.stdout.trim();
    };
    git(["init", "-b", "main"]);
    git(["config", "user.name", "Delivery fixture"]);
    git(["config", "user.email", "delivery@example.test"]);
    fs.writeFileSync(path.join(checkout, "change.txt"), "Before\n");
    git(["add", "change.txt"]);
    git(["commit", "-m", "Base"]);
    const base = git(["rev-parse", "HEAD"]);
    fs.writeFileSync(path.join(checkout, "change.txt"), "Intended patch\n");
    git(["commit", "-am", "Prepared change"]);
    const expectedTree = git(["rev-parse", "HEAD^{tree}"]);
    const patch = path.join(root, "change.patch");
    fs.writeFileSync(patch, `${git(["format-patch", "-1", "--stdout"])}\n`);
    git(["reset", "--hard", base]);
    git(["update-ref", "refs/remotes/origin/main", base]);
    git(["remote", "add", "origin", "https://github.com/owner/repo.git"]);
    const transport = require("./resolve_transport_paths.cjs");
    const native = require("./work_queue_native_creation_delivery.cjs");
    const originalResolve = transport.resolveTransportPaths;
    const originalPrepare = native.preparePullRequestDelivery;
    const handlerPath = require.resolve("./create_pull_request.cjs");
    const originalHandlerModule = require.cache[handlerPath];
    const globals = Object.fromEntries(["github", "context", "exec"].map(key => [key, Object.getOwnPropertyDescriptor(global, key)]));
    const cwd = process.cwd();
    try {
      process.env.TMPDIR = runtime;
      process.env.GIT_CONFIG_NOSYSTEM = "1";
      process.env.GIT_CONFIG_GLOBAL = "/dev/null";
      process.env.RUNNER_TEMP = runtime;
      process.env.GITHUB_WORKSPACE = checkout;
      process.env.GITHUB_TOKEN = "";
      process.env.GITHUB_REPOSITORY_ID = "7";
      process.env.GITHUB_JOB = "safe_outputs";
      process.env.GH_AW_JOB_CHECK_RUN_ID = "789";
      process.env.GH_AW_WORKFLOW_ID = "native-pr-boundary";
      process.env.GH_AW_PROMPTS_DIR = path.resolve(cwd, "../md");
      delete process.env.GH_AW_CI_TRIGGER_TOKEN;
      global.core.startGroup = vi.fn();
      global.core.endGroup = vi.fn();
      global.core.summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn(async () => {}) };
      let prepared;
      native.preparePullRequestDelivery = async (exec, branch) => {
        expect(git(["status", "--porcelain"])).toBe("");
        expect(git(["show", "HEAD:change.txt"])).toBe("Intended patch");
        prepared = await originalPrepare(exec, branch);
        expect(prepared).toEqual({ tree: expectedTree, base, transportBase: base });
        return prepared;
      };
      transport.resolveTransportPaths = () => ({ patchPath: patch, bundlePath: undefined });
      delete require.cache[handlerPath];
      const f = fixture("create_pull_request");
      global.context = f.context;
      global.exec = {
        exec: vi.fn(async (_command, args) => {
          if (args.some(arg => ["fetch", "push", "ls-remote"].includes(arg))) return 0;
          git(args);
          return 0;
        }),
        getExecOutput: vi.fn(async (_command, args) => ({ exitCode: 0, stdout: args.some(arg => ["fetch", "push", "ls-remote"].includes(arg)) ? "" : git(args), stderr: "" })),
      };
      f.client.rest.git.getRef.mockImplementation(async () => ({ data: { ref: "refs/heads/approved", object: { type: "commit", sha: git(["rev-parse", "HEAD"]) } } }));
      f.client.rest.git.getCommit.mockImplementation(async ({ commit_sha }) => ({ data: { sha: commit_sha, tree: { sha: git(["rev-parse", `${commit_sha}^{tree}`]) }, parents: [{ sha: base }] } }));
      f.client.rest.git.getTree.mockImplementation(async ({ tree_sha }) => ({ data: { sha: tree_sha, tree: [], truncated: false } }));
      f.client.rest.repos.compareCommits.mockImplementation(async ({ head }) => ({
        data: { base_commit: { sha: base }, merge_base_commit: { sha: git(["merge-base", base, head]) }, status: "ahead", ahead_by: Number(git(["rev-list", "--count", `${base}..${head}`])), behind_by: 0 },
      }));
      f.client.rest.pulls.create.mockImplementation(async parameters => {
        expect(prepared).toEqual({ tree: expectedTree, base, transportBase: base });
        Object.assign(f.pull, parameters, { head: { sha: git(["rev-parse", "HEAD"]), ref: "approved", repo: { full_name: REPOSITORY, id: 7 } }, base: { sha: base, ref: "main", repo: { full_name: REPOSITORY, id: 7 } } });
        return { data: f.pull };
      });
      process.chdir(checkout);
      await f.execute(async () => {
        global.github = wrapClaimEffectClient(f.client, { claim_handle: f.member.handle, authorize: f.authorize, context: f.context });
        const handler = await require("./create_pull_request.cjs").main({ base_branch: "main", preserve_branch_name: true, signed_commits: false, footer: false });
        const result = await handler(f.message, {});
        expect(result.success, JSON.stringify(result)).toBe(true);
        const proof = await verifyBuiltinDeliveryOutput({ claim: f.member, message: f.message, result, github: f.client, effects: f.effects });
        expect(proof.verified).toBe(true);
        expect(proof.evidence.tree).toBe(expectedTree);
      });
    } finally {
      process.chdir(cwd);
      transport.resolveTransportPaths = originalResolve;
      native.preparePullRequestDelivery = originalPrepare;
      if (originalHandlerModule) require.cache[handlerPath] = originalHandlerModule;
      else delete require.cache[handlerPath];
      for (const [key, descriptor] of Object.entries(globals)) {
        if (descriptor) Object.defineProperty(global, key, descriptor);
        else delete global[key];
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it.each(["create_discussion", "create_pull_request"])("settles %s through the actual manager verifier and publishes a protected Result", async type => {
    const f = fixture(type);
    await f.execute(async () => {
      await f.capture();
      expect(closeClaimEffectChannel()).toBeDefined();
      const delivery = await f.settle();
      expect(delivery.verification, delivery.reason).toBe("verified");
      expect(delivery.descriptor.outputs[0].resource.kind).toBe(type === "create_discussion" ? "discussion" : "pull_request");
    });
    const finalized = await f.finalize();
    expect(finalized.claims.h1.state).toBe("result");
    expect(f.queue.state.works.get(f.member.work_id).barrier).toBe("verified");
  });

  it("settles the enriched PR message emitted by the safe-output transport", async () => {
    const f = fixture("create_pull_request");
    Object.assign(f.message, { head_repo: REPOSITORY, base_branch: "main", base_commit: BASE });
    await f.execute(async () => {
      await f.capture();
      expect((await f.settle()).verification).toBe("verified");
    });
    expect((await f.finalize()).claims.h1.state).toBe("result");
    expect(f.queue.state.works.get(f.member.work_id).barrier).toBe("verified");
  });

  it("independently checks an older transport base against the prepared PR base", async () => {
    const f = fixture("create_pull_request");
    Object.assign(f.message, { head_repo: REPOSITORY, base_branch: "main", base_commit: REF });
    f.client.rest.repos.compareCommits.mockImplementation(async ({ base }) => ({
      data: { base_commit: { sha: base }, merge_base_commit: { sha: base }, status: "ahead", ahead_by: 1, behind_by: 0 },
    }));
    await f.execute(async () => {
      await f.capture();
      expect((await f.settle()).verification).toBe("verified");
      expect(f.client.rest.repos.compareCommits).toHaveBeenCalledWith(expect.objectContaining({ base: REF, head: BASE }));
    });
  });

  it.each([
    { requested: "release", effective: "release" },
    { requested: "first-pr", effective: "generated/first-pr" },
  ])("verifies transport base metadata separately from effective PR base for $requested", async ({ requested, effective }) => {
    const f = fixture("create_pull_request");
    Object.assign(f.message, { base: requested, base_branch: "main", base_commit: REF });
    f.pull.base = { sha: EFFECTIVE_BASE, ref: effective, repo: { id: 7, full_name: REPOSITORY } };
    f.client.rest.repos.compareCommits.mockImplementation(async ({ base }) => ({
      data: { base_commit: { sha: base }, merge_base_commit: { sha: base }, status: "ahead", ahead_by: 1, behind_by: 0 },
    }));
    await f.execute(async () => {
      await f.capture({ baseBranch: effective, transportBaseBranch: "main" });
      expect((await f.settle()).verification).toBe("verified");
    });
    expect(f.client.rest.repos.compareCommits).toHaveBeenCalledWith(expect.objectContaining({ base: REF, head: BASE }));
    expect(f.client.rest.repos.compareCommits).toHaveBeenCalledWith(expect.objectContaining({ base: EFFECTIVE_BASE, head: COMMIT }));
  });

  it.each([{ head_repo: "foreign/repo" }, { head_repo: null }, { base_branch: "foreign" }, { base_branch: null }, { base_commit: "not-a-commit" }, { base_commit: null }, { unexpected_transport_field: "untrusted" }])(
    "rejects captured but invalid PR transport metadata %j",
    async metadata => {
      const f = fixture("create_pull_request");
      Object.assign(f.message, metadata);
      await f.execute(async () => {
        await f.capture();
        expect((await f.settle()).verification).toBe("unknown");
        expect(f.client.rest.pulls.get).not.toHaveBeenCalled();
      });
    }
  );

  it("rejects a valid-looking transport base without independent ancestry", async () => {
    const f = fixture("create_pull_request");
    f.message.base_commit = REF;
    await f.execute(async () => {
      await f.capture();
      expect((await f.settle()).verification).toBe("unknown");
      expect(f.client.rest.pulls.get).not.toHaveBeenCalled();
    });
  });

  it.each(["create_discussion", "create_pull_request"])("does not accept %s handler success or a copied private receipt as delivery proof", async type => {
    const f = fixture(type);
    await f.execute(async () => {
      expect((await verifyBuiltinDeliveryOutput({ claim: f.member, message: f.message, result: f.result, github: f.client })).verified).toBe(false);
      await f.capture();
      expect((await verifyBuiltinDeliveryOutput({ claim: f.member, message: f.message, result: structuredClone(f.result), github: f.client })).verified).toBe(false);
    });
    f.discussion.body = "Tampered";
    f.pull.head.sha = BASE;
    const finalized = await f.finalize();
    expect(finalized.claims.h1.state).toBe("pending");
    expect(f.queue.state.works.get(f.member.work_id).barrier).not.toBe("verified");
  });

  it.each(["create_discussion", "create_pull_request"])("does not settle a %s contract from a successful fallback Issue", async type => {
    const f = fixture(type);
    await f.execute(async () => {
      await f.capture();
      Object.assign(f.result, { fallback_used: true, url: `https://github.com/${REPOSITORY}/issues/19` });
      f.client.graphql.mockClear();
      f.client.rest.pulls.get.mockClear();
      f.client.rest.git.getRef.mockClear();
      const delivery = await f.settle();
      expect(delivery.verification).toBe("unknown");
      expect(delivery.disposition).toBe("unknown");
      expect(delivery.reason).toBe("Independent scoped resource verification failed");
    });
    expect((await f.finalize()).claims.h1).toMatchObject({ state: "pending", effects: "unknown" });
    expect(f.queue.state.works.get(f.member.work_id).barrier).toBe("pending");
    f.client.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...f.queue.nativeRun(), status: "completed", conclusion: "failure" } });
    expect((await f.finalize()).claims.h1).toMatchObject({ state: "delivery_failed", effects: "unknown" });
    expect(f.queue.state.works.get(f.member.work_id).barrier).toBe("failed");
    const operations = () => f.queue.transactions.flatMap(commit => commit.operations);
    const failure = operations().find(operation => operation.kind === "DeliveryFailure");
    expect(failure).toMatchObject({ reason: "verification_exhausted", disposition: "unknown" });
    expect((await f.finalize()).claims.h1).toMatchObject({ state: "delivery_failed", effects: "unknown" });
    expect(operations().filter(operation => operation.kind === "DeliveryFailure")).toEqual([failure]);
    expect(operations().some(operation => operation.kind === "Result")).toBe(false);
    expect(f.client.graphql).not.toHaveBeenCalled();
    expect(f.client.rest.pulls.get).not.toHaveBeenCalled();
    expect(f.client.rest.git.getRef).not.toHaveBeenCalled();
    expect(f.client.rest.pulls.create).toHaveBeenCalledTimes(type === "create_pull_request" ? 1 : 0);
  });

  it.each(["id", "repository", "category", "title", "body", "labels", "unavailable"])("rejects mismatched discussion %s evidence", async mismatch => {
    const f = fixture("create_discussion");
    await f.execute(async () => {
      await f.capture();
      if (mismatch === "repository") f.discussion.repository.id = "R_foreign";
      else if (mismatch === "category") f.discussion.category.id = "C_foreign";
      else if (mismatch === "labels") f.discussion.labels.pageInfo.hasNextPage = true;
      else if (mismatch === "unavailable") f.client.graphql.mockRejectedValue(new Error("Unavailable"));
      else f.discussion[mismatch] = "Different";
      expect((await f.settle()).verification).toBe("unknown");
    });
  });

  it.each(["identity", "repository", "head", "base", "tree", "commit", "ancestry", "ref", "unavailable"])("rejects mismatched PR %s evidence without deriving no effects", async mismatch => {
    const f = fixture("create_pull_request");
    await f.execute(async () => {
      await f.capture();
      if (mismatch === "identity") f.pull.id = 191;
      if (mismatch === "repository") f.pull.head.repo.id = 8;
      if (mismatch === "head") f.pull.head.ref = "foreign";
      if (mismatch === "base") f.pull.base.ref = "foreign";
      if (mismatch === "tree") f.client.rest.git.getTree.mockResolvedValue({ data: { sha: BASE, tree: [], truncated: false } });
      if (mismatch === "commit") f.client.rest.git.getCommit.mockResolvedValue({ data: { sha: COMMIT, tree: { sha: BASE }, parents: [{ sha: BASE }] } });
      if (mismatch === "ancestry") f.client.rest.repos.compareCommits.mockResolvedValue({ data: { base_commit: { sha: BASE }, merge_base_commit: { sha: REF }, status: "diverged", ahead_by: 1, behind_by: 1 } });
      if (mismatch === "ref") f.client.rest.git.getRef.mockResolvedValue({ data: { ref: "refs/heads/approved", object: { type: "commit", sha: BASE } } });
      if (mismatch === "unavailable") f.client.rest.git.getCommit.mockRejectedValue(new Error("Unavailable"));
      const delivery = await f.settle();
      expect(delivery.verification).toBe("unknown");
      expect(delivery.disposition).not.toBe("none");
    });
  });

  it("leaves a PR unverified when native capture cannot validate its trusted prepared tree", async () => {
    const f = fixture("create_pull_request");
    await f.execute(async () => {
      f.client.rest.git.getCommit.mockResolvedValue({ data: { sha: COMMIT, tree: { sha: BASE }, parents: [{ sha: BASE }] } });
      await f.capture();
      f.client.rest.git.getCommit.mockResolvedValue({ data: { sha: COMMIT, tree: { sha: TREE }, parents: [{ sha: BASE }] } });
      expect((await f.settle()).verification).toBe("unknown");
    });
  });

  it.each([false, true])("independently accounts for a signed commit with foreign ancestry=%s", async foreign => {
    const f = fixture("create_pull_request");
    await f.execute(async () => {
      await f.capture();
      f.client.graphql.mockResolvedValue({ createCommitOnBranch: { commit: { oid: foreign ? REF : COMMIT } } });
      const wrapped = wrapClaimEffectClient(f.client, { claim_handle: f.member.handle, authorize: f.authorize, context: f.context });
      await wrapped.graphql("mutation NativeCommit($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }", {
        input: { branch: { repositoryNameWithOwner: REPOSITORY, branchName: "approved" }, expectedHeadOid: BASE, message: { headline: "Prepared change" }, fileChanges: {} },
      });
      expect(f.effects.at(-1).commit).toBe(foreign ? REF : COMMIT);
      const delivery = await f.settle();
      expect(delivery.verification).toBe(foreign ? "unknown" : "verified");
    });
  });

  it.each([false, true])("covers only native PR-label updates with an independently matching parent=%s", async foreign => {
    const f = fixture("create_pull_request");
    await f.execute(async () => {
      await f.capture();
      recordClaimEffect({ repository: REPOSITORY, kind: "issue", number: 19, expected: { labels: [] }, outcome: "succeeded" });
      f.client.rest.issues = {
        get: vi.fn(async () => ({ data: { id: 191, number: 19, labels: [], pull_request: { url: `https://api.github.com/repos/${REPOSITORY}/pulls/${foreign ? 20 : 19}` } } })),
      };
      const delivery = await f.settle();
      expect(delivery.verification).toBe(foreign ? "unknown" : "verified");
    });
  });

  it.each(["create_discussion", "create_pull_request"])("rejects changed input, a foreign repository, and a foreign original %s Claim", async type => {
    const f = fixture(type);
    await f.execute(async () => {
      await f.capture();
      for (const overrides of [{ message: { ...f.message, body: "Changed input" } }, { message: { ...f.message, repo: "foreign/repo" } }, { claim: { ...f.member, claim_id: "foreign" } }]) {
        const proof = await verifyBuiltinDeliveryOutput({ claim: f.member, message: f.message, result: f.result, github: f.client, ...overrides });
        expect(proof.verified).toBe(false);
      }
    });
  });
});
