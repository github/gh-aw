import { describe, expect, it } from "vitest";
import checks from "./work_queue_claim_adapter_checks.cjs";

checks.registerTests({ describe, it });

import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const fs = require("node:fs");
const path = require("node:path");
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { queueFixture } = require("./work_queue_lifecycle.test_helpers.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { temporaryDirectory } = require("./work_queue_effect_test_helpers.cjs");
const { createClaimAdapterHandler, preparedAdapterPath, wrapDeclaredBuiltinHandler, durableClaimAdapterEntry, durableDeclaredBuiltinReceipt, createDurableDeclaredVerifier } = require("./work_queue_claim_adapters.cjs");

describe("durable declared Claim adapters", () => {
  it.each(["prepared", "native"])("rechecks %s builtin receipts without message content and rejects mutated fields", async transport => {
    const type = transport === "native" ? "update_issue" : "custom";
    const verification = { verifier_id: type, expected: { item_number: 42, body: "expected body" } };
    const fixture = queueFixture({
      count: 1,
      configureWork: work => {
        work.payload.effect_contract = { version: 1, outputs: [{ type, min: 1, max: 1, verification }] };
      },
    });
    const assignment = fixture.assignment;
    const claim = assignment.claims[0];
    const adapter = { mode: "prepared", "effect-type": "update_issue", "target-repo": "owner/repo", "field-map": { body: "content", item_number: "number" } };
    let body = "expected body";
    const source = {
      rest: {
        repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) },
        issues: {
          update: async () => ({ data: { id: 9, number: 42 } }),
          get: async () => ({ data: { id: 9, number: 42, body, html_url: "https://github.com/owner/repo/issues/42" } }),
        },
      },
    };
    const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true });
    const root = temporaryDirectory("durable-adapter");
    try {
      await withClaimExecution({ assignment, claim_handle: "h1", authorize, effects: [] }, async () => {
        const effects = [];
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
        const execute = async message => {
          const response = await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: message.item_number, body: message.body });
          return { success: true, repo: "owner/repo", number: response.data.number };
        };
        let message;
        let handler;
        if (transport === "native") {
          message = { type, claim_handle: "h1", ...verification.expected };
          handler = wrapDeclaredBuiltinHandler(type, execute);
        } else {
          message = { type, claim_handle: "h1", number: 42, content: "expected body" };
          const filename = preparedAdapterPath(root, "h1", type);
          fs.mkdirSync(path.dirname(filename), { recursive: true });
          fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: "h1", type, messages: [{ input: message, payload: { number: 42, content: "expected body" } }] }));
          handler = await createClaimAdapterHandler({ adapter, filename, github: client, loadEffectHandler: async () => execute });
        }
        const result = await handler(message);
        const input = { claim, message, result, adapter, effects, verification, github: source };
        const entry = transport === "native" ? { type, verifier: "declared_builtin", receipt: durableDeclaredBuiltinReceipt(input) } : { type, ...durableClaimAdapterEntry(input) };
        expect(entry.receipt).not.toBeNull();
        expect(JSON.stringify(entry.receipt)).not.toContain("expected body");
        const restored = JSON.parse(JSON.stringify(entry));
        const verify = createDurableDeclaredVerifier([restored], effects);
        const durable = { claim, message: { type, claim_handle: "h1" }, durable: true, result: { entry: restored }, verification, github: source };
        expect(await verify(durable)).toMatchObject({ verified: true, resource: { number: 42 } });
        body = "changed body";
        expect(await verify(durable)).toMatchObject({ verified: false });
        body = "expected body";
        expect(await verify({ ...durable, verification: { ...verification, expected: { ...verification.expected, body: "different intent" } } })).toMatchObject({ verified: false });
      });
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
