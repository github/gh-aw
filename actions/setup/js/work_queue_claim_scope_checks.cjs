"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const scope = require("./work_queue_claim_scope.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { verifyClaimDelivery, validateDeliveryContract, verifyBuiltinDeliveryOutput } = require("./work_queue_delivery.cjs");
const manager = require("./safe_output_handler_manager.cjs");

/** @param {number} [count] @param {Record<string, unknown>} [contract] */
function assignment(count = 1, contract = { version: 1, outputs: [], no_writes: true }) {
  return {
    version: 3,
    dispatch_id: "dispatch",
    request_id: "request",
    commit_id: "commit",
    policy_epoch: "policy",
    pool: "default",
    worker_profile: "default",
    claims: Array.from({ length: count }, (_, index) => {
      /** @type {Record<string, unknown> & {effect_contract?: Record<string, unknown>}} */
      const work = { effect_contract: contract };
      /** @type {unknown[]} */
      const result_refs = [];
      return { handle: `h${index + 1}`, claim_id: `c${index + 1}`, work_id: `w${index + 1}`, work, result_refs };
    }),
  };
}

const authorized = async request => ({ authorized: true, claim_handle: request.claim_handle });

/** @param {unknown} error @param {string} state @param {string} [code] */
function isSuppressedClaimError(error, state, code) {
  return error instanceof Error && "suppressed" in error && error.suppressed === true && "state" in error && error.state === state && (code === undefined || ("code" in error && error.code === code));
}

