// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

/** Environment variables managed by tests */
const TEST_ENV_VARS = ["GH_AW_RUN_URL", "GH_TOKEN", "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", "GH_AW_AGENT_OUTPUT"];

describe("apply_safe_outputs_replay", () => {
  let originalEnv;
  let originalGlobals;

  beforeEach(() => {
    originalEnv = { ...process.env };

    originalGlobals = {
      core: global.core,
      github: global.github,
      context: global.context,
      exec: global.exec,
    };

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
    };

    global.github = {
      rest: {
        actions: {
          getWorkflowRun: vi.fn().mockResolvedValue({ data: { status: "completed", run_attempt: 1, head_sha: "a".repeat(40), path: ".github/workflows/test.lock.yml" } }),
          listJobsForWorkflowRunAttempt: vi.fn(),
        },
        repos: {
          getContent: vi.fn().mockResolvedValue({ data: { type: "file", encoding: "base64", content: Buffer.from('# gh-aw-manifest: {"version":1}\n').toString("base64") } }),
        },
      },
      paginate: vi.fn(),
    };

    global.context = {
      repo: {
        owner: "testowner",
        repo: "testrepo",
      },
    };

    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn(),
    };

    // Clear managed env vars
    for (const key of TEST_ENV_VARS) {
      delete process.env[key];
    }
  });

  afterEach(() => {
    for (const key of TEST_ENV_VARS) {
      if (originalEnv[key] !== undefined) {
        process.env[key] = originalEnv[key];
      } else {
        delete process.env[key];
      }
    }

    global.core = originalGlobals.core;
    global.github = originalGlobals.github;
    global.context = originalGlobals.context;
    global.exec = originalGlobals.exec;

    vi.clearAllMocks();
  });

  describe("parseRunUrl", () => {
    it("parses a plain run ID", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      const result = parseRunUrl("23560193313");
      expect(result.runId, "should parse run ID").toBe("23560193313");
      expect(result.owner, "owner should be null for plain ID").toBeNull();
      expect(result.repo, "repo should be null for plain ID").toBeNull();
    });

    it("parses a full GitHub Actions run URL", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      const result = parseRunUrl("https://github.com/github/gh-aw/actions/runs/23560193313");
      expect(result.runId, "should parse run ID from URL").toBe("23560193313");
      expect(result.owner, "should parse owner").toBe("github");
      expect(result.repo, "should parse repo").toBe("gh-aw");
    });

    it("parses a run URL that includes a job ID", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      const result = parseRunUrl("https://github.com/github/gh-aw/actions/runs/23560193313/job/68600993738");
      expect(result.runId, "should parse run ID ignoring job ID").toBe("23560193313");
      expect(result.owner, "should parse owner").toBe("github");
      expect(result.repo, "should parse repo").toBe("gh-aw");
    });

    it("trims whitespace from the input", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      const result = parseRunUrl("  23560193313  ");
      expect(result.runId, "should trim and parse run ID").toBe("23560193313");
    });

    it("throws for an empty string", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      expect(() => parseRunUrl(""), "should throw for empty string").toThrow(/run_url is required/);
    });

    it("throws for an invalid URL format", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      expect(() => parseRunUrl("not-a-valid-url"), "should throw for invalid format").toThrow(/Cannot parse run ID/);
    });

    it("throws for a URL without a run ID", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      expect(() => parseRunUrl("https://github.com/owner/repo/actions"), "should throw for URL without run ID").toThrow(/Cannot parse run ID/);
    });

    it("rejects URLs that only embed a GitHub run URL on another host", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      expect(() => parseRunUrl("https://evil.example/github.com/owner/repo/actions/runs/123")).toThrow(/Cannot parse run ID/);
    });
  });

  describe("recorded write gate authorization", () => {
    const record = () => ({
      version: 1,
      repository: "testowner/testrepo",
      run_id: "123",
      run_attempt: "1",
      head_sha: "a".repeat(40),
      safe_outputs_result: "success",
      verdict: "allow",
      reason: "approved",
    });
    const identity = { runId: "123", repository: "testowner/testrepo", attempt: 1, headSha: "a".repeat(40) };

    it("accepts only a matching successful recorded allow", async () => {
      const { validateReplayWriteGateRecord } = await import("./apply_safe_outputs_replay.cjs");
      expect(() => validateReplayWriteGateRecord(record(), identity)).not.toThrow();
      for (const verdict of ["deny", "partial", "unknown", "", "ALLOW"]) {
        expect(() => validateReplayWriteGateRecord({ ...record(), verdict }, identity)).toThrow(/successful recorded allow/);
      }
      for (const safe_outputs_result of ["failure", "cancelled", "timed_out", "skipped", ""]) {
        expect(() => validateReplayWriteGateRecord({ ...record(), safe_outputs_result }, identity)).toThrow(/successful recorded allow/);
      }
    });

    it("rejects missing, malformed, stale, or cross-repository records", async () => {
      const { validateReplayWriteGateRecord } = await import("./apply_safe_outputs_replay.cjs");
      for (const invalid of [
        null,
        [],
        {},
        { ...record(), repository: "other/repo" },
        { ...record(), run_id: "124" },
        { ...record(), run_attempt: "2" },
        { ...record(), head_sha: "b".repeat(40) },
        { ...record(), reason: null },
        { ...record(), items: [] },
      ]) {
        expect(() => validateReplayWriteGateRecord(invalid, identity)).toThrow();
      }
    });

    it("loads policy from the original immutable compiled workflow", async () => {
      const { getReplayPolicy } = await import("./apply_safe_outputs_replay.cjs");
      global.github.rest.repos.getContent.mockResolvedValue({
        data: { type: "file", encoding: "base64", content: Buffer.from('# gh-aw-manifest: {"version":1,"write_gate":{"version":1,"record_job":"conclusion"}}\n').toString("base64") },
      });
      expect((await getReplayPolicy("123", "testowner", "testrepo")).gate).toEqual({ version: 1, record_job: "conclusion" });
      expect(global.github.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ ref: "a".repeat(40), path: ".github/workflows/test.lock.yml" }));
    });

    it("refuses absent/malformed source policy and dry-run replay", async () => {
      const { getReplayPolicy } = await import("./apply_safe_outputs_replay.cjs");
      for (const source of [
        "name: missing manifest",
        '# gh-aw-manifest: {"version":1,"write_gate":null}\n',
        '# gh-aw-manifest: {"version":1,"write_gate":{"version":2,"record_job":"conclusion"}}\n',
        '# gh-aw-metadata: {"dry_run":true}\n# gh-aw-manifest: {"version":1}\n',
      ]) {
        global.github.rest.repos.getContent.mockResolvedValue({ data: { type: "file", encoding: "base64", content: Buffer.from(source).toString("base64") } });
        await expect(getReplayPolicy("123", "testowner", "testrepo")).rejects.toThrow();
      }
    });

    it("requires successful trusted recorder and upload steps before artifact download", async () => {
      const { enforceReplayWriteGate } = await import("./apply_safe_outputs_replay.cjs");
      const policy = { run: { run_attempt: 1, head_sha: "a".repeat(40) }, gate: { version: 1, record_job: "conclusion" } };
      const trustedJob = {
        name: "conclusion",
        status: "completed",
        steps: ["Record write gate verdict", "Upload write gate verdict"].map(name => ({ name, status: "completed", conclusion: "success" })),
      };
      for (const jobs of [
        [],
        [trustedJob, trustedJob],
        [{ ...trustedJob, steps: [] }],
        [{ ...trustedJob, steps: [{ ...trustedJob.steps[0], conclusion: "failure" }, trustedJob.steps[1]] }],
        [{ ...trustedJob, steps: [trustedJob.steps[0], { ...trustedJob.steps[1], conclusion: "failure" }] }],
      ]) {
        global.github.paginate.mockResolvedValue(jobs);
        await expect(enforceReplayWriteGate(policy, "123", "testowner", "testrepo")).rejects.toThrow();
        expect(global.exec.exec).not.toHaveBeenCalled();
      }
    });

    it("checks the downloaded verdict without falling back to the agent artifact", async () => {
      const fs = require("node:fs");
      const { enforceReplayWriteGate } = await import("./apply_safe_outputs_replay.cjs");
      const { TMP_GH_AW_PATH } = require("./constants.cjs");
      fs.mkdirSync(TMP_GH_AW_PATH, { recursive: true });
      global.github.paginate.mockResolvedValue([
        {
          name: "conclusion",
          status: "completed",
          steps: ["Record write gate verdict", "Upload write gate verdict"].map(name => ({ name, status: "completed", conclusion: "success" })),
        },
      ]);
      const policy = { run: { run_attempt: 1, head_sha: "a".repeat(40) }, gate: { version: 1, record_job: "conclusion" } };
      for (const value of [record(), { ...record(), verdict: "partial" }, null]) {
        global.exec.exec.mockImplementation(async (_command, args) => {
          const destination = args[args.indexOf("--dir") + 1];
          if (value) fs.writeFileSync(require("node:path").join(destination, "write_gate.json"), JSON.stringify(value));
          return 0;
        });
        if (value?.verdict === "allow") {
          await expect(enforceReplayWriteGate(policy, "123", "testowner", "testrepo")).resolves.toBeUndefined();
        } else {
          await expect(enforceReplayWriteGate(policy, "123", "testowner", "testrepo")).rejects.toThrow();
        }
      }
      expect(global.exec.exec.mock.calls.every(([, args]) => args[args.indexOf("--name") + 1] === "write-gate")).toBe(true);
    });
  });

  describe("buildHandlerConfigFromOutput", () => {
    it("builds config from agent output items", async () => {
      const fs = require("fs");
      const os = require("os");
      const path = require("path");
      const { buildHandlerConfigFromOutput } = await import("./apply_safe_outputs_replay.cjs");

      const tmpFile = path.join(os.tmpdir(), `test-agent-output-${Date.now()}.json`);
      const agentOutput = {
        items: [
          { type: "create_issue", title: "Test issue" },
          { type: "add_comment", body: "Hello" },
          { type: "create_issue", title: "Duplicate type" },
        ],
      };
      fs.writeFileSync(tmpFile, JSON.stringify(agentOutput));

      try {
        const config = buildHandlerConfigFromOutput(tmpFile);
        expect(Object.keys(config), "should include create_issue").toContain("create_issue");
        expect(Object.keys(config), "should include add_comment").toContain("add_comment");
        expect(Object.keys(config).length, "should deduplicate types").toBe(2);
        expect(config.create_issue, "config value should be empty object").toEqual({});
      } finally {
        fs.unlinkSync(tmpFile);
      }
    });

    it("normalizes dashes to underscores in type names", async () => {
      const fs = require("fs");
      const os = require("os");
      const path = require("path");
      const { buildHandlerConfigFromOutput } = await import("./apply_safe_outputs_replay.cjs");

      const tmpFile = path.join(os.tmpdir(), `test-agent-output-${Date.now()}.json`);
      const agentOutput = {
        items: [{ type: "push-to-pull-request-branch", branch: "main" }],
      };
      fs.writeFileSync(tmpFile, JSON.stringify(agentOutput));

      try {
        const config = buildHandlerConfigFromOutput(tmpFile);
        expect(Object.keys(config), "should normalize dashes to underscores").toContain("push_to_pull_request_branch");
      } finally {
        fs.unlinkSync(tmpFile);
      }
    });

    it("returns empty config for output with no items", async () => {
      const fs = require("fs");
      const os = require("os");
      const path = require("path");
      const { buildHandlerConfigFromOutput } = await import("./apply_safe_outputs_replay.cjs");

      const tmpFile = path.join(os.tmpdir(), `test-agent-output-${Date.now()}.json`);
      fs.writeFileSync(tmpFile, JSON.stringify({ items: [] }));

      try {
        const config = buildHandlerConfigFromOutput(tmpFile);
        expect(Object.keys(config).length, "config should be empty").toBe(0);
      } finally {
        fs.unlinkSync(tmpFile);
      }
    });

    it("returns empty config when items array is missing", async () => {
      const fs = require("fs");
      const os = require("os");
      const path = require("path");
      const { buildHandlerConfigFromOutput } = await import("./apply_safe_outputs_replay.cjs");

      const tmpFile = path.join(os.tmpdir(), `test-agent-output-${Date.now()}.json`);
      fs.writeFileSync(tmpFile, JSON.stringify({}));

      try {
        const config = buildHandlerConfigFromOutput(tmpFile);
        expect(Object.keys(config).length, "config should be empty for missing items").toBe(0);
      } finally {
        fs.unlinkSync(tmpFile);
      }
    });

    it("skips items with no type field", async () => {
      const fs = require("fs");
      const os = require("os");
      const path = require("path");
      const { buildHandlerConfigFromOutput } = await import("./apply_safe_outputs_replay.cjs");

      const tmpFile = path.join(os.tmpdir(), `test-agent-output-${Date.now()}.json`);
      const agentOutput = {
        items: [{ title: "No type field" }, { type: "create_issue", title: "Has type" }],
      };
      fs.writeFileSync(tmpFile, JSON.stringify(agentOutput));

      try {
        const config = buildHandlerConfigFromOutput(tmpFile);
        expect(Object.keys(config), "should include create_issue").toContain("create_issue");
        expect(Object.keys(config).length, "should have exactly one entry").toBe(1);
      } finally {
        fs.unlinkSync(tmpFile);
      }
    });

    it("skips items with non-string type field", async () => {
      const fs = require("fs");
      const os = require("os");
      const path = require("path");
      const { buildHandlerConfigFromOutput } = await import("./apply_safe_outputs_replay.cjs");

      const tmpFile = path.join(os.tmpdir(), `test-agent-output-${Date.now()}.json`);
      const agentOutput = {
        items: [{ type: 42 }, { type: null }, { type: "add_comment" }],
      };
      fs.writeFileSync(tmpFile, JSON.stringify(agentOutput));

      try {
        const config = buildHandlerConfigFromOutput(tmpFile);
        expect(Object.keys(config), "should only include string type").toContain("add_comment");
        expect(Object.keys(config).length, "should skip non-string types").toBe(1);
      } finally {
        fs.unlinkSync(tmpFile);
      }
    });

    it("deduplicates repeated types preserving single entry", async () => {
      const fs = require("fs");
      const os = require("os");
      const path = require("path");
      const { buildHandlerConfigFromOutput } = await import("./apply_safe_outputs_replay.cjs");

      const tmpFile = path.join(os.tmpdir(), `test-agent-output-${Date.now()}.json`);
      const agentOutput = {
        items: [{ type: "add_comment" }, { type: "add_comment" }, { type: "add_comment" }, { type: "create_issue" }],
      };
      fs.writeFileSync(tmpFile, JSON.stringify(agentOutput));

      try {
        const config = buildHandlerConfigFromOutput(tmpFile);
        expect(Object.keys(config).length, "should have 2 unique types").toBe(2);
        expect(config.add_comment, "deduplicated config value should be empty object").toEqual({});
      } finally {
        fs.unlinkSync(tmpFile);
      }
    });
  });

  describe("main", () => {
    it("calls setFailed when GH_AW_RUN_URL is not set", async () => {
      const { main } = await import("./apply_safe_outputs_replay.cjs");
      delete process.env.GH_AW_RUN_URL;
      await main();
      expect(global.core.setFailed, "should call setFailed when no run URL").toHaveBeenCalledOnce();
      expect(global.core.setFailed.mock.calls[0][0], "should mention GH_AW_RUN_URL").toMatch(/GH_AW_RUN_URL/);
    });

    it("calls setFailed for an invalid GH_AW_RUN_URL", async () => {
      const { main } = await import("./apply_safe_outputs_replay.cjs");
      process.env.GH_AW_RUN_URL = "not-a-valid-run-url";
      await main();
      expect(global.core.setFailed, "should call setFailed for unparseable URL").toHaveBeenCalledOnce();
      expect(global.core.setFailed.mock.calls[0][0], "should describe the parse error").toMatch(/Cannot parse run ID/);
    });

    it("calls setFailed when exec fails to download the artifact", async () => {
      const { main } = await import("./apply_safe_outputs_replay.cjs");
      process.env.GH_AW_RUN_URL = "23560193313";
      global.exec.exec = vi.fn().mockResolvedValue(1); // non-zero exit code
      await main();
      expect(global.core.setFailed, "should call setFailed on download failure").toHaveBeenCalledOnce();
      expect(global.core.setFailed.mock.calls[0][0], "error should mention ERR_SYSTEM").toMatch(/Failed to download agent artifact/);
    });
  });

  describe("parseRunUrl (additional edge cases)", () => {
    it("throws for null input", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      // @ts-expect-error testing invalid input
      expect(() => parseRunUrl(null), "should throw for null").toThrow(/run_url is required/);
    });

    it("throws for whitespace-only input", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      expect(() => parseRunUrl("   "), "should throw for whitespace-only input").toThrow(/Cannot parse run ID/);
    });

    it("parses run URL with query string", async () => {
      const { parseRunUrl } = await import("./apply_safe_outputs_replay.cjs");
      const result = parseRunUrl("https://github.com/github/gh-aw/actions/runs/99887766?check_suite_focus=true");
      expect(result.runId, "should parse run ID ignoring query string").toBe("99887766");
      expect(result.owner, "should parse owner").toBe("github");
      expect(result.repo, "should parse repo").toBe("gh-aw");
    });
  });
});
