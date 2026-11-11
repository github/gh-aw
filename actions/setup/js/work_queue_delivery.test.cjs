// @ts-check
import { describe, expect, it } from "vitest";
import checks from "./work_queue_claim_scope_checks.cjs";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { normalizeAssignment, withClaimExecution, withClaimResourceVerification } = require("./work_queue_claim_scope.cjs");
const { digest } = require("./work_queue_codec.cjs");
const { createClaimDeliveryVerifier, validateDeliveryContract, verifyClaimDelivery, verifyBuiltinDeliveryOutput, builtinDeliveryExpectation, durableBuiltinReceipt, verifyBuiltinExpectation } = require("./work_queue_delivery.cjs");

checks.registerTests({ describe, it });

describe("historical diagnostic delivery contracts", () => {
  it("uses standardized codes for invalid verifier configuration", () => {
    const original = assignment([{ type: "create_issue", min: 1, max: 1 }]);
    expect(() => createClaimDeliveryVerifier({ assignment: original })).toThrow(/^ERR_WORK_QUEUE_DELIVERY_READBACK_REQUIRED:/);
  });

  const assignment = outputs =>
    normalizeAssignment({
      version: 3,
      dispatch_id: "dispatch",
      request_id: "request",
      commit_id: "commit",
      policy_epoch: "policy",
      pool: "default",
      worker_profile: "default",
      claims: [{ handle: "h1", claim_id: "c1", work_id: "w1", work: { effect_contract: { version: 1, outputs } }, result_refs: [] }],
    });
  const deliver = (original, messages, options = {}) =>
    verifyClaimDelivery({
      assignment: original,
      claim_handle: "h1",
      authorize: async request => ({ authorized: true, claim_handle: request.claim_handle }),
      messages,
      results: messages.map((_, messageIndex) => ({ messageIndex, success: true, result: { number: 42 } })),
      ...options,
    });

  it("requires independent delivery readback with a stable error code", () => {
    expect(() => createClaimDeliveryVerifier({ assignment: assignment([]) })).toThrow("ERR_WORK_QUEUE_DELIVERY_READBACK_REQUIRED: Claim delivery verifier requires trusted independent readback");
  });

  it("identifies undeclared output types in delivery diagnostics", async () => {
    const original = assignment([{ type: "missing_tool", min: 1, max: 1 }]);
    expect((await deliver(original, [{ type: "report_incomplete" }])).diagnostics).toEqual({ event: "delivery.contract.undeclared_output", types: ["report_incomplete"] });
  });

  for (const legacy of ["missing_tool", "missing_data"]) {
    const canonical = `report_${legacy}`;

    it.each([{}, { output_id: "so_xbzi2dhGSY-3" }, { "depends-on": "so_priorOutput1" }, { output_id: "so_xbzi2dhGSY-3", "depends-on": "so_priorOutput1" }])(
      `matches ${legacy} contracts and messages canonically without rewriting immutable Work with metadata %j`,
      async metadata => {
        for (const contractType of [legacy, canonical]) {
          const original = assignment([
            { type: "create_issue", min: 1, max: 1 },
            { type: contractType, min: 1, max: 1 },
          ]);
          const before = JSON.stringify(original);
          const beforeDigest = digest(original);
          expect(validateDeliveryContract(original.claims[0].work.effect_contract)).toBe(original.claims[0].work.effect_contract);
          for (const messageType of [legacy, canonical]) {
            const messages = [Object.freeze({ type: "create_issue", repo: "owner/repo", ...metadata }), Object.freeze({ type: messageType })];
            const delivered = await deliver(original, messages, {
              verifyOutput: verifyBuiltinDeliveryOutput,
              github: {
                rest: {
                  repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) },
                  issues: { get: async () => ({ data: { id: 142, number: 42, html_url: "https://github.com/owner/repo/issues/42" } }) },
                },
              },
            });
            expect(delivered.verification).toBe("verified");
            expect(delivered.disposition).toBe("complete");
            expect(delivered.descriptor.outputs[1]).toEqual({ type: canonical, effect: "none" });
            expect(messages[1].type).toBe(messageType);
            expect(JSON.stringify(original)).toBe(before);
            expect(digest(original)).toBe(beforeDigest);
          }
        }
      }
    );

    it(`shares cardinality across ${legacy} and ${canonical} messages`, async () => {
      const messages = [{ type: legacy }, { type: canonical }];
      for (const type of [legacy, canonical]) {
        const exact = assignment([{ type, min: 2, max: 2 }]);
        expect((await deliver(exact, messages)).verification).toBe("verified");
        expect((await deliver(exact, [messages[0]])).reason).toMatch(/cardinality/);
        expect((await deliver(assignment([{ type, min: 0, max: 1 }]), messages)).reason).toMatch(/cardinality/);
      }
    });

    it(`rejects ambiguous duplicate ${legacy} contract definitions`, () => {
      for (const types of [
        [legacy, canonical],
        [canonical, legacy],
        [legacy, legacy],
        [canonical, canonical],
      ]) {
        expect(() => validateDeliveryContract({ version: 1, outputs: types.map(type => ({ type, min: 0, max: 1 })) })).toThrow(/Invalid bounded effect_contract/);
      }
    });

    it(`does not skip declared verification intent on ${legacy} contracts`, async () => {
      const original = assignment([{ type: legacy, min: 1, max: 1, verification: { verifier_id: "diagnostic", expected: {} } }]);
      for (const type of [legacy, canonical]) {
        expect((await deliver(original, [{ type }])).reason).toMatch(/verification intent requires a supported trusted verifier binding/);
      }
    });

    it(`still requires declared ${legacy} outputs and exact successful receipts`, async () => {
      const original = assignment([{ type: legacy, min: 1, max: 1 }]);
      expect((await deliver(original, [{ type: "report_incomplete" }])).reason).toMatch(/Undeclared output/);
      for (const outcome of [{ success: false }, { success: true, delegated: true }, { success: true, skipped: true }]) {
        const delivery = await deliver(original, [{ type: canonical }], { results: [{ messageIndex: 0, ...outcome }] });
        expect(delivery.reason).toMatch(/Missing exact nondelegated delivery receipt/);
        expect(delivery.diagnostics.output_type).toBe(canonical);
      }
    });
  }
});

