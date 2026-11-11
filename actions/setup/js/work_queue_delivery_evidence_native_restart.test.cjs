import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { createHarness } = require("./work_queue_delivery_evidence.test_helpers.cjs");
const TREE = "b".repeat(40);
const COMMIT = "c".repeat(40);
const BASE = "d".repeat(40);
const CHANGED_COMMIT = "f".repeat(40);
const BRANCH = "durable-report";

function observedNative(h, tamper) {
  h.state.readbacks++;
  const native = structuredClone(h.state.native);
  if (h.state.unavailable) {
    h.state.unavailableReadbacks++;
    native.body = "Native content has not become readable yet";
  } else if (h.state.tampered) {
    if (tamper === "head") native.head.sha = CHANGED_COMMIT;
    else native[tamper] = "Changed after evidence publication";
  }
  return native;
}

function setupDiscussion(h, tamper) {
  h.github.graphql = async (query, variables) => {
    if (query.includes("WorkQueueEffectTargets")) {
      return {
        nodes: variables.ids.map(id =>
          id === "R_7"
            ? { __typename: "Repository", id, databaseId: 7, nameWithOwner: "owner/repo" }
            : { __typename: id === "D_19" ? "Discussion" : "DiscussionCategory", id, ...(id === "D_19" ? { number: 19 } : {}), repository: { nameWithOwner: "owner/repo", databaseId: 7 } }
        ),
      };
    }
    if (query.includes("discussionCategories")) return { repository: { id: "R_7", discussionCategories: { nodes: [{ id: "C_1", name: "Reports", slug: "reports" }] } } };
    if (query.includes("createDiscussion")) {
      h.state.writes++;
      h.state.native = {
        __typename: "Discussion",
        id: "D_19",
        number: 19,
        url: "https://github.com/owner/repo/discussions/19",
        title: variables.title,
        body: variables.body,
        repository: { id: "R_7", databaseId: 7, nameWithOwner: "owner/repo" },
        category: { id: "C_1" },
        labels: { nodes: [], pageInfo: { hasNextPage: false } },
      };
      return { createDiscussion: { discussion: structuredClone(h.state.native) } };
    }
    if (query.includes("ClaimDiscussionDelivery")) {
      expect(variables.id).toBe("D_19");
      return { node: observedNative(h, tamper) };
    }
    throw new Error(`Unexpected discussion query: ${query}`);
  };
}

function setupPullRequest(h, tamper) {
  global.exec = {
    exec: async (_command, args) => {
      if (args[0] === "push") h.state.writes++;
      return 0;
    },
    getExecOutput: async (_command, args) => ({
      exitCode: 0,
      stdout: args.at(-1) === "HEAD^{tree}" ? TREE : args.at(-1) === "refs/remotes/origin/main^{commit}" ? BASE : "",
      stderr: "",
    }),
  };
  const readQueueRef = h.github.rest.git.getRef;
  Object.assign(h.github.rest.git, {
    getRef: async parameters => {
      if (parameters.ref !== `heads/${BRANCH}`) return readQueueRef(parameters);
      h.state.codeReadbacks++;
      return { data: { ref: `refs/heads/${BRANCH}`, object: { type: "commit", sha: COMMIT } } };
    },
    getCommit: async ({ commit_sha }) => {
      expect(commit_sha).toBe(COMMIT);
      return { data: { sha: COMMIT, tree: { sha: TREE }, parents: [{ sha: BASE }] } };
    },
    getTree: async ({ tree_sha }) => {
      expect(tree_sha).toBe(TREE);
      return { data: { sha: TREE, tree: [], truncated: false } };
    },
  });
  h.github.rest.repos.compareCommits = async ({ base, head }) => {
    expect({ base, head }).toEqual({ base: BASE, head: COMMIT });
    return { data: { base_commit: { sha: BASE }, merge_base_commit: { sha: BASE }, status: "ahead", ahead_by: 1, behind_by: 0 } };
  };
  h.github.rest.pulls = {
    create: async parameters => {
      h.state.writes++;
      h.state.native = {
        ...parameters,
        id: 190,
        node_id: "PR_19",
        number: 19,
        html_url: "https://github.com/owner/repo/pull/19",
        state: "open",
        labels: [],
        assignees: [],
        head: { sha: COMMIT, ref: parameters.head, repo: { full_name: "owner/repo", id: 7 } },
        base: { sha: BASE, ref: "main", repo: { full_name: "owner/repo", id: 7 } },
      };
      return { data: structuredClone(h.state.native) };
    },
    // Capture reads the pinned ref/commit/tree successfully; only subsequent
    // independent PR-content verification sees the transient mismatch.
    get: async ({ pull_number }) => {
      expect(pull_number).toBe(19);
      return { data: observedNative(h, tamper) };
    },
  };
}