function registerTests({ describe, it }) {
  describe("universal immutable Claim-scoped safe outputs", () => {
    it("matches the shared canonical assignment fixture unchanged and freezes its original membership", () => {
      const fixture = JSON.parse(fs.readFileSync(path.resolve(__dirname, "../../../specs/work-queue/fixtures/canonical-prefix.json"), "utf8"));
      const normalized = scope.normalizeAssignment(fixture.assignment);
      assert.equal(canonical(normalized), canonical(fixture.assignment));
      assert.ok(Object.isFrozen(normalized.claims));
      assert.ok(Object.isFrozen(normalized.claims[0].work));
      assert.throws(() => scope.normalizeClaimScope({ type: "noop" }, normalized), /original multi-Claim/);
    });

    it("does not reuse local handles, factory contexts or receipts across immutable dispatch identities", async () => {
      const first = assignment();
      const second = assignment();
      second.dispatch_id = "another-dispatch";
      second.claims[0].claim_id = "another-claim";
      second.claims[0].work_id = "another-work";
      const root = "/workspace/artifacts";
      assert.notEqual(scope.claimArtifactPath(root, "h1", first), scope.claimArtifactPath(root, "h1", second));
      assert.throws(() => scope.claimArtifactPath(root, "h1"), /original immutable assignment/);
      const original = await scope.withClaimExecution({ assignment: first, claim_handle: "h1" }, () => scope.claimIdentity("h1"));
      assert.equal(scope.receiptMatchesClaim(original, first.claims[0]), false);
      await scope.withClaimExecution({ assignment: first, claim_handle: "h1" }, () => {
        assert.equal(scope.receiptMatchesClaim(original, first.claims[0]), true);
      });
      assert.equal(scope.receiptMatchesClaim(original, second.claims[0]), false);
      await scope.withClaimExecution({ assignment: second, claim_handle: "h1" }, () => {
        assert.throws(() => scope.assertClaimIdentity(original), /original immutable Claim identity/);
      });
    });

    it("pins manifest writers to the full original Claim and persists immutable attribution", async () => {
      const { createManifestLogger } = require("./safe_output_manifest.cjs");
      const root = path.resolve(".queue-validation-cache", `claim-manifest-${require("crypto").randomUUID()}`);
      const first = assignment();
      const second = assignment();
      second.dispatch_id = "different-dispatch";
      second.claims[0].claim_id = "different-claim";
      let logger;
      try {
        fs.mkdirSync(root, { recursive: true });
        await scope.withClaimExecution({ assignment: first, claim_handle: "h1" }, async () => {
          logger = createManifestLogger(path.join(root, "items.jsonl"));
          logger({ type: "create_issue", number: 1 });
          const entries = fs
            .readFileSync(path.join(scope.claimArtifactPath(root, "h1"), "items.jsonl"), "utf8")
            .trim()
            .split("\n")
            .map(line => JSON.parse(line));
          assert.deepEqual({ dispatch_id: entries[0].dispatch_id, claim_id: entries[0].claim_id, work_id: entries[0].work_id, claim_handle: entries[0].claim_handle }, scope.claimIdentity("h1"));
        });
        await scope.withClaimExecution({ assignment: second, claim_handle: "h1" }, () => {
          assert.throws(() => logger({ type: "create_issue", number: 2 }), /original immutable Claim identity/);
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("normalizes all output families and never defaults a batch after sibling closure", () => {
      const singleton = scope.normalizeAssignment(assignment());
      const batch = scope.normalizeAssignment(assignment(3));
      for (const type of [
        "create_issue",
        "custom_job",
        "custom_action",
        "custom_script",
        "upload_asset",
        "upload_artifact",
        "noop",
        "missing_tool",
        "missing_data",
        "report_incomplete",
        "work_queue_submit",
        "work_queue_dispatch_next",
        "work_queue_claim_finish",
      ]) {
        assert.equal(scope.normalizeClaimScope({ type }, singleton).claim_handle, "h1");
        assert.throws(() => scope.normalizeClaimScope({ type }, batch), /multi-Claim/);
        assert.equal(scope.normalizeClaimScope({ type, claim_handle: "h3" }, batch).claim_handle, "h3");
      }
      assert.ok(Object.isFrozen(batch.claims));
      assert.ok(Object.isFrozen(batch.claims[0].work));
      assert.equal(scope.normalizeClaimScope({ type: "approve_workflow_run", run_id: "123" }, singleton).run_id, "123");
    });

    it("rejects explicit selectors, authority replacement, byte overflows and malformed Result references", () => {
      for (const claim_handle of [null, "", false, {}, [], 123, "foreign"]) {
        assert.throws(() => scope.normalizeClaimScope({ claim_handle }, assignment()));
      }
      for (const field of ["claim_id", "work_id"]) assert.throws(() => scope.normalizeClaimScope({ [field]: "foreign" }, assignment()), /conflicts/);
      for (const field of ["assignment", "work_queue_assignment", "work_queue_claim", "run_id", "run_attempt", "authorized"]) {
        assert.throws(() => scope.normalizeClaimScope({ [field]: null }, assignment()), /trusted authority/);
      }
      const oversized = assignment();
      oversized.claims[0].work.large = "x".repeat(48 * 1024);
      assert.throws(() => scope.normalizeAssignment(oversized), /48 KiB/);
      const inherited = Object.create({ worker_profile: "default" });
      Object.assign(inherited, assignment());
      delete inherited.worker_profile;
      assert.throws(() => scope.normalizeAssignment(inherited), /JSON/);
      for (const result of [null, {}, { work_id: "dep", result_commit_id: "r", descriptor: null }, { work_id: "dep", result_commit_id: "r", descriptor: {}, authorized: true }]) {
        const invalid = assignment();
        invalid.claims[0].result_refs = [result];
        assert.throws(() => scope.normalizeAssignment(invalid));
      }
    });

    it("requires fresh same-Claim proof without a global batch authorization or context escape", async () => {
      await scope.withClaimExecution({ assignment: assignment(2), claim_handle: "h1", authorize: authorized }, async () => {
        assert.throws(() => scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize: authorized }, () => {}), /original immutable assignment/);
        await assert.rejects(scope.assertClaimAuthorized({ type: "noop", claim_handle: "h2" }), /escape/);
        await assert.rejects(scope.assertClaimAuthorized({ type: "noop", claim_handle: "h1" }, { authorize: async () => ({ authorized: true, claim_handle: "h2" }) }), /same-Claim/);
        await assert.rejects(scope.assertClaimAuthorized({ type: "noop", claim_handle: "h1" }, { authorize: async request => ({ authorized: false, claim_handle: request.claim_handle, state: "cancelled", suppressed: true }) }), error =>
          isSuppressedClaimError(error, "cancelled")
        );
        await assert.rejects(scope.assertClaimAuthorized({ type: "noop", claim_handle: "h1" }, { authorize: async request => ({ authorized: false, claim_handle: request.claim_handle, state: "result", suppressed: true }) }), error =>
          isSuppressedClaimError(error, "result", "claim_already_settled")
        );
        for (const proof of [
          { authorized: false, claim_handle: "h1", state: "cancelled" },
          { authorized: false, claim_handle: "h1", state: "cancelled", suppressed: false },
          { authorized: false, claim_handle: "h1", state: "open", suppressed: true },
          { authorized: false, claim_handle: "h1", status: "cancelled", suppressed: true },
          { authorized: false, claim_handle: "h1", state: "cancelled", suppressed: "true" },
          { authorized: false, claim_handle: "h2", state: "cancelled", suppressed: true },
          { authorized: true, claim_handle: "h1", state: "result", suppressed: true },
        ]) {
          await assert.rejects(scope.assertClaimAuthorized({ type: "noop", claim_handle: "h1" }, { authorize: async () => proof }), error => error instanceof Error && (!("suppressed" in error) || error.suppressed !== true));
        }
      });
      const first = await scope.withClaimExecution({ assignment: assignment(2), claim_handle: "h1" }, () => scope.scopedArtifactFilename("/workspace/artifacts/aw.patch"));
      const second = await scope.withClaimExecution({ assignment: assignment(2), claim_handle: "h2" }, () => scope.scopedArtifactFilename("/workspace/artifacts/aw.patch"));
      assert.notEqual(first, second);
      const opaque = assignment();
      opaque.claims[0].handle = "../../escape";
      assert.match(scope.claimArtifactPath("/workspace/artifacts", "../../escape", opaque), /^\/workspace\/artifacts\/claims\/[a-f0-9]{64}$/);
    });

    it("does not infer no-write delivery from missing contracts, delegation or job success", async () => {
      assert.throws(() => validateDeliveryContract({ version: 1, outputs: [] }));
      const absent = assignment();
      delete absent.claims[0].work.effect_contract;
      const missing = await verifyClaimDelivery({ assignment: absent, claim_handle: "h1", authorize: authorized });
      assert.equal(missing.verification, "unknown");
      assert.equal(missing.disposition, "unknown");
      const contract = { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1 }] };
      const delegated = await verifyClaimDelivery({
        assignment: assignment(1, contract),
        claim_handle: "h1",
        authorize: authorized,
        messages: [{ type: "create_issue" }],
        results: [{ messageIndex: 0, success: true, delegated: true }],
      });
      assert.equal(delegated.verification, "unknown");
      assert.equal(delegated.disposition, "unknown");
      const denied = await verifyClaimDelivery({ assignment: assignment(), claim_handle: "h1", authorize: async () => ({ authorized: false, claim_handle: "h1" }) });
      assert.equal(denied.verification, "unknown");
      const settled = await verifyClaimDelivery({ assignment: assignment(), claim_handle: "h1", authorize: async () => ({ authorized: false, claim_handle: "h1", state: "result", suppressed: true }) });
      assert.equal(settled.verification, "result");
      assert.notEqual(settled.verification, "cancelled");
      let previewChecks = 0;
      const preview = await verifyClaimDelivery({
        assignment: assignment(),
        claim_handle: "h1",
        staged: true,
        authorize: async request => {
          previewChecks++;
          assert.equal(request.requireCompletion, false);
          return { authorized: false, claim_handle: "h1" };
        },
      });
      assert.equal(previewChecks, 1);
      assert.equal(preview.verification, "unknown");
    });

    it("settles completed/cancelled/completed independently and preserves the Result barrier", async () => {
      const batch = assignment(3);
      const proofs = [];
      const authorize = async request => {
        proofs.push(request.claim_handle);
        assert.equal(request.requireCompletion, true);
        return request.claim_handle === "h2" ? { authorized: false, claim_handle: "h2", state: "cancelled", suppressed: true } : { authorized: true, claim_handle: request.claim_handle };
      };
      const deliveries = [];
      for (const member of batch.claims) deliveries.push(await verifyClaimDelivery({ assignment: batch, claim_handle: member.handle, authorize }));
      assert.deepEqual(
        deliveries.map(delivery => delivery.verification),
        ["verified", "cancelled", "verified"]
      );
      assert.deepEqual(proofs, ["h1", "h2", "h3"]);
      assert.deepEqual(deliveries[0].descriptor, { version: 1, outputs: [] });
      const nativeNone = await verifyClaimDelivery({ assignment: assignment(1, { kind: "none" }), claim_handle: "h1", authorize });
      assert.equal(nativeNone.verification, "verified");
      assert.throws(() => scope.normalizeClaimScope({ type: "noop" }, batch), /multi-Claim/);
      const preview = await verifyClaimDelivery({
        assignment: batch,
        claim_handle: "h1",
        staged: true,
        authorize: request => {
          assert.equal(request.requireCompletion, false);
          return { authorized: true, claim_handle: request.claim_handle };
        },
      });
      assert.equal(preview.verification, "staged_preview");
      await assert.rejects(
        verifyClaimDelivery({
          assignment: batch,
          claim_handle: "h1",
          staged: true,
          messages: [{ type: "noop", claim_handle: "h2" }],
        }),
        /another Claim/
      );
    });

    it("independently verifies receipts and reauthorizes the actual delivered resource", async () => {
      const contract = { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1 }] };
      const base = {
        assignment: assignment(1, contract),
        claim_handle: "h1",
        messages: [{ type: "create_issue", repo: "owner/repo" }],
        results: [{ messageIndex: 0, success: true, claim_handle: "h1", result: { number: 42 } }],
        authorize: async request => {
          if (request.message.repo && request.message.repo !== "owner/repo") return { authorized: false, claim_handle: request.claim_handle };
          return authorized(request);
        },
      };
      const unknown = await verifyClaimDelivery(base);
      assert.equal(unknown.verification, "unknown");
      const proof = { verified: true, claim_handle: "h1", resource: { repository: "owner/repo", number: 42 }, evidence: { source: "independent_read" } };
      const delivered = await verifyClaimDelivery({ ...base, verifyOutput: async () => proof });
      assert.equal(delivered.verification, "verified");
      assert.equal(delivered.disposition, "complete");
      const foreign = await verifyClaimDelivery({ ...base, verifyOutput: async () => ({ ...proof, claim_handle: "h2" }) });
      assert.equal(foreign.verification, "unknown");
      await assert.rejects(verifyClaimDelivery({ ...base, verifyOutput: async () => ({ ...proof, resource: { repository: "foreign/repo", number: 42 } }) }), /same-Claim/);
      const mismatched = { ...base, results: [{ messageIndex: 0, success: true, claim_handle: "h2" }] };
      await assert.rejects(verifyClaimDelivery(mismatched), /Foreign Claim/);
    });

    it("requires the complete declared contract despite successful jobs and partial receipts without blocking siblings", async () => {
      const batch = assignment(3);
      batch.claims[0].work.effect_contract = { version: 1, outputs: [{ type: "create_issue", min: 2, max: 2 }] };
      const messages = [
        { type: "create_issue", claim_handle: "h1", repo: "owner/repo" },
        { type: "create_issue", claim_handle: "h1", repo: "owner/repo" },
      ];
      const options = {
        assignment: batch,
        claim_handle: "h1",
        authorize: authorized,
        messages,
        results: [
          { messageIndex: 0, claim_handle: "h1", success: true, result: { number: 41 } },
          { messageIndex: 1, claim_handle: "h1", success: true, result: { number: 42 } },
        ],
        verifyOutput: async ({ result }) => (result.number === 41 ? { verified: true, claim_handle: "h1", resource: { repository: "owner/repo", number: 41 }, evidence: { source: "independent_read" } } : { verified: false }),
      };
      const partial = await verifyClaimDelivery(options);
      assert.equal(partial.verification, "unknown");
      assert.equal(partial.disposition, "partial");
      assert.equal(partial.descriptor, null);
      const missing = await verifyClaimDelivery({ ...options, messages: messages.slice(0, 1), results: options.results.slice(0, 1) });
      assert.equal(missing.verification, "unknown");
      assert.match(missing.reason, /cardinality/);
      const outcomes = await Promise.allSettled([
        verifyClaimDelivery({
          ...options,
          verifyOutput: async () => {
            throw new Error("Independent native readback failed");
          },
        }),
        verifyClaimDelivery({ assignment: batch, claim_handle: "h2", authorize: authorized }),
        verifyClaimDelivery({ assignment: batch, claim_handle: "h3", authorize: authorized }),
      ]);
      assert.equal(outcomes[0].status, "rejected");
      for (const sibling of outcomes.slice(1)) {
        assert.equal(sibling.status, "fulfilled");
        if (sibling.status === "fulfilled") assert.equal(sibling.value.verification, "verified");
      }
    });

    it("rejects cross-Claim temporary references while permitting independent repeated IDs", () => {
      const partitions = new Map([
        ["h1", [{ type: "create_issue", temporary_id: "aw_one" }]],
        [
          "h2",
          [
            { type: "add_comment", body: "wrong #aw_one" },
            { type: "create_issue", temporary_id: "aw_shared" },
          ],
        ],
        [
          "h3",
          [
            { type: "create_issue", temporary_id: "aw_shared" },
            { type: "add_comment", body: "own #aw_shared" },
          ],
        ],
      ]);
      const rejected = [];
      manager.rejectCrossClaimTemporaryReferences(partitions, rejected);
      assert.equal(rejected.length, 1);
      assert.equal(rejected[0].claim_handle, "h2");
      const second = partitions.get("h2");
      const third = partitions.get("h3");
      assert.ok(second);
      assert.ok(third);
      assert.equal(second.length, 1);
      assert.equal(third.length, 2);
    });

    it("does not verify undeclared fields or partial resource state and requires separate code delivery evidence", async () => {
      const observed = {
        id: 7,
        number: 42,
        html_url: "https://github.com/owner/repo/issues/42",
        labels: [{ name: "wrong" }],
        assignees: [{ login: "bot" }],
        milestone: { number: 3 },
      };
      const options = {
        claim: assignment().claims[0],
        message: { type: "update_issue", repo: "owner/repo", item_number: 42, labels: ["expected"], assignees: ["bot"], milestone: 3 },
        result: { repo: "owner/repo", number: 42 },
        github: { rest: { issues: { get: async () => ({ data: observed }) } } },
      };
      assert.equal((await verifyBuiltinDeliveryOutput(options)).verified, false);
      observed.labels = [{ name: "expected" }];
      assert.equal((await verifyBuiltinDeliveryOutput(options)).verified, true);
      observed.assignees = [{ login: "foreign" }];
      assert.equal((await verifyBuiltinDeliveryOutput(options)).verified, false);
      observed.assignees = [{ login: "bot" }];
      assert.equal((await verifyBuiltinDeliveryOutput({ ...options, message: { ...options.message, unverified_effect: true } })).verified, false);
      assert.equal((await verifyBuiltinDeliveryOutput({ ...options, message: { type: "create_pull_request", repo: "owner/repo" } })).verified, false);
      observed.title = "wrong prefix desired title";
      assert.equal((await verifyBuiltinDeliveryOutput({ ...options, message: { type: "update_issue", repo: "owner/repo", item_number: 42, title: "desired title" } })).verified, false);
      observed.title = "desired title";
      observed.body = "desired body\n\ntrusted footer";
      const exact = {
        ...options,
        message: { type: "update_issue", repo: "owner/repo", item_number: 42, title: "desired title", body: "desired body" },
        effects: [{ claim_handle: options.claim.handle, outcome: "succeeded", kind: "issue", repository: "owner/repo", id: "7", expected: { title: "desired title", body: observed.body } }],
      };
      assert.equal((await verifyBuiltinDeliveryOutput(exact)).verified, true);
      observed.body += "\nwrong extra content";
      assert.equal((await verifyBuiltinDeliveryOutput(exact)).verified, false);
    });

    it("isolates real handler maps, temporary IDs and failed/deferred passes", async () => {
      const oldCore = global.core;
      const oldContext = global.context;
      global.core = { info() {}, warning() {}, debug() {}, error() {}, setOutput() {}, setFailed() {} };
      global.context = { repo: { owner: "owner", repo: "repo" }, payload: {} };
      try {
        /** @type {Map<string, (...args: any[]) => Promise<Record<string, unknown>>>} */
        const handlers = new Map();
        handlers.set("create_issue", async (message, ids, map) => {
          assert.equal(Object.keys(ids).length, 0);
          assert.equal(map.size, 0);
          assert.equal(message.claim_handle, scope.currentClaimHandle());
          return { success: true, repo: "owner/repo", number: message.claim_handle === "h1" ? 11 : 22, temporaryId: "aw_shared" };
        });
        handlers.set("create_pull_request", async () => ({ success: false, error: "intentional local failure" }));
        handlers.set("add_comment", async () => ({ success: true, comment_id: 1 }));
        const batch = assignment(2);
        const first = await scope.withClaimExecution({ assignment: batch, claim_handle: "h1", authorize: authorized }, () =>
          manager.processMessages(handlers, [
            { type: "create_issue", claim_handle: "h1", temporary_id: "aw_shared" },
            { type: "create_pull_request", claim_handle: "h1" },
          ])
        );
        const second = await scope.withClaimExecution({ assignment: batch, claim_handle: "h2", authorize: authorized }, () => manager.processMessages(handlers, [{ type: "create_issue", claim_handle: "h2", temporary_id: "aw_shared" }]));
        assert.equal(first.results.find(result => result.type === "create_pull_request").success, false);
        assert.equal(second.results[0].success, true);
        assert.equal(first.temporaryIdMap.aw_shared.number, 11);
        assert.equal(second.temporaryIdMap.aw_shared.number, 22);
        const singleton = assignment(1);
        singleton.claims[0].work.effect_contract = { kind: "none" };
        const noop = await scope.withClaimExecution({ assignment: singleton, claim_handle: "h1", authorize: authorized }, () => manager.processMessages(new Map(), [{ type: "noop" }]));
        assert.equal(noop.results[0].success, true);
        assert.equal(noop.results[0].delegated, undefined);
        const delivered = await verifyClaimDelivery({ assignment: singleton, claim_handle: "h1", authorize: authorized, messages: [{ type: "noop" }], results: noop.results });
        assert.equal(delivered.verification, "verified");
        let retries = 0;
        const deferred = await scope.withClaimExecution({ assignment: batch, claim_handle: "h1", authorize: authorized }, () =>
          manager.processMessages(
            new Map([
              [
                "deferred",
                async () => {
                  assert.equal(scope.currentClaimHandle(), "h1");
                  if (retries++ === 0) return { success: false, deferred: true };
                  throw new Error("only this Claim's retry fails");
                },
              ],
            ]),
            [{ type: "deferred", claim_handle: "h1" }]
          )
        );
        assert.equal(retries, 2);
        assert.equal(deferred.results[0].deferred, false);
        assert.equal(deferred.results[0].success, false);
        const { createPrReviewBufferRegistry } = require("./pr_review_buffer.cjs");
        const firstReviews = createPrReviewBufferRegistry();
        const secondReviews = createPrReviewBufferRegistry();
        firstReviews.getOrCreate("owner/repo", 42).addComment({ path: "file.go", line: 1, body: "first Claim only" });
        assert.equal(firstReviews.hasAnyContent(), true);
        assert.equal(secondReviews.getOrCreate("owner/repo", 42).getBufferedCount(), 0);
        const configured = await scope.withClaimExecution({ assignment: batch, claim_handle: "h1", authorize: authorized }, () => manager.loadHandlers({ missing_tool: { "target-repo": "foreign/repo" } }, createPrReviewBufferRegistry()));
        const reporter = configured.get("missing_tool");
        await assert.rejects(
          scope.withClaimExecution(
            {
              assignment: batch,
              claim_handle: "h1",
              authorize: async request => ({ authorized: request.message.repo !== "foreign/repo", claim_handle: request.claim_handle }),
            },
            () => reporter({ type: "missing_tool", claim_handle: "h1", reason: "blocked before effects" })
          ),
          /same-Claim/
        );
        await assert.rejects(
          scope.withClaimExecution({ assignment: batch, claim_handle: "h2", authorize: authorized }, () => reporter({ type: "missing_tool", claim_handle: "h2", reason: "cannot borrow factory" })),
          /factory context/
        );
        const invalid = await scope.withClaimExecution({ assignment: batch, claim_handle: "h1", authorize: authorized }, () =>
          manager.processMessages(handlers, [{ type: "missing_data", claim_handle: "h2", data_type: "foreign", reason: "must not become synthetic followup" }])
        );
        assert.equal(invalid.results[0].errorCode, "claim_scope_invalid");
        assert.equal(invalid.missings.missingData.length, 0);
        const ordinary = await manager.processMessages(new Map([["noop", async () => ({ success: true })]]), [{ type: "noop", message: "ordinary workflow" }]);
        assert.equal(ordinary.success, true);
      } finally {
        global.core = oldCore;
        global.context = oldContext;
      }
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
