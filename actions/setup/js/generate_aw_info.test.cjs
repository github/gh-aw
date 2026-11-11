import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import { createHash } from "node:crypto";

// Mock the global objects that GitHub Actions provides
const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

// Set up global mocks before importing the module
global.core = mockCore;

const mockContext = {
  runId: 12345,
  runNumber: 42,
  sha: "abc123def456",
  ref: "refs/heads/main",
  actor: "octocat",
  eventName: "push",
  repo: { owner: "github", repo: "my-repo" },
};

const mockGithub = {
  rest: {
    repos: {
      getContent: vi.fn(async ({ path }) => ({
        data: { type: "file", path, encoding: "base64", content: Buffer.from('# gh-aw-metadata: {}\n# gh-aw-manifest: {"version":1,"agent_metadata":{}}\n# compiled fixture\n').toString("base64") },
      })),
    },
    actions: {
      getWorkflowRun: vi.fn(),
    },
  },
};

const queueAssignment = {
  version: 3,
  dispatch_id: "d1",
  request_id: "request1",
  commit_id: "commit1",
  policy_epoch: "e1",
  pool: "default",
  worker_profile: "worker",
  claims: [{ handle: "h1", claim_id: "c1", work_id: "w1", work: { task: "test", inputs: { labels: ["bug"] } }, result_refs: [] }],
};