it("registers dispatch identity and manifest checks as independent siblings before execution", async () => {
  let registering = true;
  /** @type {Map<string, () => unknown>} */
  const callbacks = new Map();
  checks.registerTests({
    describe(_name, register) {
      expect(registering, "suite registration cannot occur inside a running check").toBe(true);
      register();
    },
    it(name, run) {
      expect(registering, "test registration cannot occur inside a running check").toBe(true);
      expect(callbacks.has(name), "each check must be registered exactly once").toBe(false);
      callbacks.set(name, run);
    },
  });
  registering = false;
  for (const name of ["does not reuse local handles, factory contexts or receipts across immutable dispatch identities", "pins manifest writers to the full original Claim and persists immutable attribution"]) {
    const run = callbacks.get(name);
    expect(run, "both checks must exist before any test callback runs").toBeTypeOf("function");
    if (!run) throw new Error(`Missing sibling check: ${name}`);
    await run();
  }
});

describe("serializable builtin delivery expectations", () => {
  const fixture = (type = "create_issue", fields = {}) => {
    const assignment = normalizeAssignment({
      version: 3,
      dispatch_id: "dispatch",
      request_id: "request",
      commit_id: "commit",
      policy_epoch: "policy",
      pool: "default",
      worker_profile: "default",
      claims: [{ handle: "h1", claim_id: "c1", work_id: "w1", work: { effect_contract: { version: 1, outputs: [{ type, min: 1, max: 1 }] } }, result_refs: [] }],
    });
    /** @type {Record<string, any>} */
    const observed = {
      id: 142,
      number: 42,
      title: "Rendered title",
      body: "Rendered body",
      state: "open",
      state_reason: null,
      labels: [{ name: "Approved" }],
      assignees: [{ login: "Bot" }],
      milestone: { number: 3 },
      merged: false,
      draft: false,
      html_url: "https://github.com/owner/repo/issues/42",
    };
    /** @type {Record<string, any>} */
    const input = {
      claim: assignment.claims[0],
      message: { type, claim_handle: "h1", repo: "owner/repo", ...fields },
      result: { number: 42, repo: "owner/repo" },
      effects: [],
      authorize: async request => ({ authorized: true, claim_handle: request.claim_handle }),
      github: {
        rest: {
          repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) },
          issues: {
            get: async () => ({ data: { ...observed } }),
            getComment: async () => ({ data: { ...observed, issue_url: "https://api.github.com/repos/owner/repo/issues/42" } }),
          },
          pulls: { get: async () => ({ data: { ...observed } }) },
        },
      },
    };
    const execute = callback => withClaimExecution({ assignment, claim_handle: "h1", authorize: input.authorize, effects: input.effects }, callback);
    const durableInput = receipt => ({ ...input, durable: true, message: { type, claim_handle: "h1" }, effects: [], result: { entry: { verifier: "builtin", receipt: JSON.parse(JSON.stringify(receipt)) } } });
    return { input, observed, execute, durableInput };
  };

  it.each([false, true])("preserves rendered effect content and pins native identity with result.id present=%s", async hasId => {
    const f = fixture("create_issue", { title: "Agent title", body: "Agent body" });
    const body = "Agent body\n\n> AI generated by a trusted workflow";
    f.observed.body = body;
    if (hasId) f.input.result.id = 142;
    f.input.effects.push(
      { claim_handle: "h2", kind: "issue", repository: "owner/repo", outcome: "succeeded", id: "142", number: 42, expected: { title: "sibling" } },
      { claim_handle: "h1", kind: "issue", repository: "owner/repo", outcome: "succeeded", id: "999", number: 43, expected: { title: "other issue" } },
      { claim_handle: "h1", kind: "issue", repository: "owner/repo", outcome: "succeeded", id: "142", number: 42, expected: { title: f.observed.title, body } }
    );
    const { github, ...withoutClient } = f.input;
    const receipt = durableBuiltinReceipt(withoutClient);
    expect(receipt).toEqual({
      type: "create_issue",
      kind: "issue",
      repository: "owner/repo",
      number: 42,
      id: "142",
      expected: { title_digest: digest(f.observed.title), body_digest: digest(body), exact_labels: false, exact_assignees: false },
    });
    expect(builtinDeliveryExpectation(withoutClient)).toEqual(receipt);
    expect(JSON.stringify(receipt)).not.toContain(body);
    expect((await verifyBuiltinExpectation(receipt, withoutClient)).verified).toBe(false);
    await f.execute(async () => {
      const proof = await verifyBuiltinDeliveryOutput(f.input);
      expect(proof.verified).toBe(true);
      await withClaimResourceVerification(proof, () => undefined, { github });
      await expect(withClaimResourceVerification(structuredClone(proof), () => undefined, { github })).rejects.toThrow(/private in-process authority receipt/);
    });
    // A later execution has only serialized expectations, not the agent message
    // or the in-memory effect objects that supplied the rendered content.
    await f.execute(async () => {
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(true);
      for (const [field, changed] of [
        ["title", "Agent title"],
        ["body", "Agent body"],
        ["id", 999],
      ]) {
        const prior = f.observed[field];
        f.observed[field] = changed;
        expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(false);
        expect((await verifyBuiltinDeliveryOutput(f.input)).verified).toBe(false);
        f.observed[field] = prior;
      }
    });
  });

  it("matches comment effects by native ID when the write has no parent number", async () => {
    const f = fixture("add_comment", { item_number: 42, body: "Agent body" });
    f.input.result.comment_id = 142;
    f.input.effects.push(
      { claim_handle: "h1", kind: "comment", repository: "owner/repo", outcome: "succeeded", id: "999", number: 42, expected: { body: "another comment" } },
      { claim_handle: "h1", kind: "comment", repository: "owner/repo", outcome: "succeeded", id: "142", number: null, expected: { body: f.observed.body } }
    );
    const receipt = durableBuiltinReceipt(f.input);
    expect(receipt.expected.body_digest).toBe(digest(f.observed.body));
    expect(receipt.comment_id).toBe(142);
    await f.execute(async () => {
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(true);
      f.observed.body = "another comment";
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(false);
    });
  });

  it.each(["message", "effect", "declared", "update"])("retains label and assignee containment versus exactness for %s expectations", async source => {
    const f = fixture(source === "update" ? "update_issue" : "create_issue", { labels: ["APPROVED"], assignees: ["BOT"] });
    if (source === "effect") f.input.effects.push({ claim_handle: "h1", kind: "issue", repository: "owner/repo", outcome: "succeeded", number: 42, expected: { labels: ["Approved"], assignees: ["Bot"] } });
    if (source === "declared") f.input.verification = { verifier_id: "create_issue", expected: { labels: ["APPROVED"], assignees: ["BOT"] } };
    const receipt = durableBuiltinReceipt(f.input);
    expect(receipt.expected.exact_labels).toBe(source !== "message");
    expect(receipt.expected.exact_assignees).toBe(source !== "message");
    await f.execute(async () => {
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(true);
      f.observed.labels.push({ name: "Extra" });
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(source === "message");
      f.observed.labels.pop();
      f.observed.assignees.push({ login: "Extra" });
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(source === "message");
      f.observed.assignees = [];
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(false);
    });
  });

  it.each([
    [{ state: "open" }, { state: "closed" }],
    [{ status: "open" }, { state: "closed" }],
    [{ state_reason: "completed" }, { state_reason: "not_planned" }],
    [{ milestone: 3 }, { milestone: 4 }],
    [{ milestone: null }, { milestone: 3 }],
    [{ milestone: 3 }, { milestone: null }],
  ])("fails closed for contradictory requested %j and recorded %j expectations", async (message, expected) => {
    const f = fixture("update_issue", message);
    f.input.effects.push({ claim_handle: "h1", kind: "issue", repository: "owner/repo", outcome: "succeeded", number: 42, expected });
    expect(builtinDeliveryExpectation(f.input)).toBeNull();
    expect(durableBuiltinReceipt(f.input)).toBeNull();
    expect((await f.execute(() => verifyBuiltinDeliveryOutput(f.input))).verified).toBe(false);
  });

  it.each([
    ["close_issue", { state_reason: "completed" }, { state: "closed", state_reason: "completed" }, { state: "open" }],
    ["close_pull_request", {}, { state: "closed" }, { state: "open" }],
    ["merge_pull_request", {}, { merged: true }, { merged: false }],
    ["mark_pull_request_as_ready_for_review", {}, { draft: false }, { draft: true }],
    ["add_labels", { labels: ["APPROVED"] }, {}, { labels: [] }],
    ["remove_labels", { labels: ["REMOVED"] }, {}, { labels: [{ name: "Removed" }] }],
    ["replace_label", { label_to_add: " approved ", label_to_remove: " removed " }, {}, { labels: [{ name: "Approved" }, { name: "Removed" }] }],
    ["update_issue", { milestone: "3", state: "open" }, {}, { milestone: null }],
    ["update_issue", { milestone: null }, { milestone: null }, { milestone: { number: 3 } }],
  ])("independently rechecks durable %s state and label changes", async (type, message, observed, changed) => {
    const f = fixture(type, message);
    Object.assign(f.observed, observed);
    const receipt = durableBuiltinReceipt(f.input);
    expect(receipt).not.toBeNull();
    await f.execute(async () => {
      expect((await verifyBuiltinDeliveryOutput(f.input)).verified).toBe(true);
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(true);
      Object.assign(f.observed, changed);
      expect((await verifyBuiltinDeliveryOutput(f.input)).verified).toBe(false);
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(false);
    });
  });

  it("retains null content and state reasons without treating missing readback fields as equal", async () => {
    const f = fixture("update_issue", { body: null, milestone: "3" });
    f.observed.body = null;
    f.input.effects.push({ claim_handle: "h1", kind: "issue", repository: "owner/repo", outcome: "succeeded", number: 42, expected: { body: null, state_reason: null, milestone: 3 } });
    const receipt = durableBuiltinReceipt(f.input);
    expect(receipt.expected).toMatchObject({ body_digest: digest(null), state_reason: null, milestone: 3 });
    await f.execute(async () => {
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(true);
      delete f.observed.body;
      expect((await verifyBuiltinDeliveryOutput(f.durableInput(receipt))).verified).toBe(false);
    });
  });

  it("rejects incomplete or mistyped serialized expectations before native reads", async () => {
    const f = fixture("create_issue", { body: "Rendered body" });
    const receipt = durableBuiltinReceipt(f.input);
    const malformed = [
      null,
      {},
      { ...receipt, expected: {} },
      { ...receipt, number: "42" },
      { ...receipt, kind: "comment" },
      { ...receipt, expected: { ...receipt.expected, body_digest: "Rendered body" } },
      { ...receipt, expected: { ...receipt.expected, labels: null } },
    ];
    f.input.github.rest.issues.get = async () => {
      throw new Error("must not read malformed expectations");
    };
    await f.execute(async () => {
      for (const invalid of malformed) expect((await verifyBuiltinDeliveryOutput(f.durableInput(invalid))).verified).toBe(false);
      const wrongVerifier = f.durableInput(receipt);
      wrongVerifier.result.entry.verifier = "asset";
      expect((await verifyBuiltinDeliveryOutput(wrongVerifier)).verified).toBe(false);
    });
  });

  it("does not collect staged, unsupported, or undeclared builtin effects", () => {
    const f = fixture();
    for (const change of [
      { result: { number: 42, staged: true } },
      { result: null },
      { message: { ...f.input.message, type: "update_pull_request" } },
      { message: { ...f.input.message, undeclared: true } },
      { message: { ...f.input.message, item_number: 43 } },
    ]) {
      expect(durableBuiltinReceipt({ ...f.input, ...change })).toBeNull();
    }
  });
});
