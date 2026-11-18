import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const protocol = require("./safe_output_authorization.cjs");
const nativeModule = require("./create_issue.cjs");
const { loadHandlers, processMessages } = require("./safe_output_handler_manager.cjs");
const { currentSafeOutputInvocation, buildSafeOutputExecutions } = require("./safe_output_execution.cjs");
const root = path.resolve(`.manager-authorization-${process.pid}`);
const scriptPath = path.join(root, "gh-aw/actions/protected.cjs");
const requirement = { endpoint: "https://authority.example/authorize", audience: "safe-outputs", source: "local" };

beforeEach(() => {
  vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() });
  vi.stubGlobal("github", { rest: {} });
  vi.stubGlobal("context", { repo: { owner: "org", repo: "repo" }, payload: {} });
  for (const [key, value] of Object.entries({
    RUNNER_TEMP: root,
    GITHUB_REPOSITORY_ID: "123",
    GITHUB_WORKFLOW_REF: "org/repo/.github/workflows/test.yml@refs/heads/main",
    GITHUB_WORKFLOW_SHA: "a".repeat(40),
    GH_AW_AUTHORIZATION_WORKFLOW_HASH: "b".repeat(64),
    GITHUB_RUN_ID: "456",
    GITHUB_RUN_ATTEMPT: "1",
    GH_AW_SAFE_OUTPUTS_STAGED: "false",
  }))
    vi.stubEnv(key, value);
  vi.spyOn(protocol, "recordAuthorizationOutcome").mockReturnValue({ evidenceRecorded: true });
  vi.spyOn(protocol, "assertAuthorizationFresh").mockImplementation(({ authorization }) => authorization);
  fs.mkdirSync(path.dirname(scriptPath), { recursive: true });
  fs.writeFileSync(
    scriptPath,
    "global.__authorizationScriptLoads++; exports.main = async () => { global.__authorizationScriptFactories++; return async message => { global.__authorizationScriptCalls++; return {success:true, observed:message}; }; };"
  );
  global.__authorizationScriptLoads = 0;
  global.__authorizationScriptFactories = 0;
  global.__authorizationScriptCalls = 0;
});

