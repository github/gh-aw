// @ts-check
"use strict";

import { afterEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { selectCheckpoint, restoreCheckpoint, validateCheckpoint } = require("./durable_artifact.cjs");
const { stageFiles } = require("./durable_files.cjs");
const { SOURCE, VERSION } = require("./pi_durable_checkpoint.cjs");
const directories = [];

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

function fixture() {
  vi.stubEnv("GH_AW_DURABLE_WORKFLOW", "continuable");
  vi.stubEnv("GITHUB_RUN_ATTEMPT", "1");
  const current = { id: 200, workflow_id: 42, run_number: 20, head_branch: "feature", repository: { id: 1 }, head_repository: { id: 1 }, status: "in_progress", event: "workflow_dispatch" };
  const parent = { ...structuredClone(current), id: 100, run_number: 19, status: "completed", conclusion: "success" };
  const core = { setOutput: vi.fn(), info: vi.fn(), warning: vi.fn() };
  const github = {
    rest: {
      actions: {
        getWorkflowRun: vi.fn(async ({ run_id }) => ({ data: String(run_id) === "200" ? current : parent })),
        listWorkflowRuns: vi.fn(async () => ({ data: { workflow_runs: [parent] } })),
        listWorkflowRunArtifacts: vi.fn(),
      },
    },
    paginate: vi.fn(async () => {
      const name = core.setOutput.mock.calls.find(([key]) => key === "artifact-name")[1].replace(/200-\d+$/, "100-1");
      return [{ id: 999, name, expired: false, expires_at: "2999-01-01T00:00:00Z" }];
    }),
  };
  const context = { repo: { owner: "owner", repo: "repo" }, runId: 200, eventName: "workflow_dispatch", ref: "refs/heads/feature", payload: {} };
  return { github, context, core, current, parent };
}

describe("durable artifact lineage", () => {
  it("starts the first run fresh without attempting to download nonexistent history", async () => {
    const options = fixture();
    options.github.rest.actions.listWorkflowRuns.mockResolvedValue({ data: { workflow_runs: [] } });
    await selectCheckpoint(options);
    expect(options.github.paginate).not.toHaveBeenCalled();
    expect(options.github.rest.actions.listWorkflowRunArtifacts).not.toHaveBeenCalled();
    expect(options.core.setOutput.mock.calls.some(([key]) => key === "artifact-id")).toBe(false);
  });

  it("uses the last successful run of the same workflow and branch, without event filtering", async () => {
    const options = fixture();
    options.parent.event = "schedule";
    await selectCheckpoint(options);
    expect(options.github.rest.actions.listWorkflowRuns).toHaveBeenCalledWith(expect.objectContaining({ workflow_id: 42, branch: "feature", status: "success" }));
    expect(options.github.rest.actions.listWorkflowRuns.mock.calls[0][0]).not.toHaveProperty("event");
    expect(options.core.setOutput).toHaveBeenCalledWith("artifact-id", "999");
    expect(options.core.setOutput).toHaveBeenCalledWith("parent-run-id", "100");
  });

  it("prefers the immediate aw_context caller, including a failed parent, over successful-run lookup", async () => {
    const options = fixture();
    options.parent.conclusion = "failure";
    options.context.payload = { inputs: { aw_context: JSON.stringify({ run_id: "100", parent_run_id: "50" }) } };
    await selectCheckpoint(options);
    expect(options.github.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
    expect(options.github.rest.actions.getWorkflowRun).toHaveBeenLastCalledWith(expect.objectContaining({ run_id: "100" }));
    expect(options.core.setOutput).toHaveBeenCalledWith("run-id", "100");
  });

  it("restores the latest published earlier attempt before reusing the inbound parent", async () => {
    const options = fixture();
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "3");
    options.context.payload = { inputs: { aw_context: '{"run_id":"100"}' } };
    options.github.paginate.mockImplementation(async (_method, { run_id }) => {
      const name = options.core.setOutput.mock.calls.find(([key]) => key === "artifact-name")[1];
      return String(run_id) === "200" ? [1, 2, 3].map(attempt => ({ id: 900 + attempt, name: name.replace(/200-3$/, `200-${attempt}`), expired: false, expires_at: "2999-01-01T00:00:00Z" })) : [];
    });
    await selectCheckpoint(options);
    expect(options.core.setOutput).toHaveBeenCalledWith("artifact-id", "902");
    expect(options.core.setOutput).toHaveBeenCalledWith("run-id", "200");
    expect(options.github.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
  });

  it("falls back to the selected parent when an earlier attempt published no checkpoint", async () => {
    const options = fixture();
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
    const parentArtifacts = options.github.paginate.getMockImplementation();
    options.github.paginate.mockImplementation(async (method, args) => (String(args.run_id) === "200" ? [] : parentArtifacts(method, args)));
    await selectCheckpoint(options);
    expect(options.core.setOutput).toHaveBeenCalledWith("run-id", "100");
  });

  it("accepts explicit parent_run_id from repository_dispatch context", async () => {
    const options = fixture();
    options.context.payload = { client_payload: { aw_context: { parent_run_id: "100" } } };
    await selectCheckpoint(options);
    expect(options.github.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
    expect(options.core.setOutput).toHaveBeenCalledWith("run-id", "100");
  });

  it.each(["branch", "workflow", "fork", "running"])("rejects an explicit %s-incompatible parent", async kind => {
    const options = fixture();
    options.context.payload = { inputs: { aw_context: '{"run_id":"100"}' } };
    if (kind === "branch") options.parent.head_branch = "main";
    if (kind === "workflow") options.parent.workflow_id = 43;
    if (kind === "fork") options.parent.head_repository.id = 2;
    if (kind === "running") options.parent.status = "in_progress";
    await expect(selectCheckpoint(options)).rejects.toThrow("same workflow and branch");
    expect(options.github.paginate).not.toHaveBeenCalled();
  });

  it("fails rather than switching parents when an explicit checkpoint expired", async () => {
    const options = fixture();
    options.context.payload = { inputs: { aw_context: '{"run_id":"100"}' } };
    options.github.paginate.mockResolvedValue([]);
    await expect(selectCheckpoint(options)).rejects.toThrow("no unexpired checkpoint");
    expect(options.github.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
  });

  it("starts fresh when the last successful run has no artifact, without using older runs", async () => {
    const options = fixture();
    options.github.paginate.mockResolvedValue([]);
    await selectCheckpoint(options);
    expect(options.github.paginate).toHaveBeenCalledTimes(1);
    expect(options.core.setOutput.mock.calls.some(([key]) => key === "artifact-id")).toBe(false);
  });

  it("surfaces API errors instead of silently starting a new session", async () => {
    const options = fixture();
    options.github.rest.actions.listWorkflowRuns.mockRejectedValue(new Error("403 forbidden"));
    await expect(selectCheckpoint(options)).rejects.toThrow("403 forbidden");
  });

  it("rejects malformed inbound context rather than silently selecting another parent", async () => {
    const options = fixture();
    options.context.payload = { inputs: { aw_context: "{invalid" } };
    await expect(selectCheckpoint(options)).rejects.toThrow("valid aw_context");
    expect(options.github.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
  });

  it("never exposes a trusted checkpoint to a fork run with the same branch name", async () => {
    const options = fixture();
    options.current.head_repository.id = 2;
    await expect(selectCheckpoint(options)).rejects.toThrow("fork workflow runs");
    expect(options.github.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
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
    const artifactClient = {
      downloadArtifact: vi.fn(async (_id, { path: destination }) => fs.cpSync(saved, destination, { recursive: true })),
    };
    return { ...options, root, session, promptFile, saved, artifactClient, token: "test-download-token" };
  }

  it("starts fresh without a download, file restore, or prompt rewrite", async () => {
    const options = restoreFixture();
    options.github.rest.actions.listWorkflowRuns.mockResolvedValue({ data: { workflow_runs: [] } });
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
    expect(options.artifactClient.downloadArtifact).toHaveBeenCalledWith(999, {
      path: path.join(options.root, "durable/bundle"),
      findBy: { token: options.token, repositoryOwner: "owner", repositoryName: "repo" },
    });
    expect(fs.readFileSync(options.session, "utf8")).toBe("previous task state");
    expect(options.core.setOutput).toHaveBeenCalledWith("parent-run-id", "100");
    expect(options.core.setOutput).toHaveBeenLastCalledWith("restored", "true");
    const record = JSON.parse(fs.readFileSync(path.join(options.root, "durable-session.jsonl"), "utf8"));
    expect(record).toMatchObject({ type: "durable.restore", data: { status: "restored", parentRunId: "100", artifactId: "999" } });
    expect(JSON.stringify(record)).not.toContain("previous task state");
    const prompt = fs.readFileSync(options.promptFile, "utf8");
    if (engine === "pi" || engine === "imported-engine") expect(prompt).toBe("Current task");
    else {
      expect(prompt).toContain("untrusted historical data");
      expect(prompt.endsWith("Current task")).toBe(true);
    }
  });

  it("fails without restoring files or enabling continuation when download authorization fails", async () => {
    const options = restoreFixture();
    options.artifactClient.downloadArtifact.mockRejectedValue(new Error("403 forbidden"));
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
    if (status === "fresh") options.github.rest.actions.listWorkflowRuns.mockResolvedValue({ data: { workflow_runs: [] } });
    if (status === "failed") options.artifactClient.downloadArtifact.mockRejectedValue(error);
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

describe("durable artifact publication", () => {
  function checkpointFixture() {
    vi.stubEnv("GH_AW_DURABLE_CHECKPOINT", "/tmp/gh-aw/agent/durable/pi-durable-continuable.json");
    vi.stubEnv("GH_AW_DURABLE_ENGINE", "pi");
    const content = '{"seq":1}\n';
    const checkpoint = {
      source: SOURCE,
      packageVersion: VERSION,
      contractVersion: 1,
      scope: "pi-durable-continuable",
      storage: "artifact",
      files: { "main.jsonl": content },
      hashes: { "main.jsonl": createHash("sha256").update(content).digest("hex") },
    };
    vi.spyOn(fs, "existsSync").mockReturnValue(true);
    vi.spyOn(fs, "lstatSync").mockReturnValue({ isFile: () => true, isSymbolicLink: () => false });
    vi.spyOn(fs, "readFileSync").mockImplementation(() => JSON.stringify(checkpoint));
    const core = { setOutput: vi.fn(), info: vi.fn() };
    return { checkpoint, core };
  }

  it("publishes only a valid complete checkpoint", () => {
    const { core } = checkpointFixture();
    validateCheckpoint({ core });
    expect(core.setOutput).toHaveBeenCalledWith("present", "true");
  });

  it("rejects a checkpoint modified by secret redaction", () => {
    const { checkpoint, core } = checkpointFixture();
    checkpoint.files["main.jsonl"] = '{"seq":"***"}\n';
    expect(() => validateCheckpoint({ core })).toThrow("damaged or redacted");
    expect(core.setOutput).not.toHaveBeenCalled();
  });

  it("rejects symlink checkpoints", () => {
    const { core } = checkpointFixture();
    fs.lstatSync.mockReturnValue({ isFile: () => false, isSymbolicLink: () => true });
    expect(() => validateCheckpoint({ core })).toThrow("regular files");
  });
});
