// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import crypto from "crypto";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { digestArtifactFiles, executorRecords, main } from "./safe_output_authorization_gate.cjs";
import { VERSION } from "./safe_output_authorization.cjs";
import { projectAuthorization } from "./safe_output_execution.cjs";
const { authorizeHandlerInvocation, handlerAuthorizationRequired, requireAuthorization } = createRequire(import.meta.url)("./safe_output_authorization_gate.cjs");
const { withSafeOutputInvocation } = createRequire(import.meta.url)("./safe_output_execution.cjs");

describe("prepared authorization artifact bindings", () => {
  let directory;

  beforeEach(() => {
    vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", "");
    directory = fs.mkdtempSync(path.resolve(".gh-aw-authorization-gate-"));
  });

  describe("external executor batch authorization", () => {
    const descriptor = { type: "custom_action", executor: "action", implementation: "a".repeat(64) };
    const core = { info: vi.fn(), setOutput: vi.fn() };

    beforeEach(() => {
      vi.clearAllMocks();
    });

    describe("external gate shared-protocol integration", () => {
      let directory;
      const audience = "https://authority.example.org/gh-aw";
      const descriptor = {
        type: "custom_action",
        instance: "custom_action",
        executor: "action",
        implementation: "owner/action@" + "a".repeat(40),
        authorization: { endpoint: "https://authority.example.org/authorize", audience, source: "local" },
      };
      let env;
      let core;

      beforeEach(() => {
        directory = fs.mkdtempSync(path.resolve(".gh-aw-authorization-live-gate-"));
        env = {
          RUNNER_TEMP: directory,
          GITHUB_REPOSITORY_ID: "123",
          GITHUB_WORKFLOW_REF: "owner/repo/.github/workflows/test.lock.yml@refs/heads/main",
          GITHUB_WORKFLOW_SHA: "a".repeat(40),
          GH_AW_AUTHORIZATION_WORKFLOW_HASH: "b".repeat(64),
          GITHUB_RUN_ID: "456",
          GITHUB_RUN_ATTEMPT: "1",
          ACTIONS_ID_TOKEN_REQUEST_URL: "https://oidc.example.org/token",
          ACTIONS_ID_TOKEN_REQUEST_TOKEN: "bootstrap-secret",
        };
        core = { setSecret: vi.fn(), setOutput: vi.fn(), info: vi.fn() };
      });

      afterEach(() => {
        fs.rmSync(directory, { recursive: true, force: true });
      });

      function authority(decision) {
        return vi.fn(async (_url, options) => {
          if (options.method === "GET") {
            const payload = Buffer.from(JSON.stringify({ aud: audience, exp: Math.floor(Date.now() / 1000) + 60 })).toString("base64url");
            return Response.json({ value: `header.${payload}.signature` });
          }
          const request = JSON.parse(options.body);
          return Response.json({
            version: VERSION,
            decision,
            request_digest: request.request_digest,
            invocation_id: request.invocation.invocation_id,
            run_attempt: "1",
            policy_revision: "policy-v1",
            decision_id: "decision-v1",
            issued_at: new Date().toISOString(),
            expires_at: new Date(Date.now() + 20000).toISOString(),
          });
        });
      }

      it("persists one matching decision per logical record before releasing the batch", async () => {
        const fetchImpl = authority("allow");
        const result = await main({ core, env, descriptor, batch: [{ type: "custom_action", secret_body: "must-not-transmit" }], fetchImpl });
        expect(result.state).toBe("allowed");
        expect(core.setOutput).toHaveBeenCalledWith("authorized", "true");
        const evidence = fs.readFileSync(path.join(directory, "gh-aw", "safe-output-authorization.jsonl"), "utf8");
        expect(evidence).toContain('"state":"allowed"');
        expect(JSON.stringify(fetchImpl.mock.calls)).not.toContain("must-not-transmit");
        expect(evidence).not.toContain("must-not-transmit");
      });

      it("fails closed without RUNNER_TEMP before contacting OIDC or the authority", async () => {
        const fetchImpl = authority("allow");
        delete env.RUNNER_TEMP;
        await expect(main({ core, env, descriptor, batch: [{ type: "custom_action" }], fetchImpl })).rejects.toThrow("trusted evidence storage");
        expect(fetchImpl).not.toHaveBeenCalled();
        expect(core.setOutput).not.toHaveBeenCalledWith("authorized", "true");
      });

      it("authorizes fractional reusable-workflow payloads without relaxing protocol response parsing", async () => {
        const fetchImpl = authority("allow");
        const result = await main({ core, env, descriptor, batch: [{ type: "custom_action" }], payload: '{"threshold":0.5,"nested":[1.25]}', fetchImpl });
        expect(result.state).toBe("allowed");
        expect(fetchImpl).toHaveBeenCalledTimes(2);
      });

      it("does not enter the external executor after a denied decision", async () => {
        const execute = vi.fn();
        await expect(
          (async () => {
            await main({ core, env, descriptor, batch: [{ type: "custom_action" }], fetchImpl: authority("deny") });
            execute();
          })()
        ).rejects.toThrow("denied");
        expect(execute).not.toHaveBeenCalled();
        expect(core.setOutput).not.toHaveBeenCalledWith("authorized", "true");
      });

      it("binds a preparation phase only to its trusted Claim without changing source ordinals", async () => {
        const fetchImpl = authority("allow");
        const claimIdentity = { claim_handle: "h2", claim_id: "claim-2", dispatch_id: "dispatch-1" };
        await main({
          core,
          env: { ...env, GH_AW_AUTHORIZATION_CLAIM_HANDLE: "h2" },
          descriptor: { ...descriptor, instance: "custom_action:prepare_claim_adapter" },
          batch: [
            { type: "custom_action", claim_handle: "h1" },
            { type: "custom_action", claim_handle: "h2" },
          ],
          claimIdentity,
          fetchImpl,
        });
        const requests = fetchImpl.mock.calls.filter(([, options]) => options.method === "POST");
        expect(requests).toHaveLength(1);
        const invocation = JSON.parse(requests[0][1].body).invocation;
        expect(invocation.batch.record_index).toBe(1);
        expect(invocation.output.name).toBe("custom_action:prepare_claim_adapter");
        expect(invocation.context).toMatchObject({ claim_id: "claim-2", delivery_id: "dispatch-1" });
      });

      it("rejects missing and foreign preparation identities before contacting the authority", async () => {
        const fetchImpl = authority("allow");
        const preparation = { ...descriptor, instance: "custom_action:prepare_claim_adapter" };
        const batch = [{ type: "custom_action", claim_handle: "h2" }];
        await expect(main({ core, env, descriptor: preparation, batch, fetchImpl })).rejects.toThrow("trusted Claim identity");
        await expect(
          main({
            core,
            env: { ...env, GH_AW_AUTHORIZATION_CLAIM_HANDLE: "h2" },
            descriptor: preparation,
            batch,
            fetchImpl,
            claimIdentity: { claim_handle: "h1", claim_id: "claim-1", dispatch_id: "dispatch-1" },
          })
        ).rejects.toThrow("foreign Claim identity");
        expect(fetchImpl).not.toHaveBeenCalled();
      });

      it("authorizes only the selected reusable-workflow source record after scheduling", async () => {
        const fetchImpl = authority("allow");
        const launch = { ...descriptor, type: "call_workflow", instance: "call_workflow:launch:worker", executor: "workflow" };
        const batch = [
          { type: "call_workflow", workflow_name: "worker" },
          { type: "call_workflow", workflow_name: "worker" },
        ];
        await expect(main({ core, env, descriptor: launch, batch, fetchImpl })).rejects.toThrow("trusted source invocation");
        expect(fetchImpl).not.toHaveBeenCalled();
        await main({
          core,
          env: { ...env, GH_AW_AUTHORIZATION_SOURCE_RECORD_INDEX: "1" },
          descriptor: launch,
          batch,
          fetchImpl,
          payload: { workflow: "worker", inputs: { task: "selected" } },
        });
        const requests = fetchImpl.mock.calls.filter(([, options]) => options.method === "POST");
        expect(requests).toHaveLength(1);
        const invocation = JSON.parse(requests[0][1].body).invocation;
        expect(invocation.batch.record_index).toBe(1);
        expect(invocation.output.name).toBe("call_workflow:launch:worker");
      });
    });

    it("does not acquire credentials or inspect live payloads for a staged preview", async () => {
      const authorize = vi.fn(() => {
        throw new Error("must not acquire credentials");
      });
      const result = await main({ core, descriptor: { ...descriptor, staged: true }, authorize });
      expect(result.state).toBe("skipped_staged");
      expect(authorize).not.toHaveBeenCalled();
      expect(core.setOutput).not.toHaveBeenCalledWith("authorized", "true");
    });

    it("keeps explicitly duplicated records distinct and preserves original batch ordinals", () => {
      const batch = [{ type: "noop" }, { type: "custom_action", text: "same" }, { type: "custom_action", text: "same" }];
      expect(executorRecords(batch, descriptor.type).map(record => record.index)).toEqual([1, 2]);
    });

    it("does not release an entire batch when one required decision is denied", async () => {
      const authorize = vi
        .fn()
        .mockResolvedValueOnce({ state: "allowed", expires_at: new Date(Date.now() + 20000).toISOString() })
        .mockRejectedValueOnce(new Error("authorization denied"));
      await expect(
        main({
          core,
          descriptor,
          batch: [
            { type: "custom_action", value: 1 },
            { type: "custom_action", value: 2 },
          ],
          authorize,
        })
      ).rejects.toThrow("denied");
      expect(core.setOutput).not.toHaveBeenCalledWith("authorized", "true");
    });

    it("checks every decision again after collecting the batch", async () => {
      const authorize = vi.fn().mockResolvedValue({ state: "allowed", expires_at: new Date(Date.now() + 1000).toISOString() });
      await expect(main({ core, descriptor, batch: [{ type: "custom_action" }], authorize })).rejects.toThrow("fresh decision");
      expect(core.setOutput).not.toHaveBeenCalledWith("authorized", "true");
    });

    it("does not let one exported action payload stand in for multiple requested records", async () => {
      const authorize = vi.fn();
      await expect(main({ core, descriptor, batch: [{ type: "custom_action" }, { type: "custom_action" }], payload: "{}", authorize })).rejects.toThrow("exactly one");
      expect(authorize).not.toHaveBeenCalled();
    });

    it.each(['{"threshold":0.5,"threshold":0.25}', '{"threshold":1e999}', '{},"other":true'])("retains strict payload validation before authorization", async payload => {
      const authorize = vi.fn();
      await expect(main({ core, descriptor, batch: [{ type: "custom_action" }], payload, authorize })).rejects.toThrow();
      expect(authorize).not.toHaveBeenCalled();
    });
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it("requires prepared context for protected direct executors, including managed authority", async () => {
    await expect(requireAuthorization({ inputs: {} })).rejects.toThrow("trusted prepared invocation context");
    expect(() => handlerAuthorizationRequired({ authorization: { from_environment: true, source: "global" } })).toThrow("trusted prepared invocation context");
    await expect(authorizeHandlerInvocation({ config: { authorization: { from_environment: true, source: "global" } }, inputs: {} })).rejects.toThrow("trusted prepared invocation context");
    await withSafeOutputInvocation({}, async () => {
      await expect(requireAuthorization({ inputs: {} })).rejects.toThrow("trusted prepared invocation context");
    });
    await expect(authorizeHandlerInvocation({ inputs: {} })).resolves.toEqual({ state: "not_configured" });
  });

  it("binds bytes and relative artifact identity instead of only a mutable path", () => {
    fs.writeFileSync(path.join(directory, "report.json"), "first");
    const first = digestArtifactFiles(directory, ["report.json"]);
    expect(first).toEqual([{ path: "report.json", digest: crypto.createHash("sha256").update("first").digest("hex"), size: 5 }]);
    fs.writeFileSync(path.join(directory, "report.json"), "second");
    expect(digestArtifactFiles(directory, ["report.json"])[0].digest).not.toBe(first[0].digest);
  });

  it("rejects escaping files and symlink substitutions", () => {
    fs.mkdirSync(path.join(directory, "staging"));
    fs.writeFileSync(path.join(directory, "private"), "not an artifact");
    fs.symlinkSync(path.join(directory, "private"), path.join(directory, "staging", "link"));
    expect(() => digestArtifactFiles(path.join(directory, "staging"), ["../private"])).toThrow("escapes");
    expect(() => digestArtifactFiles(path.join(directory, "staging"), ["link"])).toThrow("escapes");
  });

  it.skipIf(process.platform === "win32")("rejects a FIFO without blocking artifact preparation", () => {
    expect(spawnSync("mkfifo", [path.join(directory, "pipe")]).status).toBe(0);
    const result = spawnSync(
      process.execPath,
      [
        "-e",
        "try { require(process.argv[1]).digestArtifactFiles(process.argv[2], ['pipe']); process.exitCode = 2; } catch (error) { if (!error.message.includes('regular file')) throw error; }",
        createRequire(import.meta.url).resolve("./safe_output_authorization_gate.cjs"),
        directory,
      ],
      { timeout: 2000, encoding: "utf8" }
    );
    expect(result.error).toBeUndefined();
    expect(result.status).toBe(0);
  });

  it("projects bounded authorization identity and freshness without private authority details", () => {
    const metadata = {
      state: "allowed",
      output_type: "custom",
      output_name: "custom:prepare_claim_adapter",
      executor: "queue",
      binding: "invocation",
      requirement_source: "global",
      run_attempt: "2",
      issued_at: "2026-01-01T00:00:00.000Z",
      expires_at: "2026-01-01T00:00:20.000Z",
    };
    expect(projectAuthorization({ ...metadata, token: "secret", endpoint: "https://private.example.org", body: "private" })).toEqual(metadata);
    expect(projectAuthorization({ state: "allowed", binding: "exact-effect", executor: "untrusted", issued_at: "invalid" })).toEqual({ state: "allowed" });
  });
});