afterEach(() => {
  delete require.cache[scriptPath];
  fs.rmSync(root, { recursive: true, force: true });
  delete global.__authorizationScriptLoads;
  delete global.__authorizationScriptFactories;
  delete global.__authorizationScriptCalls;
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

function allow() {
  return vi.spyOn(protocol, "authorizePreparedInvocation").mockImplementation(async ({ invocation }) => ({
    state: "allowed",
    invocation_id: invocation.invocation_id,
    decision_id: "decision-1",
    expires_at: new Date(Date.now() + 30000).toISOString(),
  }));
}

function deny() {
  return vi.spyOn(protocol, "authorizePreparedInvocation").mockImplementation(async () => {
    throw new protocol.SafeOutputAuthorizationError("denied", { state: "denied", reason_code: "denied" });
  });
}

describe("manager prepared authorization boundary", () => {
  it("does not evaluate a custom module or initialize its factory before allow", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_SCRIPTS", JSON.stringify({ protected: "protected.cjs" }));
    const authorize = allow();
    authorize.mockImplementation(async ({ invocation }) => {
      expect(global.__authorizationScriptLoads).toBe(0);
      expect(global.__authorizationScriptFactories).toBe(0);
      expect(global.__authorizationScriptCalls).toBe(0);
      expect(invocation.scope.binding).toBe("invocation");
      return { state: "allowed", expires_at: new Date(Date.now() + 30000).toISOString() };
    });
    const handlers = await loadHandlers({ protected: { authorization: requirement, builtin: false } });
    expect(global.__authorizationScriptLoads).toBe(0);
    const { results } = await processMessages(handlers, [{ type: "protected", body: "private-body" }]);
    expect(results[0]).toMatchObject({ success: true, authorization: { state: "allowed" } });
    expect(global.__authorizationScriptLoads).toBe(1);
    expect(global.__authorizationScriptFactories).toBe(1);
    expect(global.__authorizationScriptCalls).toBe(1);
    expect(JSON.stringify(authorize.mock.calls)).not.toContain("private-body");
  });

  it("does not let a direct handler-map call bypass the prepared invocation boundary", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_SCRIPTS", JSON.stringify({ protected: "protected.cjs" }));
    const authorize = allow();
    const handlers = await loadHandlers({ protected: { authorization: requirement } });
    await expect(handlers.get("protected")({ type: "protected" })).rejects.toThrow("trusted prepared invocation context");
    expect(authorize).not.toHaveBeenCalled();
    expect(global.__authorizationScriptLoads).toBe(0);
    expect(global.__authorizationScriptFactories).toBe(0);
  });

  it("never enters native initialization on denial and preserves independent outputs", async () => {
    const factory = vi.spyOn(nativeModule, "main").mockResolvedValue(vi.fn());
    const authorize = deny();
    const handlers = await loadHandlers({
      create_issue: { authorization: requirement, builtin: false },
      report_incomplete: { builtin: true },
    });
    expect(factory).not.toHaveBeenCalled();
    const { results } = await processMessages(handlers, [
      { type: "create_issue", output_id: "so_aaaaaaaaaaaa", title: "Blocked" },
      { type: "create_issue", "depends-on": ["so_aaaaaaaaaaaa"], title: "Dependent" },
      { type: "report_incomplete", reason: "Independent diagnostic" },
    ]);
    expect(factory).not.toHaveBeenCalled();
    expect(authorize).toHaveBeenCalledTimes(1);
    expect(results).toHaveLength(3);
    expect(results[0]).toMatchObject({ success: false, authorization: { state: "denied" } });
    expect(results[1].success).toBe(false);
    expect(results[2].success).toBe(true);
  });

  it.each(["partial", "indeterminate"])("retains a native %s outcome instead of reporting a retryable failure", async operation_outcome => {
    vi.spyOn(nativeModule, "main").mockResolvedValue(async () => ({ success: false, operation_outcome }));
    allow();
    const handlers = await loadHandlers({ create_issue: { authorization: requirement } });
    await processMessages(handlers, [{ type: "create_issue", title: "Native result" }]);
    expect(protocol.recordAuthorizationOutcome).toHaveBeenCalledWith(expect.objectContaining({ operationStatus: operation_outcome }));
  });

  it.each(["noop", "report_missing_tool", "report_missing_data", "report_incomplete"])("exempts compiler-classified %s from global inheritance only", async type => {
    const authorize = deny();
    const handlers = await loadHandlers({ authorization: { ...requirement, source: "global" }, [type]: { builtin: true } });
    const { results } = await processMessages(handlers, [{ type, message: "diagnostic", reason: "diagnostic" }]);
    expect(authorize).not.toHaveBeenCalled();
    expect(results[0].authorization).toMatchObject({ state: "exempt_builtin" });
    const explicit = await loadHandlers({ authorization: { ...requirement, source: "global" }, [type]: { builtin: true, authorization: requirement } });
    const denied = await processMessages(explicit, [{ type, message: "diagnostic" }]);
    expect(authorize).toHaveBeenCalledTimes(1);
    expect(denied.results[0]).toMatchObject({ success: false, authorization: { state: "denied" } });
  });

  it("never trusts payload builtin flags or a diagnostic-looking custom name", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_SCRIPTS", JSON.stringify({ protected: "protected.cjs" }));
    const authorize = deny();
    const handlers = await loadHandlers({ authorization: { ...requirement, source: "global" }, protected: { builtin: false } });
    const { results } = await processMessages(handlers, [{ type: "protected", builtin: true, authorization: false }]);
    expect(authorize).toHaveBeenCalledOnce();
    expect(global.__authorizationScriptLoads).toBe(0);
    expect(results[0].authorization.state).toBe("denied");
  });

  it.each([true, "true"])("renders effective staged custom invocations without author/OIDC calls: %s", async staged => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_SCRIPTS", JSON.stringify({ protected: "protected.cjs" }));
    vi.stubEnv("GITHUB_REPOSITORY_ID", "");
    const authorize = deny();
    const handlers = await loadHandlers({ protected: { staged, authorization: { endpoint: "unavailable" } } });
    const { results } = await processMessages(handlers, [{ type: "protected" }]);
    expect(results[0]).toMatchObject({ success: true, staged: true, authorization: { state: "skipped_staged" } });
    expect(authorize).not.toHaveBeenCalled();
    expect(global.__authorizationScriptLoads).toBe(0);
    expect(global.__authorizationScriptFactories).toBe(0);
    expect(global.__authorizationScriptCalls).toBe(0);
  });

  it("preserves native staged validation failures when authorization is not configured", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    const execute = vi.fn().mockResolvedValue({ success: false, error: "Existing native validation failure" });
    vi.spyOn(nativeModule, "main").mockResolvedValue(execute);
    const authorize = deny();
    const handlers = await loadHandlers({ create_issue: {} });
    const { results } = await processMessages(handlers, [{ type: "create_issue" }]);
    expect(execute).toHaveBeenCalledOnce();
    expect(authorize).not.toHaveBeenCalled();
    expect(buildSafeOutputExecutions(results, true)[0]).toMatchObject({ status: "failed", staged: true });
  });

  it("preserves threat-detection policy before preparing a staged custom preview", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_SCRIPTS", JSON.stringify({ protected: "protected.cjs" }));
    vi.stubEnv("GH_AW_DETECTION_CONCLUSION", "failure");
    const authorize = deny();
    const handlers = await loadHandlers({ protected: { staged: true, authorization: requirement } });
    const { results } = await processMessages(handlers, [{ type: "protected" }]);
    expect(results[0]).toMatchObject({ success: false, errorCode: "threat_detection_failure_blocked", authorization: { state: "not_requested" } });
    expect(authorize).not.toHaveBeenCalled();
    expect(global.__authorizationScriptLoads).toBe(0);
  });

  it("binds immutable normalized target inputs and distinguishes duplicate records", async () => {
    const observed = [];
    vi.spyOn(nativeModule, "main").mockResolvedValue(async message => {
      observed.push(currentSafeOutputInvocation());
      expect(Object.isFrozen(message)).toBe(true);
      expect(Object.isFrozen(message.nested)).toBe(true);
      return { success: true };
    });
    const authorize = allow();
    const handlers = await loadHandlers({ create_issue: { authorization: requirement } });
    const message = { type: "create_issue", title: "Identical", nested: { value: 1 } };
    await processMessages(handlers, [message, { ...message }]);
    expect(authorize).toHaveBeenCalledTimes(2);
    expect(observed[0].invocation.invocation_id).not.toBe(observed[1].invocation.invocation_id);
    expect(observed[0].batchDigest).toBe(observed[1].batchDigest);
    expect(observed.map(item => item.recordIndex)).toEqual([0, 1]);
  });

  it("binds caller/default repository and inherited mentions to the same frozen factory policy", async () => {
    vi.stubEnv("GH_AW_TARGET_REPO_SLUG", "org/original");
    const config = { mentions: { "allowed-aliases": ["maintainers"] }, create_issue: { authorization: requirement, title_prefix: "Reviewed" } };
    const factory = vi.spyOn(nativeModule, "main").mockResolvedValue(async message => {
      expect(Object.isFrozen(message)).toBe(true);
      expect(Object.isFrozen(currentSafeOutputInvocation().config)).toBe(true);
      return { success: true };
    });
    const prepare = vi.spyOn(protocol, "prepareInvocation");
    const authorize = allow();
    authorize.mockImplementation(async ({ invocation }) => {
      config.create_issue.title_prefix = "Changed after preparation";
      process.env.GH_AW_TARGET_REPO_SLUG = "org/changed";
      expect(invocation.scope).toMatchObject({
        binding: "invocation",
        refs: ["source-repository-id:123", "configured-repository:org/original", "validated-repository-selector:org/original"],
      });
      return { state: "allowed", expires_at: new Date(Date.now() + 30000).toISOString() };
    });
    const handlers = await loadHandlers(config, undefined, []);
    await processMessages(handlers, [{ type: "create_issue", title: "Bound defaults" }]);
    expect(prepare.mock.calls[0][0].normalizedInputs.defaults).toEqual({ repository: "org/original", mentions: { "allowed-aliases": ["maintainers"] } });
    expect(factory).toHaveBeenCalledWith(expect.objectContaining({ "target-repo": "org/original", title_prefix: "Reviewed", mentions: { "allowed-aliases": ["maintainers"] } }), null);
  });

  it("exposes a validated native target selector without claiming resolved-effect binding", async () => {
    const update = require("./update_issue.cjs");
    vi.spyOn(update, "main").mockResolvedValue(async () => ({ success: true }));
    const authorize = allow();
    const handlers = await loadHandlers({ update_issue: { authorization: requirement, target: "*", allowed_repos: ["other/*"] } });
    await processMessages(handlers, [{ type: "update_issue", repo: "other/allowed", issue_number: 42, body: "private target content" }]);
    const invocation = authorize.mock.calls[0][0].invocation;
    expect(invocation.scope.binding).toBe("invocation");
    expect(invocation.scope.refs).toContain("validated-repository-selector:other/allowed");
    expect(invocation.scope.refs).toContain("validated-issue-selector:other/allowed#42");
    expect(JSON.stringify(invocation)).not.toContain("private target content");
  });

  it("rejects a disallowed native repository before authority or handler initialization", async () => {
    const factory = vi.spyOn(nativeModule, "main").mockResolvedValue(vi.fn());
    const authorize = allow();
    const handlers = await loadHandlers({ create_issue: { authorization: requirement } });
    const { results } = await processMessages(handlers, [{ type: "create_issue", repo: "other/disallowed", title: "Not authorized" }]);
    expect(authorize).not.toHaveBeenCalled();
    expect(factory).not.toHaveBeenCalled();
    expect(results[0]).toMatchObject({ success: false, authorization: { state: "not_requested", reason_code: "target_selector_invalid" } });
  });

  it("retains authorization separately from operation failure and post-effect audit errors", async () => {
    const handler = vi.fn().mockResolvedValue({ success: false, error: "operation failed" });
    vi.spyOn(nativeModule, "main").mockResolvedValue(handler);
    allow();
    protocol.recordAuthorizationOutcome.mockReturnValue({ evidenceRecorded: false, reason_code: "evidence_failed" });
    const handlers = await loadHandlers({ create_issue: { authorization: requirement } });
    const { results } = await processMessages(handlers, [{ type: "create_issue", title: "One attempt" }]);
    expect(handler).toHaveBeenCalledOnce();
    expect(buildSafeOutputExecutions(results)[0]).toMatchObject({ status: "failed", authorization: { state: "allowed", evidenceRecorded: false } });
  });

  it("resolves temporary targets before consuming a decision and initializes the script only on the resolved retry", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_SCRIPTS", JSON.stringify({ protected: "protected.cjs" }));
    vi.spyOn(nativeModule, "main").mockResolvedValue(async () => ({ success: true, temporaryId: "aw_parent", repo: "org/repo", number: 42 }));
    const authorize = allow();
    const handlers = await loadHandlers({ create_issue: {}, protected: { authorization: requirement } });
    const { results } = await processMessages(handlers, [
      { type: "protected", item_number: "aw_parent", body: "See #aw_parent" },
      { type: "create_issue", title: "Parent" },
    ]);
    expect(authorize).toHaveBeenCalledOnce();
    expect(global.__authorizationScriptFactories).toBe(1);
    expect(global.__authorizationScriptCalls).toBe(1);
    expect(results.find(record => record.type === "protected")).toMatchObject({
      success: true,
      deferred: false,
      result: { observed: { item_number: 42, body: "See #42" } },
      authorization: { state: "allowed" },
    });
  });

  it("does not consume custom-action decisions in the payload-export manager", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_ACTIONS", JSON.stringify({ custom_action: "custom_action" }));
    const authorize = deny();
    const action = require("./safe_output_action_handler.cjs");
    const execute = vi.fn(async () => {
      expect(currentSafeOutputInvocation().authorization).toMatchObject({ state: "not_requested", reason_code: "deferred_executor" });
      return { success: true };
    });
    vi.spyOn(action, "main").mockResolvedValue(execute);
    const handlers = await loadHandlers({ custom_action: { authorization: requirement, authorization_executor: "action" } });
    const { results } = await processMessages(handlers, [{ type: "custom_action", value: "resolved" }]);
    expect(authorize).not.toHaveBeenCalled();
    expect(execute).toHaveBeenCalledOnce();
    expect(results[0]).toMatchObject({ success: true, authorization: { state: "not_requested", reason_code: "deferred_executor" } });
  });

  it("fails closed without evaluating the custom script when decision evidence cannot persist", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_SCRIPTS", JSON.stringify({ protected: "protected.cjs" }));
    vi.spyOn(protocol, "authorizePreparedInvocation").mockRejectedValue(new protocol.SafeOutputAuthorizationError("evidence_failed", { state: "error", reason_code: "evidence_failed" }));
    const handlers = await loadHandlers({ protected: { authorization: requirement } });
    const { results } = await processMessages(handlers, [{ type: "protected" }]);
    expect(results[0]).toMatchObject({ success: false, authorization: { state: "error", reason_code: "evidence_failed" } });
    expect(global.__authorizationScriptLoads).toBe(0);
    expect(global.__authorizationScriptFactories).toBe(0);
    expect(global.__authorizationScriptCalls).toBe(0);
  });

  it("uses the trusted named repository-dispatch requirement before loading its implementation", async () => {
    const dispatch = require("./dispatch_repository.cjs");
    const execute = vi.fn().mockResolvedValue({ success: true });
    const factory = vi.spyOn(dispatch, "main").mockResolvedValue(execute);
    const authorize = allow();
    const handlers = await loadHandlers({ dispatch_repository: { tools: { launch: { authorization: requirement, repository: "org/repo" } } } });
    expect(factory).not.toHaveBeenCalled();
    await processMessages(handlers, [{ type: "dispatch_repository", tool_name: "launch" }]);
    expect(authorize).toHaveBeenCalledWith(expect.objectContaining({ requirement, invocation: expect.objectContaining({ output: expect.objectContaining({ name: "launch" }) }) }));
    expect(execute).toHaveBeenCalledOnce();
  });

  it("records the actual late native authorization rather than the deferred preparation placeholder", async () => {
    const upload = require("./upload_artifact.cjs");
    vi.spyOn(upload, "main").mockResolvedValue(async () => {
      const state = currentSafeOutputInvocation();
      expect(state.authorization.state).toBe("not_requested");
      state.authorization = { state: "allowed", decision_id: "late-native-decision" };
      return { success: true };
    });
    const authorize = deny();
    const handlers = await loadHandlers({ upload_artifact: { authorization: requirement } });
    const { results } = await processMessages(handlers, [{ type: "upload_artifact", file: "reviewed-file" }]);
    expect(authorize).not.toHaveBeenCalled();
    expect(results[0]).toMatchObject({ success: true, authorization: { state: "allowed", decision_id: "late-native-decision" } });
    expect(protocol.recordAuthorizationOutcome).toHaveBeenCalledWith(expect.objectContaining({ authorization: expect.objectContaining({ decision_id: "late-native-decision" }), operationStatus: "success" }));
  });

  it("blocks buffered publication when the original permission is no longer fresh", async () => {
    const review = require("./submit_pr_review.cjs");
    vi.spyOn(review, "main").mockResolvedValue(async () => ({ success: true, deferred_manifest: true, repo: "org/repo", pull_request_number: 7 }));
    allow();
    protocol.assertAuthorizationFresh
      .mockImplementationOnce(({ authorization }) => authorization)
      .mockImplementation(() => {
        throw new protocol.SafeOutputAuthorizationError("invalid_validity", { state: "error", reason_code: "invalid_validity" });
      });
    let writes = 0;
    const finalize = vi.fn(async (records, protectedTargets) => {
      if (!protectedTargets.has("org/repo#7")) writes++;
    });
    const handlers = await loadHandlers({ submit_pull_request_review: { authorization: requirement, target: "*", "target-repo": "org/repo" } });
    const { results } = await processMessages(handlers, [{ type: "submit_pull_request_review", pull_request_number: 7, event: "COMMENT" }], null, finalize);
    expect(finalize).toHaveBeenCalledOnce();
    expect(writes).toBe(0);
    expect(results[0]).toMatchObject({ success: false, authorization: { state: "error", reason_code: "invalid_validity" } });
    expect(protocol.recordAuthorizationOutcome).not.toHaveBeenCalled();
  });
});