const scenarios = [
  ["create_discussion", "none"],
  ["create_discussion", "title"],
  ["create_discussion", "body"],
  ["create_pull_request", "none"],
  ["create_pull_request", "title"],
  ["create_pull_request", "body"],
  ["create_pull_request", "head"],
];

describe("durable native creation delivery after a process restart", () => {
  it.each(scenarios)("independently rechecks %s after restart with tamper=%s without repeating writes", async (type, tamper) => {
    const h = createHarness({
      type,
      messages: [{ type, title: "Required report", body: "Report body", ...(type === "create_pull_request" ? { branch: BRANCH } : {}) }],
      config: {
        [type]: {
          max: 1,
          footer: false,
          ...(type === "create_discussion" ? { category: "Reports", fallback_to_issue: false } : { allow_empty: true, base_branch: "main", preserve_branch_name: true }),
        },
      },
      setupNative(harness) {
        Object.assign(harness.state, { readbacks: 0, unavailableReadbacks: 0, codeReadbacks: 0 });
        if (type === "create_discussion") setupDiscussion(harness, tamper);
        else setupPullRequest(harness, tamper);
      },
    });
    try {
      const member = h.fixture.assignment.claims[0];
      const worker = await h.workerPass();
      expect(worker.settlements[0].success, JSON.stringify(h.logs)).toBe(true);
      expect(worker.settlements[0].delivery.verification).toBe("unknown");
      expect(h.state.terminal).toBe(false);
      expect(h.state.unavailableReadbacks).toBeGreaterThan(0);
      expect(h.state.writes).toBe(1);
      expect(h.projection.works.get(member.work_id).barrier).toBe("pending");
      expect(h.operations("Result")).toHaveLength(0);
      expect(h.operations("DeliveryFailure")).toHaveLength(0);

      const evidence = h.operations("DeliveryEvidence");
      expect(evidence, JSON.stringify(h.logs)).toHaveLength(1);
      expect(evidence[0]).toMatchObject({ claim_id: member.claim_id, run_id: "42", run_attempt: 1 });
      expect(h.projection.deliveryEvidence.get(member.claim_id)).toEqual(evidence[0]);
      expect(evidence[0].outputs).toHaveLength(1);
      expect(evidence[0].outputs[0]).toMatchObject({ index: 0, type, verifier: "native_creation" });
      const { digest } = require("./work_queue_codec.cjs");
      const receipt = evidence[0].outputs[0].receipt;
      expect(receipt).toMatchObject({
        number: 19,
        repository: "owner/repo",
        title_digest: digest(h.state.native.title),
        body_digest: digest(h.state.native.body),
        message_digest: expect.stringMatching(/^[a-f0-9]{64}$/),
      });
      for (const field of ["title", "body", "message"]) expect(receipt).not.toHaveProperty(field);
      expect(evidence[0].effects.some(effect => effect.outcome === "succeeded")).toBe(true);
      if (type === "create_pull_request") {
        expect(receipt).toMatchObject({ commit: COMMIT, tree: TREE, base: BASE, transportBase: BASE, branch: BRANCH });
        expect(h.state.codeReadbacks).toBeGreaterThan(0);
      }

      const writesBeforeRestart = h.state.writes;
      const readbacksBeforeRestart = h.state.readbacks;
      h.restartForMaintenance({ tampered: tamper !== "none" });
      const maintained = await h.maintenance();
      const settlement = maintained.results.find(result => result.dispatch_id === h.fixture.assignment.dispatch_id);
      expect(settlement?.delivery?.claims.h1.state, JSON.stringify(h.logs)).toBe(tamper === "none" ? "result" : "delivery_failed");
      expect(h.state.readbacks).toBeGreaterThan(readbacksBeforeRestart);
      expect(h.settlement(member)).toEqual({ barrier: tamper === "none" ? "verified" : "failed", evidenceRetained: false, results: tamper === "none" ? 1 : 0, failures: tamper === "none" ? 0 : 1, writes: writesBeforeRestart });
      expect(h.operations("DeliveryEvidence")).toHaveLength(1);

      if (tamper === "none") {
        const results = h.operations("Result");
        expect(results[0].descriptor.outputs).toHaveLength(1);
        expect(results[0].descriptor.outputs[0].resource).toMatchObject({ kind: type === "create_discussion" ? "discussion" : "pull_request", repository: "owner/repo", number: 19 });
      } else {
        const failures = h.operations("DeliveryFailure");
        expect(failures[0]).toMatchObject({ claim_id: member.claim_id, reason: "verification_exhausted" });
        expect(failures[0].evidence.attempts).toBeGreaterThan(0);
      }
    } finally {
      h.dispose();
    }
  });
});
