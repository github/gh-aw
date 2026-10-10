// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import os from "os";
import path from "path";
import fs from "fs";
import vm from "node:vm";
import { createRequire } from "node:module";

// Globals required by push_experiment_state.cjs and its dependencies
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  debug: vi.fn(),
};

const mockExec = {
  getExecOutput: vi.fn(),
};

const mockContext = {
  repo: { owner: "testowner", repo: "testrepo" },
};

global.core = mockCore;
global.exec = mockExec;
global.context = mockContext;
global.github = {};

const { main, mergeExperimentStateJSON, mergeExperimentStateJSONL, mergeAppendOnlyJSONL, mergeExperimentRuns, checkoutOrCreateBranch } = await import("./push_experiment_state.cjs");

describe("push_experiment_state", () => {
  let tmpDir;

  beforeEach(() => {
    vi.clearAllMocks();
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "push-exp-test-"));
    process.env.GITHUB_WORKSPACE = tmpDir;
    process.env.GITHUB_REPOSITORY = "testowner/testrepo";
  });

  afterEach(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
    delete process.env.GH_AW_EXPERIMENT_STATE_DIR;
    delete process.env.GH_AW_EXPERIMENT_BRANCH;
    delete process.env.GH_TOKEN;
    delete process.env.GITHUB_WORKSPACE;
    delete process.env.GITHUB_REPOSITORY;
    delete process.env.GH_AW_ALLOWED_TARGET_REPOS;
  });

  it("calls setFailed when GH_AW_EXPERIMENT_BRANCH is not set", async () => {
    process.env.GH_TOKEN = "ghp_test";
    delete process.env.GH_AW_EXPERIMENT_BRANCH;

    await main();

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_EXPERIMENT_BRANCH"));
  });

  it("calls setFailed when GH_TOKEN is not set", async () => {
    process.env.GH_AW_EXPERIMENT_BRANCH = "experiments/myworkflow";
    delete process.env.GH_TOKEN;
    delete process.env.GITHUB_TOKEN;

    await main();

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_TOKEN"));
  });

  it("logs info and returns when no state files are present in state dir", async () => {
    process.env.GH_TOKEN = "ghp_test";
    process.env.GH_AW_EXPERIMENT_BRANCH = "experiments/myworkflow";
    process.env.GH_AW_EXPERIMENT_STATE_DIR = tmpDir;
    // tmpDir exists but is empty — no state.json or assignments.json

    await main();

    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No experiment state files found"));
  });

  it("calls setFailed when target repository is not in allowedRepos allowlist", async () => {
    process.env.GH_TOKEN = "ghp_test";
    process.env.GH_AW_EXPERIMENT_BRANCH = "experiments/myworkflow";
    process.env.GH_AW_ALLOWED_TARGET_REPOS = "someowner/somerepo";

    await main();

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_ALLOWED_TARGET_REPOS"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("testowner/testrepo"));
  });

  it("does not fail when target repository is included in GH_AW_ALLOWED_TARGET_REPOS", async () => {
    process.env.GH_TOKEN = "ghp_test";
    process.env.GH_AW_EXPERIMENT_BRANCH = "experiments/myworkflow";
    process.env.GH_AW_EXPERIMENT_STATE_DIR = tmpDir;
    process.env.GH_AW_ALLOWED_TARGET_REPOS = "other/repo, testowner/testrepo";

    await main();

    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No experiment state files found"));
  });

  it("reports every unreadable state file before failing", async () => {
    process.env.GH_TOKEN = "ghp_test";
    process.env.GH_AW_EXPERIMENT_BRANCH = "experiments/myworkflow";
    process.env.GH_AW_EXPERIMENT_STATE_DIR = tmpDir;

    const stateFile = path.join(tmpDir, "state.json");
    const assignmentsFile = path.join(tmpDir, "assignments.json");
    fs.writeFileSync(stateFile, "{}");
    fs.writeFileSync(assignmentsFile, "{}");

    const originalStatSync = fs.statSync;
    const statSpy = vi.spyOn(fs, "statSync").mockImplementation(targetPath => {
      if (targetPath === stateFile || targetPath === assignmentsFile) {
        throw new Error(`cannot inspect ${path.basename(targetPath)}`);
      }
      return originalStatSync.call(fs, targetPath);
    });

    await main();

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(`Failed to inspect experiment state files:\n${stateFile}: cannot inspect state.json\n${assignmentsFile}: cannot inspect assignments.json`));
    expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("Files to push:"));

    statSpy.mockRestore();
  });

  it.each([false, true])("authenticates retry fetches and advances the base only after a successful fetch (fetch fails: %s)", async fetchFails => {
    const baseRef = "a".repeat(40);
    const remoteHead = "b".repeat(40);
    const gitEnv = { GIT_CONFIG_COUNT: "1", GIT_CONFIG_KEY_0: "http.https://github.com/.extraheader", GIT_CONFIG_VALUE_0: "masked-auth-header" };
    const execGitSync = vi.fn(args => {
      if (args[0] === "status") return "M state.json";
      if (args[0] === "rev-parse") return baseRef;
      if (fetchFails && args[0] === "fetch" && args[1] === "origin") throw new Error("temporary fetch failure");
      return "";
    });
    const pushSignedCommits = vi.fn().mockRejectedValueOnce(new Error("non-fast-forward")).mockResolvedValue(undefined);
    const exec = { getExecOutput: vi.fn().mockResolvedValue({ stdout: `${remoteHead}\trefs/heads/evals/test` }) };
    const require = createRequire(import.meta.url);
    const module = { exports: {} };
    const workspaceDir = path.join(tmpDir, "workspace");
    fs.mkdirSync(workspaceDir);
    fs.writeFileSync(path.join(tmpDir, "state.json"), "{}");
    const mocks = {
      "./git_helpers.cjs": { execGitSync, withGitRetry: async fn => fn() },
      "./git_auth_helpers.cjs": { getGitAuthEnv: () => gitEnv },
      "./push_signed_commits.cjs": { pushSignedCommits },
    };
    vm.runInNewContext(fs.readFileSync(path.join(import.meta.dirname, "push_experiment_state.cjs"), "utf8"), {
      require: id => mocks[id] || require(id),
      module,
      core: mockCore,
      exec,
      context: mockContext,
      github: {},
      setTimeout: fn => fn(),
      process: { env: { GH_AW_STATE_BRANCH: "evals/test", GH_AW_STATE_DIR: tmpDir, GITHUB_WORKSPACE: workspaceDir, GH_TOKEN: "test-token" } },
    });
    await module.exports.main();
    expect(exec.getExecOutput).toHaveBeenCalledWith("git", ["ls-remote", "origin", "refs/heads/evals/test"], { cwd: workspaceDir, env: expect.objectContaining(gitEnv) });
    expect(execGitSync).toHaveBeenCalledWith(["fetch", "origin", "refs/heads/evals/test"], expect.objectContaining({ env: expect.objectContaining(gitEnv) }));
    expect(pushSignedCommits).toHaveBeenCalledTimes(2);
    expect(pushSignedCommits.mock.calls[1][0].baseRef).toBe(fetchFails ? baseRef : remoteHead);
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("merges concurrent experiment state updates without losing same-variant increments", () => {
    const baseState = {
      counts: { prompt_style: { concise: 1, detailed: 1 } },
      runs: [{ run_id: "100", timestamp: "2026-07-31T12:00:00.000Z", assignments: { prompt_style: "concise" } }],
    };
    const remoteState = {
      counts: { prompt_style: { concise: 2, detailed: 1 } },
      runs: [...baseState.runs, { run_id: "200", timestamp: "2026-07-31T12:01:00.000Z", assignments: { prompt_style: "concise" } }],
    };
    const localState = {
      counts: { prompt_style: { concise: 2, detailed: 1 } },
      runs: [...baseState.runs, { run_id: "300", timestamp: "2026-07-31T12:02:00.000Z", assignments: { prompt_style: "concise" } }],
    };

    const mergedState = mergeExperimentStateJSON(baseState, remoteState, localState);

    expect(mergedState.counts.prompt_style.concise).toBe(3);
    expect(mergedState.counts.prompt_style.detailed).toBe(1);
    expect(mergedState.runs).toHaveLength(3);
    expect(mergedState.runs.map(run => run.run_id)).toEqual(["100", "200", "300"]);
  });

  it("merges concurrent continual stages without adding them", () => {
    const baseState = { counts: {}, continual: { optimization: { current_stage: 1 } } };
    const remoteState = { counts: {}, continual: { optimization: { current_stage: 2 } } };
    const localState = { counts: {}, continual: { optimization: { current_stage: 2 } } };

    expect(mergeExperimentStateJSON(baseState, remoteState, localState).continual).toEqual({
      optimization: { current_stage: 2 },
    });
  });

  it("mergeExperimentRuns returns runs in timestamp order regardless of input order", () => {
    const remote = [
      { run_id: "R1", timestamp: "2026-08-01T12:02:00.000Z", assignments: { f: "A" } },
      { run_id: "R2", timestamp: "2026-08-01T11:59:00.000Z", assignments: { f: "B" } },
    ];
    const local = [{ run_id: "L1", timestamp: "2026-08-01T12:01:00.000Z", assignments: { f: "A" } }];
    const merged = mergeExperimentRuns(remote, local);
    expect(merged.map(r => r.run_id)).toEqual(["R2", "L1", "R1"]);
  });

  it("mergeExperimentRuns deduplicates identical runs", () => {
    const run = { run_id: "X1", timestamp: "2026-08-01T12:00:00.000Z", assignments: { f: "A" } };
    const merged = mergeExperimentRuns([run], [run]);
    expect(merged).toHaveLength(1);
    expect(merged[0].run_id).toBe("X1");
  });

  it("mergeExperimentStateJSONL returns entries sorted by timestamp", () => {
    const remote = ['{"run_id":"R1","timestamp":"2026-08-01T12:02:00.000Z","assignments":{"f":"A"}}', '{"run_id":"R2","timestamp":"2026-08-01T11:58:00.000Z","assignments":{"f":"B"}}'].join("\n") + "\n";
    const local = '{"run_id":"L1","timestamp":"2026-08-01T12:01:00.000Z","assignments":{"f":"A"}}\n';

    const result = mergeExperimentStateJSONL(remote, local);
    const entries = result
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(entries.map(e => e.run_id)).toEqual(["R2", "L1", "R1"]);
  });

  it("mergeExperimentStateJSONL compacts ledger and preserves counts via baseline_counts", () => {
    // Build 514 run records (over MAX_LEDGER_RECORDS = 512).
    const baseMs = Date.UTC(2026, 0, 1);
    const makeRun = i => ({
      run_id: `run-${String(i).padStart(4, "0")}`,
      timestamp: new Date(baseMs + i * 60000).toISOString(), // 1 min apart, all unique
      assignments: { f: i % 2 === 0 ? "A" : "B" },
    });

    const remoteRuns = Array.from({ length: 513 }, (_, i) => makeRun(i));
    const localRun = makeRun(513);

    const remote = remoteRuns.map(r => JSON.stringify(r)).join("\n") + "\n";
    const local = JSON.stringify(localRun) + "\n";

    const result = mergeExperimentStateJSONL(remote, local);
    const entries = result
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));

    expect(entries).toHaveLength(512);
    // First remaining entry should have baseline_counts accumulating pruned records.
    // Pruned are run-0000 (f:A) and run-0001 (f:B).
    expect(entries[0].baseline_counts).toBeDefined();
    expect(entries[0].baseline_counts.f.A).toBeGreaterThan(0);
    expect(entries[0].baseline_counts.f.B).toBeGreaterThan(0);
  });

  it("mergeExperimentStateJSONL deduplicates and handles empty inputs", () => {
    const line = '{"run_id":"X1","timestamp":"2026-08-01T12:00:00.000Z","assignments":{"f":"A"}}\n';
    const result = mergeExperimentStateJSONL(line, line);
    const entries = result
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(entries).toHaveLength(1);
    expect(entries[0].run_id).toBe("X1");
  });

  it("mergeExperimentStateJSONL skips unparseable lines and emits a warning", () => {
    const valid = '{"run_id":"V1","timestamp":"2026-08-01T12:00:00.000Z","assignments":{"f":"A"}}\n';
    // Simulate a partially-written or corrupted line in the remote content.
    const corrupt = "not-valid-json\n" + valid;
    const result = mergeExperimentStateJSONL(corrupt, "");
    const entries = result
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(entries).toHaveLength(1);
    expect(entries[0].run_id).toBe("V1");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("skipping unparseable line"));
  });

  it("mergeAppendOnlyJSONL appends new eval entries without pruning existing history", () => {
    const existingEntries = Array.from({ length: 513 }, (_, i) => ({
      id: `existing-${i}`,
      timestamp: new Date(Date.UTC(2026, 0, 1) + i * 60000).toISOString(),
      runid: String(i),
    }));
    const newEntry = {
      id: "new",
      timestamp: "2026-08-01T12:00:00.000Z",
      runid: "new-run",
    };

    const result = mergeAppendOnlyJSONL(existingEntries.map(entry => JSON.stringify(entry)).join("\n") + "\n", `${JSON.stringify(newEntry)}\n`);
    const entries = result
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));

    expect(entries).toHaveLength(514);
    expect(entries[0]).toEqual(existingEntries[0]);
    expect(entries.at(-1)).toEqual(newEntry);
  });

  it("mergeAppendOnlyJSONL deduplicates entries during concurrent update merges", () => {
    const shared = '{"id":"shared","timestamp":"2026-08-01T12:00:00.000Z","runid":"1"}\n';
    const remote = shared + '{"id":"remote","timestamp":"2026-08-01T12:01:00.000Z","runid":"2"}\n';
    const local = shared + '{"id":"local","timestamp":"2026-08-01T12:02:00.000Z","runid":"3"}\n';

    const result = mergeAppendOnlyJSONL(remote, local)
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));

    expect(result.map(entry => entry.id)).toEqual(["shared", "remote", "local"]);
  });

  it("mergeAppendOnlyJSONL preserves opaque malformed lines", () => {
    const malformed = "not-valid-json\n";
    const remote = malformed + '{"id":"remote","timestamp":"2026-08-01T12:01:00.000Z","runid":"2"}\n';
    const local = malformed + '{"id":"local","timestamp":"2026-08-01T12:02:00.000Z","runid":"3"}\n';

    const result = mergeAppendOnlyJSONL(remote, local).trim().split("\n");

    expect(result[0]).toBe("not-valid-json");
    expect(result[1]).toBe('{"id":"remote","timestamp":"2026-08-01T12:01:00.000Z","runid":"2"}');
    expect(result[2]).toBe('{"id":"local","timestamp":"2026-08-01T12:02:00.000Z","runid":"3"}');
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("preserving unparseable line"));
  });

  describe("checkoutOrCreateBranch", () => {
    let repoDir;

    beforeEach(() => {
      const { spawnSync } = require("child_process");
      repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "push-exp-repo-"));
      spawnSync("git", ["init", "-b", "main"], { cwd: repoDir });
      spawnSync("git", ["config", "user.email", "test@example.com"], { cwd: repoDir });
      spawnSync("git", ["config", "user.name", "Test"], { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "seed.txt"), "seed\n");
      spawnSync("git", ["add", "."], { cwd: repoDir });
      spawnSync("git", ["commit", "-m", "seed"], { cwd: repoDir });
    });

    afterEach(() => {
      fs.rmSync(repoDir, { recursive: true, force: true });
    });

    it("retries only the fetch on transient failures and then checks out", async () => {
      const fetchFn = vi
        .fn()
        .mockImplementationOnce(() => {
          throw new Error("Git command timed out: git fetch ...");
        })
        .mockImplementationOnce(() => {
          throw new Error("error: RPC failed; HTTP 502 curl 22 The requested URL returned error: 502");
        })
        .mockImplementation(() => {
          // Simulate a successful fetch creating the local branch ref.
          require("child_process").spawnSync("git", ["branch", "evals/myworkflow"], { cwd: repoDir });
        });

      const result = await checkoutOrCreateBranch("evals/myworkflow", "https://x@github.com/o/r.git", repoDir, {
        fetchFn,
        baseDelayMs: 0,
      });

      expect(fetchFn).toHaveBeenCalledTimes(3);
      expect(mockCore.warning).toHaveBeenCalledTimes(2);
      expect(result).toMatch(/^[0-9a-f]{40}$/);
    });

    it("does not retry deterministic fetch failures", async () => {
      const fetchFn = vi.fn().mockImplementation(() => {
        throw new Error("fatal: Authentication failed for 'https://github.com/o/r.git/'");
      });

      await expect(
        checkoutOrCreateBranch("evals/myworkflow", "https://x@github.com/o/r.git", repoDir, {
          fetchFn,
          baseDelayMs: 0,
        })
      ).rejects.toThrow("Authentication failed");

      expect(fetchFn).toHaveBeenCalledTimes(1);
      expect(mockCore.warning).not.toHaveBeenCalled();
    });

    it("does not retry the orphan-branch path when the remote ref is missing", async () => {
      const fetchFn = vi.fn().mockImplementation(() => {
        throw new Error("fatal: couldn't find remote ref evals/myworkflow");
      });

      const result = await checkoutOrCreateBranch("evals/myworkflow", "https://x@github.com/o/r.git", repoDir, {
        fetchFn,
        baseDelayMs: 0,
      });

      expect(fetchFn).toHaveBeenCalledTimes(1);
      expect(result).toBe("");
    });
  });
});
