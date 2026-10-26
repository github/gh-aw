import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { buildSafeOutputOutcomes, canonicalSafeOutputUrl, projectSafeOutputData, finalizeBufferedReviewOutcomes } from "./safe_output_outcomes.cjs";
import { writeSafeOutputOutcomes, extractCreatedItemFromResult } from "./safe_output_manifest.cjs";
import { main as actionHandlerFactory } from "./safe_output_action_handler.cjs";
import { computeSafeOutputsStatus } from "./safe_outputs_status.cjs";
import { main as handlerManagerMain, processMessages, rollbackReviewResultsForPR, skipReviewResultsForPR } from "./safe_output_handler_manager.cjs";
import { writeUnifiedSession } from "./unified_session.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("safe-output application outcomes", () => {
  it.each([
    [{ success: true }, "success"],
    [{ success: false, error: "PRIVATE_ERROR" }, "failed"],
    [{ success: true, skipped: true }, "skipped"],
    [{ success: true, result: { warning: "PRIVATE_WARNING" } }, "warning"],
    [{ cancelled: true }, "cancelled"],
    [{ deferred: true }, "deferred"],
    [{ delegated: true, success: true }, "delegated"],
    [{ success: true, result: { deferred_manifest: true } }, "deferred"],
    [{ success: true, result: { staged: true } }, "staged"],
  ])("records %j as %s without content", (result, status) => {
    const [outcome] = buildSafeOutputOutcomes([{ type: "add_comment", messageIndex: 0, body: "PRIVATE_BODY", metadata: { content: "PRIVATE_METADATA" }, ...result }]);
    expect(outcome).toMatchObject({ type: "add_comment", messageIndex: 0, status });
    expect(JSON.stringify(outcome)).not.toContain("PRIVATE");
    expect(Object.keys(outcome).sort()).toEqual(["messageIndex", "staged", "status", "timestamp", "type"]);
  });

  it("does not describe staged previews or rolled-back reviews as applied", () => {
    const result = { type: "submit_pull_request_review", messageIndex: 0, success: true, result: { repo: "org/repo", pull_request_number: 1, deferred_manifest: true, review_url: "https://github.com/org/repo/pull/1#pullrequestreview-5" } };
    expect(buildSafeOutputOutcomes([result], true)[0]).toMatchObject({ status: "deferred", staged: true });
    rollbackReviewResultsForPR([result], "org/repo", 1, "PRIVATE_ERROR");
    expect(buildSafeOutputOutcomes([result])[0]).toMatchObject({ status: "failed" });
    expect(buildSafeOutputOutcomes([result])[0]).not.toHaveProperty("url");
    const skipped = { ...result, success: true };
    skipReviewResultsForPR([skipped], "org/repo", 1, "PRIVATE_REASON");
    expect(buildSafeOutputOutcomes([skipped])[0]).toMatchObject({ status: "skipped" });
    expect(buildSafeOutputOutcomes([{ success: true }], true)[0]).toMatchObject({ status: "staged", staged: true });
  });

  it("fans out array results without promoting failed, skipped, or empty results to applied", () => {
    const outcomes = buildSafeOutputOutcomes([
      {
        type: "add_comment",
        messageIndex: 3,
        success: true,
        result: [
          { success: true, url: "https://github.com/org/repo/issues/1#issuecomment-1", body: "PRIVATE_BODY" },
          { success: false, error: "PRIVATE_ERROR" },
          { skipped: true, reason: "PRIVATE_REASON" },
        ],
      },
      { type: "add_comment", messageIndex: 4, success: true, result: [] },
    ]);
    expect(outcomes.map(outcome => outcome.status)).toEqual(["success", "failed", "skipped", "skipped"]);
    expect(outcomes.map(outcome => outcome.messageIndex)).toEqual([3, 3, 3, 4]);
    expect(outcomes[0].url).toBe("https://github.com/org/repo/issues/1#issuecomment-1");
    expect(JSON.stringify(outcomes)).not.toContain("PRIVATE");
  });

  it("uses durable inline comment URLs and marks filtered comments skipped", () => {
    const makeComment = line => ({ type: "create_pull_request_review_comment", success: true, result: { repo: "org/repo", pull_request_number: 1, deferred_manifest: true, metadata: { path: "PRIVATE_PATH", line, side: "RIGHT" } } });
    const records = [makeComment(2), makeComment(3), { type: "submit_pull_request_review", success: true, result: { repo: "org/repo", pull_request_number: 1, deferred_manifest: true } }];
    finalizeBufferedReviewOutcomes(records, "org/repo", 1, {
      success: true,
      review_url: "https://github.com/org/repo/pull/1#pullrequestreview-5",
      comment_count: 1,
      review_comments: [{ url: "https://github.com/org/repo/pull/1#discussion_r6", metadata: { path: "PRIVATE_PATH", line: 2, side: "RIGHT" } }],
    });
    const outcomes = buildSafeOutputOutcomes(records);
    expect(outcomes.map(outcome => outcome.status)).toEqual(["success", "skipped", "success"]);
    expect(outcomes[0].url).toBe("https://github.com/org/repo/pull/1#discussion_r6");
    expect(outcomes[2].url).toBe("https://github.com/org/repo/pull/1#pullrequestreview-5");
    expect(JSON.stringify(outcomes)).not.toContain("PRIVATE");
  });

  it("keeps incomplete review evidence deferred and staged reviews non-applied", () => {
    const record = { type: "create_pull_request_review_comment", success: true, result: { repo: "org/repo", pull_request_number: 1, deferred_manifest: true, metadata: { path: "PRIVATE_PATH", line: 2 } } };
    finalizeBufferedReviewOutcomes([record], "org/repo", 1, { success: true, comment_count: 2, review_comments: [] });
    expect(buildSafeOutputOutcomes([record])[0].status).toBe("deferred");
    finalizeBufferedReviewOutcomes([record], "org/repo", 1, { success: true, staged: true });
    expect(buildSafeOutputOutcomes([record])[0].status).toBe("staged");
  });

  it("redacts configured secrets before JSON serialization, including quoted and multiline inputs", () => {
    const root = fs.mkdtempSync(path.join(process.cwd(), ".safe-output-outcomes-test-"));
    const secrets = ["opaque-secret", 'opaque"quoted', "opaque\nmultiline"];
    vi.stubEnv("GH_AW_SECRET_NAMES", "PLAIN,QUOTED,MULTILINE");
    for (const [index, name] of ["PLAIN", "QUOTED", "MULTILINE"].entries()) vi.stubEnv(`SECRET_${name}`, secrets[index]);
    try {
      const file = path.join(root, "safe-output-outcomes.jsonl");
      writeSafeOutputOutcomes(
        secrets.map(secret => ({ type: secret, success: true, result: { url: `https://github.com/${secret}/repo/issues/1`, body: secret } })),
        false,
        file
      );
      const content = fs.readFileSync(file, "utf8");
      for (const secret of secrets) {
        expect(content).not.toContain(secret);
        expect(content).not.toContain(JSON.stringify(secret).slice(1, -1));
      }
      expect(content).not.toContain("opaque");
      expect(
        content
          .trim()
          .split("\n")
          .map(line => JSON.parse(line))
          .map(entry => entry.status)
      ).toEqual(["success", "success", "success"]);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it.each([
    ["https://github.com/org/repo/issues/2?token=PRIVATE#issuecomment-9", "https://github.com/org/repo/issues/2#issuecomment-9"],
    ["https://github.com/org/repo/pull/2?signature=PRIVATE#PRIVATE_BODY", "https://github.com/org/repo/pull/2"],
    ["https://github.com/org/repo/pull/2#discussion_r123", "https://github.com/org/repo/pull/2#discussion_r123"],
    ["https://github.com/org/repo/actions/runs/123", "https://github.com/org/repo/actions/runs/123"],
    ["https://github.com/org/repo/actions/artifacts/123", "https://github.com/org/repo/actions/artifacts/123"],
    ["https://github.com/org/repo/actions/runs/123/artifacts/456?signature=PRIVATE", "https://github.com/org/repo/actions/runs/123/artifacts/456"],
    ["https://github.com/org/repo/actions/runs/123/job/456?token=PRIVATE#PRIVATE_LOG", "https://github.com/org/repo/actions/runs/123/job/456"],
    ["https://github.com/org/repo/issues/123/artifacts/456", undefined],
    ["https://github.com/org/repo/actions/runs/123/job/PRIVATE_TITLE", undefined],
    ["https://github.com/org/repo/check-runs/123", "https://github.com/org/repo/check-runs/123"],
    ["https://github.com/orgs/org/projects/1", "https://github.com/orgs/org/projects/1"],
    ["******github.com/org/repo/issues/2", undefined],
    ["http://github.com/org/repo/issues/2", undefined],
    ["https://github.com.evil.example/org/repo/issues/2", undefined],
    ["https://github.com/org/repo/blob/main/PRIVATE_BODY", undefined],
    ["https://example.com/PRIVATE_BODY", undefined],
    ["https://linear.app/team/issue/ENG-123/PRIVATE_TITLE", undefined],
    ["https://team.atlassian.net/browse/ENG-123", undefined],
    ["https://dev.azure.com/org/project/_workitems/edit/123", undefined],
    ["javascript:PRIVATE", undefined],
    [{ url: "PRIVATE" }, undefined],
  ])("canonicalizes %s conservatively", (input, expected) => {
    expect(canonicalSafeOutputUrl(input)).toBe(expected);
  });

  it("supports the configured GitHub Enterprise origin without accepting other hosts", () => {
    vi.stubEnv("GITHUB_SERVER_URL", "https://github.example.com");
    expect(canonicalSafeOutputUrl("https://github.example.com/org/repo/issues/2?secret=PRIVATE")).toBe("https://github.example.com/org/repo/issues/2");
    expect(canonicalSafeOutputUrl("https://other.example.com/org/repo/issues/2")).toBeUndefined();
  });

  it("rejects credential-bearing resource URLs", () => {
    const url = new URL("https://github.com/org/repo/issues/2");
    url.username = "PRIVATE_USER";
    url.password = "PRIVATE_PASSWORD";
    expect(canonicalSafeOutputUrl(url.toString())).toBeUndefined();
  });

  it.each([
    "url",
    "html_url",
    "issue_url",
    "pull_request_url",
    "discussion_url",
    "comment_url",
    "reply_url",
    "review_url",
    "item_url",
    "commit_url",
    "check_run_url",
    "projectUrl",
    "autofixUrl",
    "artifactUrl",
    "run_url",
    "workflow_run_url",
  ])("retains a canonical URL from the %s result alias", field => {
    const [outcome] = buildSafeOutputOutcomes([{ type: "custom_output", success: true, result: { [field]: "https://github.com/org/repo/issues/1?secret=PRIVATE" } }]);
    expect(outcome.url).toBe("https://github.com/org/repo/issues/1");
  });

  it("falls back to a safe result alias and accepts opaque commit/check identities", () => {
    const [outcome] = buildSafeOutputOutcomes([{ type: "create_check_run", success: true, result: { url: "https://example.com/PRIVATE", check_run_url: "https://github.com/org/repo/runs/123?secret=PRIVATE" } }]);
    expect(outcome.url).toBe("https://github.com/org/repo/runs/123");
    expect(canonicalSafeOutputUrl(`https://github.com/org/repo/commit/${"a".repeat(40)}?secret=PRIVATE`)).toBe(`https://github.com/org/repo/commit/${"a".repeat(40)}`);
    expect(canonicalSafeOutputUrl("https://github.com/org/repo/commit/PRIVATE_TITLE")).toBeUndefined();
  });

  it("projects legacy evidence without raw content or object-valued identifiers", () => {
    expect(
      projectSafeOutputData({
        type: "create_issue",
        number: 1,
        status: "success",
        repo: { body: "PRIVATE" },
        error: "PRIVATE",
        message: "PRIVATE",
        metadata: "PRIVATE",
        title: "PRIVATE",
        url: "https://github.com/org/repo/issues/1?auth=PRIVATE",
      })
    ).toEqual({
      type: "create_issue",
      number: 1,
      status: "success",
      url: "https://github.com/org/repo/issues/1",
    });
  });

  it("records custom action payload exports as delegated, never applied", async () => {
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() });
    vi.stubGlobal("context", { repo: { owner: "org", repo: "repo" }, payload: {} });
    const handler = await actionHandlerFactory({ action_name: "external_action" });
    const processing = await processMessages(new Map([["external_action", handler]]), [{ type: "external_action", body: "PRIVATE_BODY", url: "https://github.com/org/repo/issues/999" }]);
    const [outcome] = buildSafeOutputOutcomes(processing.results);
    expect(outcome).toMatchObject({ type: "external_action", messageIndex: 0, status: "delegated" });
    expect(outcome).not.toHaveProperty("url");
    expect(JSON.stringify(outcome)).not.toContain("PRIVATE");
    expect(computeSafeOutputsStatus(processing.results)).toMatchObject({ itemsApplied: 0, itemsSucceeded: 0, itemsFailed: 0 });
    expect(extractCreatedItemFromResult("external_action", processing.results[0].result)).toBeNull();
  });

  it.each([
    ["success", "staged"],
    ["handler_failure", "failed"],
    ["manager_catch", undefined],
    ["custom_job", "delegated"],
  ])("persists outcomes through main's finally hook on %s", async (scenario, expectedStatus) => {
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() });
    vi.stubGlobal("context", { eventName: "workflow_dispatch", repo: { owner: "org", repo: "repo" }, payload: {} });
    vi.stubGlobal("github", { rest: { rateLimit: { get: vi.fn().mockResolvedValue({ data: { resources: { core: { remaining: 5000, limit: 5000, reset: 0 } } } }) } } });
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", scenario === "manager_catch" ? "invalid-json" : JSON.stringify(scenario === "custom_job" ? {} : { noop: {} }));
    vi.stubEnv("GH_AW_SAFE_OUTPUT_JOBS", scenario === "custom_job" ? JSON.stringify({ external_job: "payload" }) : "");
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    vi.spyOn(fs, "writeFileSync").mockImplementation(() => undefined);
    const append = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    const message = scenario === "custom_job" ? { type: "external_job", body: "PRIVATE_BODY" } : { type: "noop", ...(scenario === "handler_failure" ? {} : { message: "PRIVATE_MESSAGE" }) };
    await handlerManagerMain({ messages: [message] });
    expect(append).toHaveBeenCalledTimes(1);
    expect(String(append.mock.calls[0][0])).toMatch(/safe-output-outcomes\.jsonl$/);
    const content = String(append.mock.calls[0][1]);
    expect(content).not.toContain("PRIVATE");
    if (expectedStatus) {
      expect(JSON.parse(content)).toMatchObject({ type: message.type, messageIndex: 0, status: expectedStatus, staged: true });
    } else {
      expect(content).toBe("");
    }
    if (scenario === "handler_failure" || scenario === "manager_catch") expect(global.core.setFailed).toHaveBeenCalled();
    else expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("propagates handler-specific staged review finalization through main without claiming application", async () => {
    const pullRequest = { number: 1, state: "open", head: { sha: "abc123" }, user: { login: "review-author" } };
    const summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn(), summary });
    vi.stubGlobal("context", { eventName: "pull_request", repo: { owner: "org", repo: "repo" }, payload: { pull_request: pullRequest } });
    const createReview = vi.fn();
    vi.stubGlobal("github", {
      rest: {
        rateLimit: { get: vi.fn().mockResolvedValue({ data: { resources: { core: { remaining: 5000, limit: 5000, reset: 0 } } } }) },
        pulls: { get: vi.fn().mockResolvedValue({ data: pullRequest }), createReview },
      },
    });
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "false");
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify({ submit_pull_request_review: { staged: true } }));
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    vi.spyOn(fs, "writeFileSync").mockImplementation(() => undefined);
    const append = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    await handlerManagerMain({ messages: [{ type: "submit_pull_request_review", pull_request_number: 1, body: "PRIVATE_REVIEW_BODY", event: "COMMENT" }] });
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(createReview).not.toHaveBeenCalled();
    const outcomesCall = append.mock.calls.find(([file]) => String(file).endsWith("safe-output-outcomes.jsonl"));
    expect(outcomesCall).toBeDefined();
    const outcome = JSON.parse(String(outcomesCall[1]));
    expect(outcome).toMatchObject({ type: "submit_pull_request_review", messageIndex: 0, status: "staged", staged: true });
    expect(outcome).not.toHaveProperty("url");
    expect(JSON.stringify(outcome)).not.toContain("PRIVATE");
  });

  it("persists real processing results and collects root and Claim outcomes into the unified session", async () => {
    const root = fs.mkdtempSync(path.join(process.cwd(), ".safe-output-outcomes-test-"));
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() });
    vi.stubGlobal("context", { repo: { owner: "org", repo: "repo" }, payload: {} });
    try {
      const messages = [
        { type: "create_issue", title: "PRIVATE_TITLE", body: "PRIVATE_BODY" },
        { type: "add_comment", body: "PRIVATE_BODY" },
        { type: "add_labels", labels: ["PRIVATE_LABEL"] },
      ];
      const handlers = new Map([
        ["create_issue", async () => ({ success: true, url: "https://github.com/org/repo/issues/1?token=PRIVATE" })],
        [
          "add_comment",
          async () => {
            throw new Error("PRIVATE_ERROR");
          },
        ],
        ["add_labels", async () => ({ skipped: true, reason: "PRIVATE_REASON" })],
      ]);
      const processing = await processMessages(handlers, messages);
      writeSafeOutputOutcomes(processing.results, false, path.join(root, "safe-output-outcomes.jsonl"));
      writeSafeOutputOutcomes([{ type: "add_comment", messageIndex: 0, delegated: true }], false, path.join(root, "claims/claim-1/safe-output-outcomes.jsonl"));
      const events = writeUnifiedSession({ rootDir: root, graders: [], jobResults: JSON.stringify({ external_job: { result: "failure", outputs: { body: "PRIVATE_OUTPUT" } } }) });
      const outcomes = events.filter(event => event.type === "safe_output.outcome");
      expect(outcomes.map(event => event.data.status)).toEqual(["success", "failed", "skipped", "delegated"]);
      expect(outcomes[0].data.url).toBe("https://github.com/org/repo/issues/1");
      expect(outcomes.every(event => event.provenance.phase === "safe_outputs")).toBe(true);
      expect(events.find(event => event.type === "workflow.job_result").data).toEqual({ jobId: "external_job", result: "failure" });
      const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
      expect(persisted).not.toContain("PRIVATE");
      expect(validateSession(persisted)).toBe(events.length);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
