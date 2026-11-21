// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { withSafeOutputInvocation } = require("./safe_output_execution.cjs");
const { createClaimAdapterHandler, preparedAdapterPath } = require("./work_queue_claim_adapters.cjs");

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("prepared Claim adapter safe-output authorization", () => {
  it.each(["GH_AW_SAFE_OUTPUTS_STAGED", "GH_AW_CLAIM_ADAPTER_STAGED"])("skips preparation before loading execution Claims when %s is true", async variable => {
    vi.stubEnv(variable, "true");
    const core = { setOutput: vi.fn(), info: vi.fn() };
    const { main } = require("./work_queue_prepare_claim_adapter.cjs");
    await expect(main({ core })).resolves.toEqual({ active: false, staged: true });
    expect(core.setOutput).toHaveBeenCalledWith("active", "false");
  });

  it.each([
    ["unavailable authority", true, "missing_credentials"],
    ["missing trusted executor", false, "trusted prepared invocation context"],
  ])("does not initialize the live effect implementation with %s", async (_reason, trustedExecutor, expectedError) => {
    const root = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-claim-authorization-"));
    vi.stubEnv("RUNNER_TEMP", root);
    vi.stubEnv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", undefined);
    vi.stubEnv("ACTIONS_ID_TOKEN_REQUEST_URL", undefined);
    vi.stubGlobal("core", { info: vi.fn(), warning: vi.fn(), setSecret: vi.fn() });
    const fixture = { assignment: { dispatch_id: "d1", claims: [{ handle: "h1", claim_id: "c1", work_id: "w1", work: {}, result_refs: [] }] } };
    const message = { type: "custom", claim_handle: "h1", number: 42, content: "prepared body" };
    const adapter = { mode: "prepared", "effect-type": "update_issue", "target-repo": "owner/repo", "field-map": { body: "content", item_number: "number" } };
    const loadEffectHandler = vi.fn();
    const authorizeClaim = async request => ({ claim_handle: request.claim_handle, authorized: true });
    const state = {
      authorize: trustedExecutor ? options => require("./safe_output_authorization_gate.cjs").authorizeHandlerInvocation(options) : undefined,
      requirement: { endpoint: "https://authority.example.org/authorize", audience: "gh-aw" },
      batchDigest: "b".repeat(64),
      recordIndex: 0,
      invocation: {
        caller: {
          host: "https://github.com",
          repository_id: "123",
          workflow_ref: "owner/repo/.github/workflows/test.lock.yml@refs/heads/main",
          workflow_sha: "a".repeat(40),
          workflow_hash: "b".repeat(64),
          run_id: "456",
          run_attempt: "1",
        },
        output: { type: "custom", name: "custom", executor: "queue", implementation_digest: "a".repeat(64) },
        context: {},
      },
    };
    try {
      await withClaimExecution({ assignment: fixture.assignment, claim_handle: "h1", authorize: authorizeClaim }, async () => {
        const filename = preparedAdapterPath(root, "h1", "custom");
        fs.mkdirSync(path.dirname(filename), { recursive: true });
        fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: "h1", type: "custom", messages: [{ input: message, payload: { number: 42, content: "prepared body" } }] }));
        const handler = await createClaimAdapterHandler({ adapter, filename, loadEffectHandler });
        await expect(withSafeOutputInvocation(state, () => handler(message))).rejects.toThrow(expectedError);
      });
      expect(loadEffectHandler).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
