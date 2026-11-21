// @ts-check
"use strict";

import { afterEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const path = require("node:path");
const { selectCheckpoint, restoreCheckpoint } = require("./durable_artifact.cjs");
const { checkpointArtifactPrefix } = require("./continuation_request.cjs");
const { stageFiles } = require("./durable_files.cjs");
const directories = [];

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

function fixture() {
  vi.stubEnv("GH_AW_DURABLE_WORKFLOW", "continuable");
  vi.stubEnv("GITHUB_RUN_ATTEMPT", "1");
  const current = {
    id: 200,
    workflow_id: 42,
    run_number: 20,
    run_attempt: 1,
    head_branch: "feature",
    repository: { id: 1 },
    head_repository: { id: 1 },
    status: "in_progress",
    event: "workflow_dispatch",
    actor: { login: "github-actions[bot]" },
  };
  const parent = { ...structuredClone(current), id: 100, run_number: 19, status: "in_progress", conclusion: null };
  const relay = {
    id: 300,
    workflow_id: 77,
    event: "workflow_dispatch",
    path: ".github/workflows/agentic-continuation-relay.yml",
    head_branch: "main",
    display_title: "Continuation relay 100/1",
    repository: { id: 1 },
    head_repository: { id: 1 },
    status: "in_progress",
  };
  const core = { setOutput: vi.fn(), info: vi.fn(), warning: vi.fn() };
  const state = {
    currentArtifacts: [],
    relayArtifacts: [{ id: 777, name: "continuation-dispatched", expired: false, expires_at: "2999-01-01T00:00:00Z" }],
  };
  const github = {
    rest: {
      actions: {
        getWorkflowRun: vi.fn(async ({ run_id }) => ({ data: { 200: current, 300: relay }[String(run_id)] ?? parent })),
        listWorkflowRuns: vi.fn(async () => ({ data: { workflow_runs: [parent] } })),
        listWorkflowRunArtifacts: vi.fn(),
      },
      repos: { get: vi.fn(async () => ({ data: { default_branch: "main" } })) },
    },
    paginate: vi.fn(async (_method, { run_id }) => {
      const name = core.setOutput.mock.calls.find(([key]) => key === "artifact-name")[1].replace(/200-\d+$/, "100-1");
      if (String(run_id) === "200") return state.currentArtifacts;
      if (String(run_id) === "300") return state.relayArtifacts;
      return [
        { id: 999, name, expired: false, expires_at: "2999-01-01T00:00:00Z" },
        { id: 555, name: "continuation-request-1", expired: false, expires_at: "2999-01-01T00:00:00Z" },
      ];
    }),
  };
  const context = { repo: { owner: "owner", repo: "repo" }, runId: 200, eventName: "workflow_dispatch", ref: "refs/heads/feature", payload: {} };
  const request = { version: 1, hints: "Resume at step 3", depth: 1, max: 5, parent_run_id: "100", parent_run_attempt: "1", aw_context: {} };
  const readRequest = vi.fn(async () => structuredClone(request));
  const readDispatchEvidence = vi.fn(async () => {
    const awContext = JSON.parse(context.payload.inputs.aw_context);
    return {
      parent_run_id: "100",
      parent_run_attempt: awContext.run_attempt,
      checkpoint_run_attempt: awContext.checkpoint_attempt,
      relay_run_id: "300",
      child_run_id: "200",
    };
  });
  const persistContinuationContext = vi.fn(async () => {});
  const readContinuationContext = vi.fn(async () => undefined);
  return { github, context, core, current, parent, relay, request, readRequest, readDispatchEvidence, persistContinuationContext, readContinuationContext, state };
}

function continuationPayload(overrides = {}) {
  return { inputs: { aw_context: JSON.stringify({ run_id: "100", run_attempt: "1", checkpoint_attempt: "1", relay_run_id: "300", continuation: { depth: 1, max: 5 }, ...overrides }) } };
}

describe("durable artifact lineage", () => {
  it("starts fresh without listing runs or artifacts when no continuation was requested", async () => {
    const options = fixture();
    const result = await selectCheckpoint(options);
    expect(result.continuation).toBeUndefined();
    expect(options.github.paginate).not.toHaveBeenCalled();
    expect(options.github.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
    expect(options.core.setOutput).toHaveBeenCalledWith("continuation-depth", "0");
    expect(options.core.setOutput.mock.calls.some(([key]) => key === "artifact-id")).toBe(false);
  });

  it("ignores ordinary aw_context lineage that carries no continuation request", async () => {
    const options = fixture();
    options.context.payload = { inputs: { aw_context: JSON.stringify({ run_id: "100" }) } };
    await selectCheckpoint(options);
    expect(options.github.paginate).not.toHaveBeenCalled();
    expect(options.core.setOutput.mock.calls.some(([key]) => key === "artifact-id")).toBe(false);
  });

  it.each([
    ["missing relay lineage", options => (options.context.payload = continuationPayload({ relay_run_id: undefined })), "requires aw_context.relay_run_id"],
    ["direct dispatch by a user", options => (options.current.actor.login = "octocat"), "must be dispatched by the continuation relay"],
    ["relay from another branch", options => (options.relay.head_branch = "attacker"), "must run from the default branch"],
    ["relay copy at another path", options => (options.relay.path = ".github/workflows/copy.yml"), "must come from"],
    ["unrelated relay run", options => (options.relay.display_title = "Continuation relay 99/1"), "not bound to run 100 attempt 1"],
    ["non-dispatch relay run", options => (options.relay.event = "push"), "must be a workflow_dispatch run"],
  ])("rejects forged continuation lineage: %s", async (_name, mutate, message) => {
    const options = fixture();
    options.context.payload = continuationPayload();
    mutate(options);
    await expect(selectCheckpoint(options)).rejects.toThrow(message);
    expect(options.readRequest).not.toHaveBeenCalled();
  });

  it("restores a still-running parent that recorded a continuation request and reads hints from that request", async () => {
    const options = fixture();
    options.context.payload = continuationPayload();
    const result = await selectCheckpoint(options);
    expect(result.continuation).toEqual({ depth: 1, max: 5, hints: "Resume at step 3" });
    expect(options.readRequest).toHaveBeenCalledWith(555);
    expect(options.core.setOutput).toHaveBeenCalledWith("relay-run-id", "300");
    expect(options.github.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
    expect(options.github.rest.actions.getWorkflowRun).toHaveBeenCalledWith(expect.objectContaining({ run_id: "100" }));
    expect(options.github.rest.actions.getWorkflowRun).toHaveBeenCalledWith(expect.objectContaining({ run_id: "300" }));
    expect(options.core.setOutput).toHaveBeenCalledWith("artifact-id", "999");
    expect(options.core.setOutput).toHaveBeenCalledWith("run-id", "100");
    expect(options.core.setOutput).toHaveBeenCalledWith("continuation-depth", "1");
  });

  it("accepts a relay workflow path with an @ref suffix", async () => {
    const options = fixture();
    options.context.payload = continuationPayload();
    options.relay.path += "@refs/heads/main";
    await expect(selectCheckpoint(options)).resolves.toMatchObject({ continuation: { hints: "Resume at step 3" } });
  });

  it("never takes hints from aw_context", async () => {
    const options = fixture();
    options.context.payload = continuationPayload({ continuation: { depth: 1, max: 5, hints: "injected" } });
    const result = await selectCheckpoint(options);
    expect(result.continuation?.hints).toBe("Resume at step 3");
  });

  it("selects the request of the parent attempt named in aw_context", async () => {
    const options = fixture();
    options.context.payload = continuationPayload({ run_attempt: "2" });
    options.relay.display_title = "Continuation relay 100/2";
    await expect(selectCheckpoint(options)).rejects.toThrow("no unexpired continuation request");
  });

  it.each([
    ["missing", artifacts => artifacts.filter(item => !item.name.startsWith("continuation-request-"))],
    ["expired", artifacts => artifacts.map(item => (item.name.startsWith("continuation-request-") ? { ...item, expired: true } : item))],
  ])("fails closed when the parent continuation request is %s", async (_kind, transform) => {
    const options = fixture();
    options.context.payload = continuationPayload();
    const artifacts = options.github.paginate.getMockImplementation();
    options.github.paginate.mockImplementation(async (method, args) => transform(await artifacts(method, args)));
    await expect(selectCheckpoint(options)).rejects.toThrow("no unexpired continuation request");
    expect(options.readRequest).not.toHaveBeenCalled();
  });

  it.each([
    ["depth mismatch", request => (request.depth = 2), "does not match"],
    ["wrong parent", request => (request.parent_run_id = "101"), "does not belong"],
    ["oversized hints", request => (request.hints = "x".repeat(4001)), "at most 4000"],
  ])("rejects a parent request with a %s", async (_kind, mutate, message) => {
    const options = fixture();
    options.context.payload = continuationPayload();
    mutate(options.request);
    await expect(selectCheckpoint(options)).rejects.toThrow(message);
  });

  it("restores the latest published earlier attempt before reusing the inbound parent", async () => {
    const options = fixture();
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "3");
    options.context.payload = continuationPayload();
    options.github.paginate.mockImplementation(async (_method, { run_id }) => {
      const name = options.core.setOutput.mock.calls.find(([key]) => key === "artifact-name")[1];
      if (String(run_id) === "300") return options.state.relayArtifacts;
      return String(run_id) === "200"
        ? [1, 2, 3].map(attempt => ({ id: 900 + attempt, name: name.replace(/200-3$/, `200-${attempt}`), expired: false, expires_at: "2999-01-01T00:00:00Z" }))
        : [{ id: 555, name: "continuation-request-1", expired: false, expires_at: "2999-01-01T00:00:00Z" }];
    });
    await selectCheckpoint(options);
    expect(options.core.setOutput).toHaveBeenCalledWith("artifact-id", "902");
    expect(options.core.setOutput).toHaveBeenCalledWith("run-id", "200");
  });

  it("restores a child rerun from its saved authenticated lineage without the parent request", async () => {
    const options = fixture();
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
    options.context.payload = continuationPayload();
    const name = `${checkpointArtifactPrefix("continuable", options.current)}200-1`;
    options.state.currentArtifacts = [
      { id: 901, name: "continuation-context-1", expired: false, expires_at: "2999-01-01T00:00:00Z" },
      { id: 902, name, expired: false, expires_at: "2999-01-01T00:00:00Z" },
    ];
    options.readContinuationContext.mockResolvedValue({
      version: 1,
      run_id: "200",
      parent_run_id: "100",
      parent_run_attempt: "1",
      checkpoint_run_attempt: "1",
      relay_run_id: "300",
      depth: 1,
      max: 5,
      hints: "Resume at step 3",
    });
    const result = await selectCheckpoint(options);
    expect(result.continuation).toEqual({ depth: 1, max: 5, hints: "Resume at step 3" });
    expect(options.readRequest).not.toHaveBeenCalled();
    expect(options.readDispatchEvidence).not.toHaveBeenCalled();
    expect(options.github.paginate).toHaveBeenCalledWith(options.github.rest.actions.listWorkflowRunArtifacts, expect.objectContaining({ run_id: 200 }));
  });

  it("waits for relay evidence only for a bounded interval", async () => {
    const options = fixture();
    options.context.payload = continuationPayload();
    options.state.relayArtifacts = [];
    await expect(selectCheckpoint({ ...options, dispatchEvidenceWaitMs: 0 })).rejects.toThrow("has not published dispatch evidence");
    expect(options.readRequest).not.toHaveBeenCalled();
  });

  it("falls back to the requesting parent when an earlier attempt published no checkpoint", async () => {
    const options = fixture();
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
    options.context.payload = continuationPayload();
    const parentArtifacts = options.github.paginate.getMockImplementation();
    options.github.paginate.mockImplementation(async (method, args) => {
      if (String(args.run_id) === "300") return options.state.relayArtifacts;
      return String(args.run_id) === "200" ? [] : parentArtifacts(method, args);
    });
    await selectCheckpoint(options);
    expect(options.core.setOutput).toHaveBeenCalledWith("run-id", "100");
  });

  it("rejects continuation lineage from repository_dispatch context", async () => {
    const options = fixture();
    options.current.event = "repository_dispatch";
    options.context.payload = { client_payload: { aw_context: { run_id: "100", run_attempt: "1", relay_run_id: "300", continuation: { depth: 2, max: 3 } } } };
    options.request.depth = 2;
    await expect(selectCheckpoint(options)).rejects.toThrow("must be workflow_dispatch runs");
    expect(options.readRequest).not.toHaveBeenCalled();
  });

  it.each(["branch", "workflow", "fork"])("rejects a %s-incompatible parent", async kind => {
    const options = fixture();
    options.context.payload = continuationPayload();
    if (kind === "branch") options.parent.head_branch = "main";
    if (kind === "workflow") options.parent.workflow_id = 43;
    if (kind === "fork") options.parent.head_repository.id = 2;
    await expect(selectCheckpoint(options)).rejects.toThrow("same workflow and branch");
    expect(options.github.paginate).not.toHaveBeenCalled();
  });

  it.each([[{ run_id: "" }], [{ run_id: "200" }], [{ run_id: "abc" }]])("rejects continuation without a distinct parent run %j", async overrides => {
    const options = fixture();
    options.context.payload = continuationPayload(overrides);
    await expect(selectCheckpoint(options)).rejects.toThrow("aw_context.run_id");
  });

  it("starts fresh with the continuation hints when the parent published no checkpoint", async () => {
    const options = fixture();
    options.context.payload = continuationPayload();
    options.github.paginate.mockImplementation(async (_method, { run_id }) => (String(run_id) === "300" ? options.state.relayArtifacts : [{ id: 555, name: "continuation-request-1", expired: false, expires_at: "2999-01-01T00:00:00Z" }]));
    const result = await selectCheckpoint(options);
    expect(result.continuation?.hints).toBe("Resume at step 3");
    expect(options.core.warning).toHaveBeenCalledWith(expect.stringContaining("no unexpired durable checkpoint"));
    expect(options.core.setOutput.mock.calls.some(([key]) => key === "artifact-id")).toBe(false);
  });

  it("surfaces API errors instead of silently starting a new session", async () => {
    const options = fixture();
    options.context.payload = continuationPayload();
    options.github.paginate.mockRejectedValue(new Error("403 forbidden"));
    await expect(selectCheckpoint(options)).rejects.toThrow("403 forbidden");
  });

  it("rejects malformed inbound context rather than silently starting fresh", async () => {
    const options = fixture();
    options.context.payload = { inputs: { aw_context: "{invalid" } };
    await expect(selectCheckpoint(options)).rejects.toThrow("valid aw_context");
    expect(options.github.paginate).not.toHaveBeenCalled();
  });

  it("never exposes a trusted checkpoint to a fork run with the same branch name", async () => {
    const options = fixture();
    options.current.head_repository.id = 2;
    options.context.payload = continuationPayload();
    await expect(selectCheckpoint(options)).rejects.toThrow("fork workflow runs");
    expect(options.github.paginate).not.toHaveBeenCalled();
  });
});

describe("single-step durable restoration", () => {
  function restoreFixture(engine = "gemini") {
    const options = fixture();
    const root = fs.mkdtempSync(path.resolve(".durable-restore-test-"));
    directories.push(root);
    const session = path.join(root, "sessions/state.jsonl");
    fs.mkdirSync(path.dirname(session), { recursive: true });
    fs.writeFileSync(session, "previous task state");
    vi.stubEnv("GH_AW_DURABLE_ENGINE", engine);
    vi.stubEnv("GH_AW_DURABLE_FILES", JSON.stringify([session]));
    stageFiles({ root, core: { info() {}, setOutput() {} } });
    const saved = path.join(root, "saved");
    fs.cpSync(path.join(root, "durable/bundle"), saved, { recursive: true });
    fs.rmSync(path.join(root, "durable/bundle"), { recursive: true });
    fs.unlinkSync(session);
    const promptFile = path.join(root, "prompt.txt");
    fs.writeFileSync(promptFile, "Current task");
    const userPromptFile = path.join(root, "user.txt");
    const systemPromptFile = path.join(root, "system.txt");
    if (engine === "pi") {
      fs.writeFileSync(userPromptFile, "Current task");
      fs.writeFileSync(systemPromptFile, "Current system instructions");
    }
    const artifactClient = {
      downloadArtifact: vi.fn(async (id, { path: destination }) => {
        if (id === 555) {
          fs.mkdirSync(destination, { recursive: true });
          fs.writeFileSync(path.join(destination, "continuation-request.json"), JSON.stringify(options.request));
          return;
        }
        if (id === 777) {
          fs.mkdirSync(destination, { recursive: true });
          fs.writeFileSync(path.join(destination, "dispatched.json"), JSON.stringify(await options.readDispatchEvidence()));
          return;
        }
        fs.cpSync(saved, destination, { recursive: true });
      }),
      uploadArtifact: vi.fn(async () => ({ id: 888 })),
    };
    options.context.payload = continuationPayload();
    return { ...options, root, session, promptFile, userPromptFile, systemPromptFile, saved, artifactClient, token: "test-download-token" };
  }

  it("starts fresh without a download, file restore, or prompt rewrite", async () => {
    const options = restoreFixture();
    options.context.payload = {};
    await restoreCheckpoint(options);
    expect(options.artifactClient.downloadArtifact).not.toHaveBeenCalled();
    expect(fs.existsSync(options.session)).toBe(false);
    expect(fs.readFileSync(options.promptFile, "utf8")).toBe("Current task");
    expect(options.core.setOutput).not.toHaveBeenCalledWith("restored", "true");
    const record = JSON.parse(fs.readFileSync(path.join(options.root, "durable-session.jsonl"), "utf8"));
    expect(record).toMatchObject({ type: "durable.restore", data: { status: "fresh", runId: "200" } });
  });

  it.each(["copilot", "claude", "codex", "gemini", "agy", "pi", "imported-engine"])("downloads and validates %s state before enabling continuation", async engine => {
    const options = restoreFixture(engine);
    await restoreCheckpoint(options);
    expect(options.artifactClient.downloadArtifact).toHaveBeenCalledWith(555, expect.objectContaining({ path: path.join(options.root, "durable/request") }));
    expect(fs.existsSync(path.join(options.root, "durable/request"))).toBe(false);
    expect(options.artifactClient.downloadArtifact).toHaveBeenCalledWith(999, {
      path: path.join(options.root, "durable/bundle"),
      findBy: { token: options.token, repositoryOwner: "owner", repositoryName: "repo" },
    });
    expect(fs.readFileSync(options.session, "utf8")).toBe("previous task state");
    expect(options.core.setOutput).toHaveBeenCalledWith("parent-run-id", "100");
    expect(options.core.setOutput).toHaveBeenLastCalledWith("restored", "true");
    const record = JSON.parse(fs.readFileSync(path.join(options.root, "durable-session.jsonl"), "utf8"));
    expect(record).toMatchObject({ type: "durable.restore", data: { status: "restored", parentRunId: "100", artifactId: "999", relayRunId: "300" } });
    expect(JSON.stringify(record)).not.toContain("previous task state");
    const prompt = fs.readFileSync(engine === "pi" ? options.userPromptFile : options.promptFile, "utf8");
    if (engine === "imported-engine") expect(prompt).toBe("Current task");
    else {
      expect(prompt).toContain("untrusted historical data");
      expect(prompt).toContain("Resume at step 3");
      expect(prompt.endsWith("Current task")).toBe(true);
    }
    if (engine === "pi") {
      expect(fs.readFileSync(options.promptFile, "utf8")).toBe("Current task");
      expect(fs.readFileSync(options.systemPromptFile, "utf8")).toBe("Current system instructions");
    }
  });

  it("passes continuation hints to a fresh session when the parent published no checkpoint", async () => {
    const options = restoreFixture("copilot");
    options.github.paginate.mockImplementation(async (_method, { run_id }) => (String(run_id) === "300" ? options.state.relayArtifacts : [{ id: 555, name: "continuation-request-1", expired: false, expires_at: "2999-01-01T00:00:00Z" }]));
    await restoreCheckpoint(options);
    expect(options.artifactClient.downloadArtifact).not.toHaveBeenCalledWith(999, expect.anything());
    expect(options.core.setOutput).not.toHaveBeenCalledWith("restored", "true");
    const prompt = fs.readFileSync(options.promptFile, "utf8");
    expect(prompt).toContain("Resume at step 3");
    expect(prompt).toContain("Reconstruct");
    expect(prompt).not.toContain("untrusted historical data");
    expect(prompt.endsWith("Current task")).toBe(true);
  });

  it("uses an explicitly configured Pi user prompt without changing the raw or system prompt", async () => {
    const options = restoreFixture("pi");
    const userPromptFile = path.join(options.root, "custom-user.txt");
    fs.writeFileSync(userPromptFile, "Current split workflow task");
    vi.stubEnv("GH_AW_PI_USER_PROMPT", userPromptFile);
    await restoreCheckpoint(options);
    const prompt = fs.readFileSync(userPromptFile, "utf8");
    expect(prompt).toContain("untrusted historical data");
    expect(prompt).toContain("The harness resumes your exact native session");
    expect(prompt.endsWith("Current split workflow task")).toBe(true);
    expect(fs.readFileSync(options.userPromptFile, "utf8")).toBe("Current task");
    expect(fs.readFileSync(options.promptFile, "utf8")).toBe("Current task");
    expect(fs.readFileSync(options.systemPromptFile, "utf8")).toBe("Current system instructions");
  });

  it("does not rewrite a first-run Pi split prompt when no continuation was requested", async () => {
    const options = restoreFixture("pi");
    options.context.payload = {};
    await restoreCheckpoint(options);
    expect(fs.readFileSync(options.userPromptFile, "utf8")).toBe("Current task");
    expect(fs.readFileSync(options.systemPromptFile, "utf8")).toBe("Current system instructions");
    expect(options.core.setOutput).not.toHaveBeenCalledWith("restored", "true");
  });

  it("fails closed when Pi's consumed split prompt is missing, without rewriting the raw prompt", async () => {
    const options = restoreFixture("pi");
    fs.unlinkSync(options.userPromptFile);
    await expect(restoreCheckpoint(options)).rejects.toThrow("ENOENT");
    expect(fs.readFileSync(options.promptFile, "utf8")).toBe("Current task");
    expect(options.core.setOutput).not.toHaveBeenCalledWith("restored", "true");
  });

  it("fails without restoring files or enabling continuation when download authorization fails", async () => {
    const options = restoreFixture();
    const download = options.artifactClient.downloadArtifact.getMockImplementation();
    options.artifactClient.downloadArtifact.mockImplementation(async (id, args) => {
      if (id === 999) throw new Error("403 forbidden");
      return download(id, args);
    });
    await expect(restoreCheckpoint(options)).rejects.toThrow("403 forbidden");
    expect(JSON.parse(fs.readFileSync(path.join(options.root, "durable-session.jsonl"), "utf8")).data.status).toBe("failed");
    expect(fs.existsSync(options.session)).toBe(false);
    expect(fs.readFileSync(options.promptFile, "utf8")).toBe("Current task");
    expect(options.core.setOutput).not.toHaveBeenCalledWith("restored", "true");
  });

  it.each(["fresh", "restored", "failed"])("keeps the %s restore outcome when metadata I/O fails", async status => {
    const options = restoreFixture();
    const target = path.join(options.root, "durable-session.jsonl");
    fs.mkdirSync(target);
    const error = new Error("original download error");
    if (status === "fresh") options.context.payload = {};
    if (status === "failed") {
      const download = options.artifactClient.downloadArtifact.getMockImplementation();
      options.artifactClient.downloadArtifact.mockImplementation(async (id, args) => {
        if (id === 999) throw error;
        return download(id, args);
      });
    }
    if (status === "failed") await expect(restoreCheckpoint(options)).rejects.toBe(error);
    else await expect(restoreCheckpoint(options)).resolves.toBeUndefined();
    expect(options.core.warning).toHaveBeenCalledTimes(1);
    expect(options.core.warning).toHaveBeenCalledWith("Durable session metadata could not be recorded; continuation evidence is unavailable.");
    expect(fs.existsSync(options.session)).toBe(status === "restored");
    if (status === "restored") expect(options.core.setOutput).toHaveBeenCalledWith("restored", "true");
    else expect(options.core.setOutput).not.toHaveBeenCalledWith("restored", "true");
  });

  it("does not fail a successful restore when recording metadata is invalid", async () => {
    const options = restoreFixture("invalid/engine");
    await expect(restoreCheckpoint(options)).resolves.toBeUndefined();
    expect(options.core.setOutput).toHaveBeenCalledWith("restored", "true");
    expect(fs.existsSync(options.session)).toBe(true);
    expect(fs.existsSync(path.join(options.root, "durable-session.jsonl"))).toBe(false);
    expect(options.core.warning).toHaveBeenCalledTimes(1);
  });

  it("rejects damaged downloaded state before touching the prompt or enabling continuation", async () => {
    const options = restoreFixture();
    const blob = fs.readdirSync(options.saved).find(file => file !== "manifest.json");
    fs.writeFileSync(path.join(options.saved, blob), "corrupt state");
    await expect(restoreCheckpoint(options)).rejects.toThrow("damaged or redacted");
    expect(fs.existsSync(options.session)).toBe(false);
    expect(fs.readFileSync(options.promptFile, "utf8")).toBe("Current task");
    expect(options.core.setOutput).not.toHaveBeenCalledWith("restored", "true");
  });

  it("does not enable continuation when preparing the current prompt fails", async () => {
    const options = restoreFixture();
    fs.unlinkSync(options.promptFile);
    await expect(restoreCheckpoint(options)).rejects.toThrow("ENOENT");
    expect(options.core.setOutput).not.toHaveBeenCalledWith("restored", "true");
  });
});