describe("generate_aw_info.cjs", () => {
  let main;
  let awInfoPath;
  let originalGithub;

  it("persists declared sub-agent models without configured skills", async () => {
    const models = [{ name: "reader", model: "small", patterns: ["github-copilot/*haiku*"] }];
    process.env.GH_AW_INFO_SUB_AGENT_MODELS = JSON.stringify(models);
    await main(mockCore, mockContext);
    const info = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(info.sub_agent_models).toEqual(models);
    expect(info.skills).toBeUndefined();
  });

  // Compiled GH_AW_INFO_SUB_AGENT_MODELS of typist (Claude) and daily-arxiv-researcher (Codex,
  // whose run 39394965127 never delegated), as written to their lock files.
  it.each([
    ["claude", '[{"model":"small","name":"duplicate-type-finder","patterns":["anthropic/*haiku*"]}]'],
    [
      "codex",
      '[{"model":"openai/gpt-5.4-mini","name":"preliminary-investigator","patterns":["openai/gpt-5.4-mini"]},{"model":"openai/gpt-5.4-mini","name":"paper-screener","patterns":["openai/gpt-5.4-mini"]},{"model":"openai/gpt-5.4-mini","name":"relevance-ranker","patterns":["openai/gpt-5.4-mini"]},{"model":"openai/gpt-5.6-sol","name":"opportunity-extractor","patterns":["openai/gpt-5.6-sol"]}]',
    ],
  ])("claude_codex_aw_info_contains_declared_subagent_models (%s)", async (_engine, declarations) => {
    process.env.GH_AW_INFO_SUB_AGENT_MODELS = declarations;
    await main(mockCore, mockContext);
    const info = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(info.sub_agent_models).toEqual(JSON.parse(declarations));
  });

  beforeEach(async () => {
    vi.clearAllMocks();
    originalGithub = global.github;
    // Keep compiler-fixed runner paths in memory; never write to the host's /tmp.
    const files = new Map();
    const realRead = fs.readFileSync.bind(fs);
    const realExists = fs.existsSync.bind(fs);
    const realStat = fs.lstatSync.bind(fs);
    const realPath = fs.realpathSync.bind(fs);
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    vi.spyOn(fs, "writeFileSync").mockImplementation((filename, content) => files.set(String(filename), String(content)));
    vi.spyOn(fs, "existsSync").mockImplementation(filename => (String(filename).startsWith("/tmp/gh-aw") ? files.has(String(filename)) : realExists(filename)));
    vi.spyOn(fs, "lstatSync").mockImplementation(filename => (String(filename).endsWith(".lock.yml") ? { isFile: () => true, size: 128 } : realStat(filename)));
    vi.spyOn(fs, "realpathSync").mockImplementation(filename => (String(filename).endsWith(".lock.yml") ? String(filename) : realPath(filename)));
    vi.spyOn(fs, "readFileSync").mockImplementation((filename, ...args) => {
      if (String(filename).endsWith(".lock.yml")) {
        const header = '# gh-aw-metadata: {}\n# gh-aw-manifest: {"version":1,"agent_metadata":{}}\n# compiled fixture\n';
        return args[0] === "utf8" ? header : Buffer.from(header);
      }
      if (files.has(String(filename))) return files.get(String(filename));
      if (String(filename).startsWith("/tmp/gh-aw")) throw Object.assign(new Error("missing fixture"), { code: "ENOENT" });
      return realRead(filename, ...args);
    });
    vi.spyOn(fs, "unlinkSync").mockImplementation(filename => files.delete(String(filename)));
    awInfoPath = "/tmp/gh-aw/aw_info.json";
    delete process.env.GH_AW_WORK_QUEUE_ENABLED;
    delete process.env.GH_AW_WORK_QUEUE_ROLE;

    // Set default env vars for compile-time values
    process.env.GH_AW_INFO_ENGINE_ID = "copilot";
    process.env.GH_AW_INFO_ENGINE_NAME = "GitHub Copilot CLI";
    process.env.GH_AW_INFO_MODEL = "gpt-4";
    process.env.GH_AW_INFO_VERSION = "";
    process.env.GH_AW_INFO_AGENT_VERSION = "0.0.419";
    process.env.GH_AW_INFO_CLI_VERSION = "";
    process.env.GH_AW_INFO_WORKFLOW_NAME = "my-workflow";
    process.env.GH_AW_INFO_WORKFLOW_ID = "github/my-repo/.github/workflows/my-workflow.lock.yml@refs/heads/main";
    process.env.GH_AW_INFO_WORKFLOW_SHA = "a".repeat(40);
    global.github = mockGithub;
    process.env.GH_AW_INFO_EXPERIMENTAL = "false";
    process.env.GH_AW_INFO_SUPPORTS_TOOLS_ALLOWLIST = "true";
    delete process.env.GH_AW_INFO_CACHE_MEMORY;
    process.env.GH_AW_INFO_STAGED = "false";
    delete process.env.GH_AW_INFO_DRY_RUN;
    process.env.GH_AW_INFO_ALLOWED_DOMAINS = "[]";
    process.env.GH_AW_INFO_FIREWALL_ENABLED = "false";
    process.env.GH_AW_INFO_AWF_VERSION = "";
    process.env.GH_AW_INFO_AWMG_VERSION = "";
    process.env.GH_AW_INFO_FIREWALL_TYPE = "";
    process.env.GH_AW_INFO_AGENT_RUNTIME = "";
    process.env.GH_AW_INFO_FRONTMATTER_SOURCE = "";
    process.env.GH_AW_INFO_FRONTMATTER_EMOJI = "";
    process.env.GH_AW_INFO_BODY_MODIFIED = "";
    process.env.GH_AW_INFO_FEATURES = "";
    process.env.GH_AW_INFO_SKILLS = "";
    delete process.env.GH_AW_INFO_FETCH_RUN_CREATED_AT;

    // Dynamic import to get fresh module state
    const module = await import("./generate_aw_info.cjs");
    main = module.main;
  });

  afterEach(() => {
    if (fs.existsSync(awInfoPath)) {
      fs.unlinkSync(awInfoPath);
    }
    // Clean up env vars
    const keysToDelete = Object.keys(process.env).filter(k => k.startsWith("GH_AW_INFO_"));
    for (const key of keysToDelete) {
      delete process.env[key];
    }
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
    global.github = originalGithub;
  });

  it("should write aw_info.json with values from env vars and context", async () => {
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "1");
    await main(mockCore, mockContext);

    expect(fs.existsSync(awInfoPath)).toBe(true);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));

    expect(awInfo.engine_id).toBe("copilot");
    expect(awInfo.engine_name).toBe("GitHub Copilot CLI");
    expect(awInfo.model).toBe("gpt-4");
    expect(awInfo.workflow_name).toBe("my-workflow");
    expect(awInfo.workflow_id).toBe("github/my-repo/.github/workflows/my-workflow.lock.yml@refs/heads/main");
    expect(awInfo.workflow_call_id).toBe("12345-1:github/my-repo/.github/workflows/my-workflow.lock.yml@refs/heads/main");
    expect(awInfo.experimental).toBe(false);
    expect(awInfo.supports_tools_allowlist).toBe(true);
    expect(awInfo.cache_memory).toBe(false);
    expect(awInfo.run_id).toBe(12345);
    expect(awInfo.run_number).toBe(42);
    expect(awInfo.sha).toBe("abc123def456");
    expect(awInfo.repository).toBe("github/my-repo");
    expect(awInfo.actor).toBe("octocat");
    expect(awInfo.event_name).toBe("push");
    expect(awInfo.staged).toBe(false);
    expect(awInfo.dry_run).toBe(false);
    expect(awInfo.firewall_enabled).toBe(false);
    expect(awInfo.features).toBeUndefined();
    expect(awInfo.created_at).toBeTruthy();
  });

  it("keeps compiler metadata in aw_info.json without dumping it to Actions logs", async () => {
    const agent = { name: "private-agent", description: "confidential-agent-description", model: "private-model", tools: ["confidential-tool"] };
    const digest = value => createHash("sha256").update(JSON.stringify(value)).digest("hex");
    const agentDigest = digest(agent);
    const errorDigest = digest("invalid-agent-source");
    const manifest = {
      version: 1,
      agent_metadata: { [agentDigest]: agent, [errorDigest]: null },
      metadata_errors: { [`artifact:${errorDigest}`]: "confidential-compiler-error" },
    };
    mockGithub.rest.repos.getContent.mockImplementationOnce(async ({ path }) => ({
      data: { type: "file", path, encoding: "base64", content: Buffer.from(`# gh-aw-metadata: {}\n# gh-aw-manifest: ${JSON.stringify(manifest)}\n# compiled fixture\n`).toString("base64") },
    }));

    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo).toMatchObject({
      agent_metadata: { [agentDigest.slice(0, 20)]: agent },
      metadata_errors: { [`artifact:${errorDigest.slice(0, 20)}`]: "confidential-compiler-error" },
      engine_id: "copilot",
      run_id: mockContext.runId,
    });
    expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ ref: process.env.GH_AW_INFO_WORKFLOW_SHA }));
    expect(mockCore.setOutput).toHaveBeenCalledWith("model", "gpt-4");
    expect(mockCore.setOutput).toHaveBeenCalledWith("engine_id", "copilot");
    expect(mockCore.info).toHaveBeenCalledWith("Generated aw_info.json at: " + awInfoPath);
    const logs = [mockCore.info, mockCore.debug, mockCore.warning, mockCore.error].flatMap(log => log.mock.calls.flat()).join("\n");
    for (const value of ["agent_metadata", "model_pins", "metadata_errors", agent.name, agent.description, agent.model, ...agent.tools, ...Object.values(manifest.metadata_errors)]) {
      expect(logs).not.toContain(value);
    }
  });

  describe("GitHub API quota exhaustion while reading the executing lock manifest", () => {
    const quotaFailure = resetSeconds =>
      Object.assign(new Error("API rate limit exceeded for installation"), {
        status: 403,
        response: { status: 403, headers: { "x-ratelimit-remaining": "0", "x-ratelimit-limit": "15000", "x-ratelimit-used": "15000", "x-ratelimit-resource": "core", "x-ratelimit-reset": String(resetSeconds) } },
      });
    const clockAt = start => {
      let current = start;
      vi.spyOn(Date, "now").mockImplementation(() => current);
      return {
        now: () => current,
        sleep: vi.fn(async ms => {
          current += ms;
        }),
      };
    };

    it("waits for the failed response's reset and then generates run info", async () => {
      const start = Date.parse("2026-11-09T23:58:53Z");
      const reset = Date.parse("2026-11-10T00:00:04Z");
      const clock = clockAt(start);
      const requestTimes = [];
      const fixture = mockGithub.rest.repos.getContent.getMockImplementation();
      mockGithub.rest.repos.getContent.mockImplementationOnce(async () => {
        requestTimes.push(clock.now());
        throw quotaFailure(reset / 1000);
      });
      mockGithub.rest.repos.getContent.mockImplementationOnce(async request => {
        requestTimes.push(clock.now());
        return fixture(request);
      });
      await main(mockCore, mockContext, mockGithub, { quotaWait: clock });
      expect(clock.sleep).toHaveBeenCalledExactlyOnceWith(reset + 1000 - start);
      expect(requestTimes).toHaveLength(2);
      expect(requestTimes[1]).toBeGreaterThanOrEqual(reset);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(fs.writeFileSync).toHaveBeenCalledWith(awInfoPath, expect.any(String));
    });

    it("blocks activation with an explicit quota summary when the reset is beyond the bounded wait", async () => {
      const start = Date.parse("2026-11-09T23:00:00Z");
      const clock = clockAt(start);
      mockGithub.rest.repos.getContent.mockRejectedValueOnce(quotaFailure(start / 1000 + 3600));
      await expect(main(mockCore, mockContext, mockGithub, { quotaWait: clock })).rejects.toThrow("GitHub API quota exhausted; activation blocked until at least 2026-11-10T00:00:00.000Z");
      expect(clock.sleep).not.toHaveBeenCalled();
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledTimes(1);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("resource=core; limit=15000; remaining=0; used=15000; reset=2026-11-10T00:00:00.000Z"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("reason=reset_exceeds_budget"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("### GitHub API quota exhausted"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("no unverified fallback was used"));
      expect(fs.writeFileSync).not.toHaveBeenCalledWith(awInfoPath, expect.any(String));
    });

    it("does not reclassify an authorization 403 as quota exhaustion", async () => {
      const denied = Object.assign(new Error("Resource not accessible by integration"), { status: 403, response: { status: 403, headers: {} } });
      mockGithub.rest.repos.getContent.mockRejectedValueOnce(denied);
      await expect(main(mockCore, mockContext, mockGithub)).rejects.toBe(denied);
      expect(mockCore.summary.addRaw).not.toHaveBeenCalledWith(expect.stringContaining("GitHub API quota exhausted"));
    });
  });

  it("records the executing reusable workflow separately from its caller in the same run", async () => {
    const callerRef = "github/my-repo/.github/workflows/caller.yml@refs/heads/main";
    const executingRef = "org/platform/.github/workflows/worker.lock.yml@refs/tags/v1";
    process.env.GH_AW_INFO_WORKFLOW_ID = executingRef;
    vi.stubEnv("GITHUB_WORKFLOW_REF", callerRef);
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
    const caller = { repo: "github/my-repo", run_id: "12345", run_attempt: "2", workflow_id: callerRef, workflow_call_id: `12345-2:${callerRef}` };
    await main(mockCore, { ...mockContext, payload: { inputs: { aw_context: JSON.stringify(caller) } } }, mockGithub);
    const info = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(info).toMatchObject({
      workflow_id: executingRef,
      workflow_call_id: `12345-2:${executingRef}`,
      caller_workflow_ref: callerRef,
      run_id: 12345,
      run_attempt: "2",
      event_name: "push",
      context: caller,
    });
    expect(String(info.run_id)).toBe(info.context.run_id);
    expect(info.workflow_id).not.toBe(info.context.workflow_id);
    expect(info.workflow_call_id).not.toBe(info.context.workflow_call_id);
  });

  it("does not misidentify the caller as the executing workflow when its reference is unavailable", async () => {
    delete process.env.GH_AW_INFO_WORKFLOW_ID;
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
    vi.stubEnv("GITHUB_WORKFLOW_REF", "github/my-repo/.github/workflows/caller.yml@refs/heads/main");
    await expect(main(mockCore, mockContext)).rejects.toThrow("executing workflow identity");
    expect(fs.existsSync(awInfoPath)).toBe(false);
  });

  it("defaults the workflow call ID to the first attempt when the runner omits it", async () => {
    vi.stubEnv("GITHUB_RUN_ATTEMPT", undefined);
    await main(mockCore, mockContext);
    const info = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(info.workflow_call_id).toBe("12345-1:github/my-repo/.github/workflows/my-workflow.lock.yml@refs/heads/main");
  });

  it("should record dry-run and explicitly report unverified extension effects", async () => {
    process.env.GH_AW_INFO_DRY_RUN = "true";
    await main(mockCore, mockContext);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.dry_run).toBe(true);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("not verified read-only"));
  });

  it("should expose the authoritative run creation time when requested", async () => {
    process.env.GH_AW_INFO_FETCH_RUN_CREATED_AT = "true";
    mockGithub.rest.actions.getWorkflowRun.mockResolvedValue({ data: { created_at: "2026-08-24T12:00:00Z" } });

    await main(mockCore, mockContext, mockGithub);

    expect(mockGithub.rest.actions.getWorkflowRun).toHaveBeenCalledWith({
      owner: "github",
      repo: "my-repo",
      run_id: 12345,
    });
    expect(mockCore.setOutput).toHaveBeenCalledWith("run_created_at", "2026-08-24T12:00:00Z");
  });

  it("should include features from GH_AW_INFO_FEATURES and preserve value types", async () => {
    process.env.GH_AW_INFO_FEATURES = JSON.stringify({
      "gh-aw-detection": true,
      "custom-mode": "strict",
    });

    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.features).toEqual({
      "gh-aw-detection": true,
      "custom-mode": "strict",
    });
  });

  it("should include frontmatter skills and log them with core.info", async () => {
    process.env.GH_AW_INFO_SKILLS = JSON.stringify(["githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6", "githubnext/skills/review/security@1f181b37d3fe5862ab590648f25a292e345b5de6"]);

    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.skills).toEqual(["githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6", "githubnext/skills/review/security@1f181b37d3fe5862ab590648f25a292e345b5de6"]);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Configured frontmatter skills"));
  });

  it("should warn and ignore non-array GH_AW_INFO_SKILLS", async () => {
    process.env.GH_AW_INFO_SKILLS = JSON.stringify("not-an-array");

    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.skills).toBeUndefined();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("GH_AW_INFO_SKILLS must be a JSON array"));
  });

  it("should warn and skip non-string GH_AW_INFO_SKILLS entries", async () => {
    process.env.GH_AW_INFO_SKILLS = JSON.stringify([42, "githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6"]);

    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.skills).toEqual(["githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6"]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("GH_AW_INFO_SKILLS[0]"));
  });

  it("should warn and ignore invalid GH_AW_INFO_SKILLS JSON", async () => {
    process.env.GH_AW_INFO_SKILLS = "not-json";

    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.skills).toBeUndefined();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to parse GH_AW_INFO_SKILLS"));
  });

  it("should include frontmatter source/emoji and body_modified when configured", async () => {
    process.env.GH_AW_INFO_FRONTMATTER_SOURCE = "github/gh-aw/.github/workflows/example.md@main";
    process.env.GH_AW_INFO_FRONTMATTER_EMOJI = "🧪";
    process.env.GH_AW_INFO_BODY_MODIFIED = "true";

    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.frontmatter_source).toBe("github/gh-aw/.github/workflows/example.md@main");
    expect(awInfo.frontmatter_emoji).toBe("🧪");
    expect(awInfo.body_modified).toBe(true);
  });

  it("should set model output", async () => {
    await main(mockCore, mockContext);

    expect(mockCore.setOutput).toHaveBeenCalledWith("model", "gpt-4");
  });

  it("should include cli_version only when GH_AW_INFO_CLI_VERSION is set", async () => {
    process.env.GH_AW_INFO_CLI_VERSION = "1.2.3";
    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.cli_version).toBe("1.2.3");
  });

  it("should not include cli_version when GH_AW_INFO_CLI_VERSION is empty", async () => {
    process.env.GH_AW_INFO_CLI_VERSION = "";
    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.cli_version).toBeUndefined();
  });

  it("should set cache_memory to true when GH_AW_INFO_CACHE_MEMORY is true", async () => {
    process.env.GH_AW_INFO_CACHE_MEMORY = "true";
    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.cache_memory).toBe(true);
  });

  it("should parse allowed domains from JSON env var", async () => {
    process.env.GH_AW_INFO_ALLOWED_DOMAINS = '["github.com","api.github.com"]';
    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.allowed_domains).toEqual(["github.com", "api.github.com"]);
  });

  it("should warn and use empty array for invalid allowed_domains JSON", async () => {
    process.env.GH_AW_INFO_ALLOWED_DOMAINS = "not-valid-json";
    await main(mockCore, mockContext);

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to parse GH_AW_INFO_ALLOWED_DOMAINS"));
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.allowed_domains).toEqual([]);
  });

  it("should warn for missing required context fields", async () => {
    const incompleteContext = { runId: 1 };
    await main(mockCore, incompleteContext);

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("context.runNumber is not set"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("context.sha is not set"));
  });

  it("should set firewall info from env vars", async () => {
    process.env.GH_AW_INFO_FIREWALL_ENABLED = "true";
    process.env.GH_AW_INFO_AWF_VERSION = "v0.23.0";
    process.env.GH_AW_INFO_FIREWALL_TYPE = "squid";
    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.firewall_enabled).toBe(true);
    expect(awInfo.awf_version).toBe("v0.23.0");
    expect(awInfo.steps.firewall).toBe("squid");
  });

  it("should set agent_runtime from env var", async () => {
    process.env.GH_AW_INFO_AGENT_RUNTIME = "cloud-hypervisor";
    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.agent_runtime).toBe("cloud-hypervisor");
  });

  it("should default agent_runtime to empty string when not set", async () => {
    await main(mockCore, mockContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.agent_runtime).toBe("");
  });

  it("should fail when model name contains an unresolved GitHub Actions expression", async () => {
    process.env.GH_AW_INFO_MODEL = "${{ vars.COPILOT_MODEL }}";

    await expect(main(mockCore, mockContext)).rejects.toThrow("ERR_CONFIG");
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("ERR_CONFIG"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("unresolved GitHub Actions expression"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(JSON.stringify("${{ vars.COPILOT_MODEL }}")));
  });

  it("should fail when model name contains a partial unresolved expression", async () => {
    process.env.GH_AW_INFO_MODEL = "${{ vars.MODEL || 'gpt-4' }}";

    await expect(main(mockCore, mockContext)).rejects.toThrow("ERR_CONFIG");
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("ERR_CONFIG"));
  });

  it.each(["router", "router/economy", "router/balanced", "router/robust", "router/fast", "Router/Economy", "router/economy?effort=high"])(
    "fails before the agent starts when an unrouted main model resolves to the router value %s",
    async model => {
      // For example vars.GH_AW_DEFAULT_MODEL_COPILOT or ${{ vars.MODEL }} resolving to a router value.
      process.env.GH_AW_INFO_MODEL = model;
      process.env.GH_AW_INFO_MODEL_CONFIG = '{"routing":{"enabled":false},"models":{"allowed":[],"blocked":[]},"max_ai_credits_source":"default"}';
      try {
        await expect(main(mockCore, mockContext)).rejects.toThrow("ERR_CONFIG");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(`resolved to the router value ${JSON.stringify(model)} at run time`));
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("router, router/economy, router/balanced, router/robust"));
        expect(fs.existsSync(awInfoPath)).toBe(false);
      } finally {
        delete process.env.GH_AW_INFO_MODEL_CONFIG;
      }
    }
  );

  it("fails on a runtime router value when no model config is compiled", async () => {
    process.env.GH_AW_INFO_MODEL = "router/economy";
    delete process.env.GH_AW_INFO_MODEL_CONFIG;
    await expect(main(mockCore, mockContext)).rejects.toThrow("ERR_CONFIG");
  });

  it("fails when an empty model expression falls back to a router value from the model variables", async () => {
    // model: ${{ inputs.model }} resolved empty; the harness would use GH_AW_MODEL_FALLBACK,
    // here vars.GH_AW_DEFAULT_MODEL_COPILOT=router/economy.
    process.env.GH_AW_INFO_MODEL = "";
    process.env.GH_AW_INFO_MODEL_FALLBACK = "router/economy";
    process.env.GH_AW_INFO_MODEL_CONFIG = '{"routing":{"enabled":false},"models":{"allowed":[],"blocked":[]},"max_ai_credits_source":"default"}';
    try {
      await expect(main(mockCore, mockContext)).rejects.toThrow("ERR_CONFIG");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining('resolved to the router value "router/economy" at run time'));
      expect(fs.existsSync(awInfoPath)).toBe(false);
    } finally {
      delete process.env.GH_AW_INFO_MODEL_FALLBACK;
      delete process.env.GH_AW_INFO_MODEL_CONFIG;
    }
  });

  it("checks the resolved model expression rather than the fallback when the expression is nonempty", async () => {
    process.env.GH_AW_INFO_MODEL = "gpt-6.1-sol";
    process.env.GH_AW_INFO_MODEL_FALLBACK = "router/economy";
    try {
      await main(mockCore, mockContext);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    } finally {
      delete process.env.GH_AW_INFO_MODEL_FALLBACK;
    }
  });

  it("fails when vars.GH_AW_MODEL_AGENT_<ENGINE> disables implicit routing and resolves to a router value", async () => {
    // Without a model, GH_AW_INFO_MODEL is vars.GH_AW_MODEL_AGENT_COPILOT || vars.GH_AW_DEFAULT_MODEL_COPILOT || 'auto',
    // and a nonempty agent variable makes the activation decision disable implicit routing.
    process.env.GH_AW_INFO_MODEL = "router/robust";
    process.env.GH_AW_INFO_MODEL_CONFIG = '{"routing":{"enabled":false},"models":{"allowed":[],"blocked":[]},"max_ai_credits_source":"default"}';
    try {
      await expect(main(mockCore, mockContext)).rejects.toThrow("ERR_CONFIG");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining('resolved to the router value "router/robust" at run time'));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("vars.GH_AW_MODEL_AGENT_<ENGINE>"));
    } finally {
      delete process.env.GH_AW_INFO_MODEL_CONFIG;
    }
  });

  it("does not fail on a router-like model variable when the compiled config routes the run", async () => {
    // Explicit routing ignores the model variables, so their value is never passed to the engine.
    process.env.GH_AW_INFO_MODEL = "router/economy";
    process.env.GH_AW_INFO_MODEL_CONFIG = '{"routing":{"enabled":true,"goal":"cost","mode":"economy","requested":"router/economy"},"models":{"allowed":[],"blocked":[]},"max_ai_credits_source":"default"}';
    try {
      await main(mockCore, mockContext);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
      expect(awInfo.model_config.routing).toEqual({ enabled: true, goal: "cost", mode: "economy", requested: "router/economy" });
    } finally {
      delete process.env.GH_AW_INFO_MODEL_CONFIG;
    }
  });

  it.each(["openrouter/auto", "routerx", "my-router/economy", "gpt-6.1-sol"])("does not treat %s as a router value", async model => {
    process.env.GH_AW_INFO_MODEL = model;
    await main(mockCore, mockContext);
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should not fail when model name does not contain unresolved expressions", async () => {
    const cases = [
      { label: "plain string", model: "gpt-4o" },
      { label: "empty string", model: "" },
    ];

    for (const { label, model } of cases) {
      process.env.GH_AW_INFO_MODEL = model;

      try {
        await main(mockCore, mockContext);

        expect(mockCore.setFailed).not.toHaveBeenCalled();
        const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
        expect(awInfo.model).toBe(model);
      } catch (error) {
        throw new Error(`${label} case failed: ${error instanceof Error ? error.message : String(error)}`);
      }
    }
  });

  it("should not fail when model is a resolved experiment variant string", async () => {
    // Workflows using experiment variants set model to a GitHub Actions expression in frontmatter
    // (e.g. model: "${{ needs.activation.outputs.model_size }}").  GitHub Actions evaluates
    // that expression before passing the value as an environment variable, so by the time
    // generate_aw_info.cjs runs, GH_AW_INFO_MODEL holds the resolved variant string.
    process.env.GH_AW_INFO_MODEL = "claude-haiku-4.5";

    await main(mockCore, mockContext);

    expect(mockCore.setFailed).not.toHaveBeenCalled();
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.model).toBe("claude-haiku-4.5");
  });

  it("should fail when a numeric context field contains non-numeric data", async () => {
    const maliciousContext = {
      ...mockContext,
      payload: {
        issue: { number: "42; DROP TABLE users" },
      },
    };

    await expect(main(mockCore, maliciousContext)).rejects.toThrow();
    expect(mockCore.setFailed).toHaveBeenCalled();
  });

  it("should pass context validation when numeric fields are valid integers", async () => {
    const validContext = {
      ...mockContext,
      payload: {
        issue: { number: 42 },
        pull_request: { number: 100 },
      },
    };

    await main(mockCore, validContext);
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ All context variables validated successfully"));
  });

  it("should call generateWorkflowOverview to write step summary", async () => {
    await main(mockCore, mockContext);

    expect(mockCore.summary.addRaw).toHaveBeenCalled();
    expect(mockCore.summary.write).toHaveBeenCalled();
  });

  it("should reject aw_context that is not a plain object", async () => {
    const contextWithArrayInput = {
      ...mockContext,
      payload: { inputs: { aw_context: JSON.stringify([1, 2, 3]) } },
    };
    await main(mockCore, contextWithArrayInput);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toBeUndefined();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("must be a JSON object"));
    expect(mockCore.setOutput).toHaveBeenCalledWith("aw_context", "{}");
  });

  it("should ignore non-JSON aw_context in downstream expressions", async () => {
    await main(mockCore, {
      ...mockContext,
      payload: { inputs: { aw_context: "SomeProject" } },
    });

    expect(mockCore.setOutput).toHaveBeenCalledWith("aw_context", "{}");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to parse aw_context input as JSON"));
  });

  it("exposes normalized webhook arrays in aw_info and expression outputs without requiring caller-run fields", async () => {
    const deliveries = [
      { delivery_id: "first", payload: { url: "https://api.github.com/orgs/org/projectsV2/1/items/1", type: ["edited"] } },
      { delivery_id: "second", payload: { url: "https://api.github.com/orgs/org/projectsV2/1/items/2", type: ["created"] } },
      { delivery_id: "third", payload: { url: "https://api.github.com/orgs/org/projectsV2/1/items/1", type: ["edited"] } },
    ];
    const raw = { webhook: { event: "projects_v2_item", deliveries: [...deliveries, deliveries[0]] }, items: [{ item_number: "forged" }], item_number: "first-item-must-not-be-selected" };
    const expected = {
      webhook: { event: "projects_v2_item", deliveries },
      items: [1, 2].map(number => ({ item_type: "projects_v2_item", item_number: String(number), repo: "", source: "https://api.github.com/orgs/org/projectsV2/1", url: `https://api.github.com/orgs/org/projectsV2/1/items/${number}` })),
    };
    for (const payload of [{ client_payload: { schema_version: 2, aw_context: raw } }, { inputs: { aw_context: JSON.stringify({ repo: "org/repo", run_id: "1", workflow_id: "parent", ...raw }) } }]) {
      vi.clearAllMocks();
      await main(mockCore, { ...mockContext, eventName: "repository_dispatch", payload });
      const info = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
      expect(info.context).toMatchObject(expected);
      expect(info.context.item_number).toBe("");
      const [, json] = mockCore.setOutput.mock.calls.find(([key]) => key === "aw_context");
      expect(JSON.parse(json)).toMatchObject(expected);
      expect(mockCore.warning).not.toHaveBeenCalled();
      expect(mockCore.info.mock.calls.flat().join("\n")).not.toContain("forged");
      expect(mockCore.info.mock.calls.flat().join("\n")).not.toContain("do-not-log-payload");
    }
  });

  it("fails malformed webhook arrays instead of returning an empty context", async () => {
    const payload = { client_payload: { aw_context: { webhook: { event: "projects_v2_item", deliveries: [] } } } };
    await expect(main(mockCore, { ...mockContext, payload })).rejects.toThrow(/non-empty array/);
    expect(mockCore.setOutput).not.toHaveBeenCalledWith("aw_context", expect.anything());
    expect(fs.existsSync(awInfoPath)).toBe(false);
  });

  it("should reject aw_context with nested objects", async () => {
    const contextWithNested = {
      ...mockContext,
      payload: {
        inputs: {
          aw_context: JSON.stringify({
            repo: "org/repo",
            run_id: "123",
            workflow_id: "my-workflow",
            nested: { bad: "field" },
          }),
        },
      },
    };
    await main(mockCore, contextWithNested);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toBeUndefined();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("nested objects"));
  });

  it("preserves the canonical Claim-array assignment and its nested immutable payload in normalized context", async () => {
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
    const caller = { repo: "org/repo", run_id: "123", workflow_id: "caller" };
    const expected = { ...caller, work_queue_assignment: queueAssignment };
    for (const payload of [{ inputs: { aw_context: JSON.stringify(expected) } }, { client_payload: { aw_context: expected } }]) {
      await main(mockCore, { ...mockContext, payload });
      expect(JSON.parse(fs.readFileSync(awInfoPath, "utf8")).context).toEqual(expected);
      expect(mockCore.setOutput).toHaveBeenCalledWith("aw_context", JSON.stringify(expected));
    }
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("normalizes the compiler-managed work_queue_assignment input into context", async () => {
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
    const caller = { repo: "org/repo", run_id: "123", workflow_id: "caller" };
    const expected = { ...caller, work_queue_assignment: queueAssignment };
    await main(mockCore, {
      ...mockContext,
      payload: {
        inputs: {
          aw_context: JSON.stringify(caller),
          work_queue_assignment: JSON.stringify(queueAssignment),
        },
      },
    });

    expect(JSON.parse(fs.readFileSync(awInfoPath, "utf8")).context).toEqual(expected);
    expect(mockCore.setOutput).toHaveBeenCalledWith("aw_context", JSON.stringify(expected));
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it.each(["work_queue", "work_claim", "work_queue_claim"])("rejects the retired %s assignment without publishing context", async field => {
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
    const legacy = { work_id: "w", claim_id: "c", work: { task: "test" } };
    const caller = { repo: "org/repo", run_id: "123", workflow_id: "caller" };
    for (const payload of [{ inputs: { aw_context: JSON.stringify({ ...caller, [field]: legacy }) } }, { inputs: { aw_context: JSON.stringify(caller), [field]: JSON.stringify(legacy) } }]) {
      mockCore.warning.mockClear();
      mockCore.setOutput.mockClear();
      await expect(main(mockCore, { ...mockContext, payload })).rejects.toThrow(/legacy/);
      expect(fs.existsSync(awInfoPath)).toBe(false);
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("aw_context", expect.anything());
    }
  });

  it.each([null, [], "claim", { work_id: "", claim_id: "c", work: {} }, { ...queueAssignment, claims: [] }, { ...queueAssignment, claims: [{ ...queueAssignment.claims[0], work: [] }] }, { ...queueAssignment, unexpected: true }])(
    "rejects a malformed current assignment without publishing context",
    async work_queue_assignment => {
      vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
      await expect(
        main(mockCore, {
          ...mockContext,
          payload: { inputs: { aw_context: JSON.stringify({ repo: "org/repo", run_id: "123", workflow_id: "caller", work_queue_assignment }) } },
        })
      ).rejects.toThrow();
      expect(fs.existsSync(awInfoPath)).toBe(false);
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("aw_context", expect.anything());
    }
  );

  it("rejects duplicate assignment sources and decoded JSON keys without falling back to ordinary context", async () => {
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
    const caller = { repo: "org/repo", run_id: "123", workflow_id: "caller" };
    for (const payload of [
      { inputs: { aw_context: JSON.stringify({ ...caller, work_queue_assignment: queueAssignment }), work_queue_assignment: JSON.stringify(queueAssignment) } },
      { inputs: { aw_context: JSON.stringify(caller), work_queue_assignment: JSON.stringify(queueAssignment) }, client_payload: { work_queue_assignment: queueAssignment } },
      { inputs: { aw_context: JSON.stringify({ ...caller, work_queue_assignment: queueAssignment }).replace('"dispatch_id":"d1"', '"dispatch_id":"d1","dispatch_id":"d2"') } },
    ]) {
      mockCore.warning.mockClear();
      mockCore.setOutput.mockClear();
      await expect(main(mockCore, { ...mockContext, payload })).rejects.toThrow();
      expect(fs.existsSync(awInfoPath)).toBe(false);
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("aw_context", expect.anything());
    }
  });

  it.each([
    {},
    { inputs: { work_queue_assignment: null } },
    { inputs: { work_queue_assignment: "" } },
    { inputs: { work_queue_claim: JSON.stringify(queueAssignment) } },
    { inputs: { work_queue_assignment: JSON.stringify({ ...queueAssignment, claims: [] }) } },
    { inputs: { work_queue_assignment: JSON.stringify(queueAssignment), aw_context: JSON.stringify({ work_queue_assignment: queueAssignment }) } },
    { inputs: { work_queue_assignment: JSON.stringify(queueAssignment) }, client_payload: { work_queue_assignment: queueAssignment } },
  ])("fails closed for missing, malformed, legacy or shadowed declared-worker input before publishing metadata", async payload => {
    const previousRole = process.env.GH_AW_WORK_QUEUE_ROLE;
    try {
      vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
      process.env.GH_AW_WORK_QUEUE_ROLE = "worker";
      await expect(main(mockCore, { ...mockContext, payload })).rejects.toThrow();
      expect(fs.existsSync(awInfoPath)).toBe(false);
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("aw_context", expect.anything());
    } finally {
      if (previousRole === undefined) delete process.env.GH_AW_WORK_QUEUE_ROLE;
      else process.env.GH_AW_WORK_QUEUE_ROLE = previousRole;
    }
  });

  it("rejects an observer assignment rather than exporting worker metadata", async () => {
    const previousRole = process.env.GH_AW_WORK_QUEUE_ROLE;
    try {
      vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
      process.env.GH_AW_WORK_QUEUE_ROLE = "observer";
      await expect(main(mockCore, { ...mockContext, payload: { inputs: { work_queue_assignment: JSON.stringify(queueAssignment) } } })).rejects.toThrow(/observer_assignment_forbidden/);
      expect(fs.existsSync(awInfoPath)).toBe(false);
    } finally {
      if (previousRole === undefined) delete process.env.GH_AW_WORK_QUEUE_ROLE;
      else process.env.GH_AW_WORK_QUEUE_ROLE = previousRole;
    }
  });

  it("preserves a declared worker's original multi-Claim assignment without injecting authority fields", async () => {
    const previousRole = process.env.GH_AW_WORK_QUEUE_ROLE;
    const assignment = { ...queueAssignment, claims: [...queueAssignment.claims, { handle: "h2", claim_id: "claim2", work_id: "work2", work: {}, result_refs: [] }] };
    const caller = { repo: "org/repo", run_id: "123", workflow_id: "caller" };
    try {
      vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
      process.env.GH_AW_WORK_QUEUE_ROLE = "worker";
      await main(mockCore, {
        ...mockContext,
        payload: { client_payload: { aw_context: caller, work_queue_assignment: assignment } },
      });
      expect(JSON.parse(fs.readFileSync(awInfoPath, "utf8")).context).toEqual({ ...caller, work_queue_assignment: assignment });
      expect(mockCore.setOutput).toHaveBeenCalledWith("aw_context", JSON.stringify({ ...caller, work_queue_assignment: assignment }));
    } finally {
      if (previousRole === undefined) delete process.env.GH_AW_WORK_QUEUE_ROLE;
      else process.env.GH_AW_WORK_QUEUE_ROLE = previousRole;
    }
  });

  it.each(["worker", "observer", "invalid-role"])("keeps disabled ordinary context unchanged despite stray %s role and assignment inputs", async role => {
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "false");
    vi.stubEnv("GH_AW_WORK_QUEUE_ROLE", role);
    const caller = { repo: "org/repo", run_id: "123", workflow_id: "caller", note: "ordinary metadata" };
    await main(mockCore, {
      ...mockContext,
      payload: { inputs: { aw_context: JSON.stringify(caller), work_queue_assignment: '{"broken":' }, client_payload: { work_queue_assignment: null } },
    });
    expect(JSON.parse(fs.readFileSync(awInfoPath, "utf8")).context).toEqual(caller);
    expect(mockCore.setOutput).toHaveBeenCalledWith("aw_context", JSON.stringify(caller));
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it.each(["work_queue", "work_claim"])("preserves main's disabled %s metadata normalization without current Claim authority", async field => {
    const legacy = { work_id: "w", claim_id: "c", work: { plan: "legacy metadata" } };
    const caller = { repo: "org/repo", run_id: "123", workflow_id: "caller" };
    await main(mockCore, { ...mockContext, payload: { inputs: { aw_context: JSON.stringify({ ...caller, [field]: legacy }) } } });
    const metadata = JSON.parse(fs.readFileSync(awInfoPath, "utf8")).context;
    expect(metadata).toEqual({ ...caller, work_queue: legacy });
    expect(metadata).not.toHaveProperty("work_queue_assignment");
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("treats new nested queue data as ordinary unsupported metadata when disabled", async () => {
    const caller = { repo: "org/repo", run_id: "123", workflow_id: "caller", work_queue_assignment: queueAssignment };
    await main(mockCore, { ...mockContext, payload: { inputs: { aw_context: JSON.stringify(caller) } } });
    expect(JSON.parse(fs.readFileSync(awInfoPath, "utf8"))).not.toHaveProperty("context");
    expect(mockCore.setOutput).toHaveBeenCalledWith("aw_context", "{}");
    expect(mockCore.warning).toHaveBeenCalledWith("aw_context contains nested objects for keys: work_queue_assignment. Ignoring aw_context.");
  });

  it.each([1, 2])("validates a standalone %i-Claim activation without fabricating caller provenance", async count => {
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
    vi.stubEnv("GH_AW_WORK_QUEUE_ROLE", "worker");
    const assignment = { ...queueAssignment, claims: Array.from({ length: count }, (_, i) => ({ ...queueAssignment.claims[0], handle: `h${i + 1}`, claim_id: `c${i + 1}`, work_id: `w${i + 1}` })) };
    await main(mockCore, { ...mockContext, payload: { inputs: { work_queue_assignment: JSON.stringify(assignment) } } });
    expect(JSON.parse(fs.readFileSync(awInfoPath, "utf8"))).not.toHaveProperty("context");
    expect(mockCore.setOutput).toHaveBeenCalledWith("aw_context", "{}");
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("should reject aw_context missing required fields", async () => {
    const contextMissingFields = {
      ...mockContext,
      payload: {
        inputs: {
          aw_context: JSON.stringify({ actor: "octocat" }),
        },
      },
    };
    await main(mockCore, contextMissingFields);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toBeUndefined();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("missing required fields"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("run_id"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("repo"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("workflow_id"));
  });

  it("should accept valid aw_context and set awInfo.context", async () => {
    const validContext = {
      repo: "org/repo",
      run_id: "12345",
      run_attempt: "2",
      workflow_id: "org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      episode_id: "12345-1:org/repo/.github/workflows/root.yml@refs/heads/main",
      hop_id: "12345-2:org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      parent_hop_id: "12345-1:org/repo/.github/workflows/root.yml@refs/heads/main",
      origin_event: "issues",
      root_repo: "org/repo",
      root_workflow_id: "org/repo/.github/workflows/root.yml@refs/heads/main",
      root_run_id: "12345",
      workflow_call_id: "12345-1",
      time: new Date().toISOString(),
      actor: "octocat",
      event_type: "issues",
      item_type: "issue",
      item_number: "42",
      comment_id: "",
    };
    const contextWithValid = {
      ...mockContext,
      payload: { inputs: { aw_context: JSON.stringify(validContext) } },
    };
    await main(mockCore, contextWithValid);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toEqual(validContext);
    expect(mockCore.setOutput).toHaveBeenCalledWith("aw_context", JSON.stringify(validContext));
    expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("aw_context"));
  });

  it("should accept aw_context object from repository_dispatch client_payload", async () => {
    const validContext = {
      repo: "org/repo",
      run_id: "12345",
      workflow_id: "org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      episode_id: "12345-1:org/repo/.github/workflows/root.yml@refs/heads/main",
      hop_id: "12345-2:org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      parent_hop_id: "12345-1:org/repo/.github/workflows/root.yml@refs/heads/main",
      origin_event: "repository_dispatch",
      root_repo: "org/repo",
      root_workflow_id: "org/repo/.github/workflows/root.yml@refs/heads/main",
      root_run_id: "12345",
      workflow_call_id: "12345-2:org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      time: new Date().toISOString(),
      actor: "octocat",
      event_type: "repository_dispatch",
      item_type: "",
      item_number: "",
      comment_id: "",
    };
    const repoDispatchContext = {
      ...mockContext,
      eventName: "repository_dispatch",
      payload: { client_payload: { aw_context: validContext } },
    };

    await main(mockCore, repoDispatchContext);

    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toEqual(validContext);
    expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("aw_context"));
  });

  it("should accept valid aw_context with comment_id and item_number for comment events", async () => {
    const validContext = {
      repo: "org/repo",
      run_id: "12345",
      workflow_id: "org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      workflow_call_id: "12345-1",
      time: new Date().toISOString(),
      actor: "octocat",
      event_type: "issue_comment",
      item_type: "issue",
      item_number: "7",
      comment_id: "99001122",
    };
    const contextWithComment = {
      ...mockContext,
      payload: { inputs: { aw_context: JSON.stringify(validContext) } },
    };
    await main(mockCore, contextWithComment);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toEqual(validContext);
    expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("aw_context"));
  });

  it("should accept valid aw_context for pull_request_review events", async () => {
    const validContext = {
      repo: "org/repo",
      run_id: "12345",
      workflow_id: "org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      workflow_call_id: "12345-1",
      time: new Date().toISOString(),
      actor: "octocat",
      event_type: "pull_request_review",
      item_type: "pull_request",
      item_number: "100",
      comment_id: "55667788",
    };
    const contextWithReview = {
      ...mockContext,
      payload: { inputs: { aw_context: JSON.stringify(validContext) } },
    };
    await main(mockCore, contextWithReview);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toEqual(validContext);
    expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("aw_context"));
  });

  it("should accept valid aw_context for check_run events", async () => {
    const validContext = {
      repo: "org/repo",
      run_id: "12345",
      workflow_id: "org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      workflow_call_id: "12345-1",
      time: new Date().toISOString(),
      actor: "octocat",
      event_type: "check_run",
      item_type: "check_run",
      item_number: "7654321",
      comment_id: "",
    };
    const contextWithCheckRun = {
      ...mockContext,
      payload: { inputs: { aw_context: JSON.stringify(validContext) } },
    };
    await main(mockCore, contextWithCheckRun);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toEqual(validContext);
    expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("aw_context"));
  });

  it("should accept valid aw_context for check_suite events", async () => {
    const validContext = {
      repo: "org/repo",
      run_id: "12345",
      workflow_id: "org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      workflow_call_id: "12345-1",
      time: new Date().toISOString(),
      actor: "octocat",
      event_type: "check_suite",
      item_type: "check_suite",
      item_number: "9988776",
      comment_id: "",
    };
    const contextWithCheckSuite = {
      ...mockContext,
      payload: { inputs: { aw_context: JSON.stringify(validContext) } },
    };
    await main(mockCore, contextWithCheckSuite);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toEqual(validContext);
    expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("aw_context"));
  });

  it("should accept valid aw_context with comment_node_id for discussion_comment events", async () => {
    const validContext = {
      repo: "org/repo",
      run_id: "12345",
      workflow_id: "org/repo/.github/workflows/dispatcher.yml@refs/heads/main",
      workflow_call_id: "12345-1",
      time: new Date().toISOString(),
      actor: "octocat",
      event_type: "discussion_comment",
      item_type: "discussion",
      item_number: "240",
      comment_id: "77889900",
      comment_node_id: "DC_kwDOParentComment456",
    };
    const contextWithDiscussion = {
      ...mockContext,
      payload: { inputs: { aw_context: JSON.stringify(validContext) } },
    };
    await main(mockCore, contextWithDiscussion);
    const awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
    expect(awInfo.context).toEqual(validContext);
    expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("aw_context"));
  });
});
