// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Set GH_AW_PROMPTS_DIR before loading any module that transitively reads prompts
// at initialization time (messages_header.cjs reads safe_outputs_disclosure_header.md).
process.env.GH_AW_PROMPTS_DIR = path.resolve(__dirname, "../md");

const req = createRequire(import.meta.url);

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};
global.core = mockCore;

const { main, formatFailedJobsList, getFailedNonBuiltinJobs, BUILTIN_REPORTED_JOB_IDS } = req("./report_failed_jobs.cjs");

afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllEnvs();
});

describe("formatFailedJobsList", () => {
  it("formats a plain job name with a URL as a markdown link", () => {
    const result = formatFailedJobsList([{ name: "my-job", html_url: "https://github.com/owner/repo/actions/runs/1/jobs/2" }]);
    expect(result).toBe("- [`my-job`](https://github.com/owner/repo/actions/runs/1/jobs/2)");
  });

  it("formats a plain job name without a URL as a plain code span", () => {
    const result = formatFailedJobsList([{ name: "my-job", html_url: null }]);
    expect(result).toBe("- `my-job`");
  });

  it("sanitizes markdown/HTML special characters in job name", () => {
    const result = formatFailedJobsList([{ name: "<script>alert(1)</script>", html_url: null }]);
    expect(result).not.toContain("<script>");
    expect(result).not.toContain("</script>");
  });

  it("sanitizes job name containing HTML comment injection", () => {
    const result = formatFailedJobsList([{ name: "<!-- @exploituser injected payload -->", html_url: null }]);
    expect(result).not.toContain("@exploituser");
  });

  it("rejects html_url with javascript: scheme and renders name-only fallback", () => {
    const result = formatFailedJobsList([{ name: "my-job", html_url: "javascript:alert(1)" }]);
    expect(result).not.toContain("javascript:");
    expect(result).toBe("- `my-job`");
  });

  it("joins multiple jobs with newlines", () => {
    const result = formatFailedJobsList([
      { name: "job-a", html_url: null },
      { name: "job-b", html_url: null },
    ]);
    expect(result).toBe("- `job-a`\n- `job-b`");
  });

  it("returns empty string for empty jobs array", () => {
    expect(formatFailedJobsList([])).toBe("");
  });
});

describe("getFailedNonBuiltinJobs", () => {
  beforeEach(() => {
    global.context = {
      repo: { owner: "owner", repo: "repo" },
      runId: 123,
    };
    vi.stubEnv("GH_AW_JOB_RESULTS", "{}");
    vi.stubEnv("GH_AW_JOB_DISPLAY_NAMES", "{}");
    vi.stubEnv("GH_AW_RUN_URL", "https://github.com/owner/repo/actions/runs/123");
    global.github = {
      rest: {
        actions: {
          listJobsForWorkflowRun: vi.fn().mockResolvedValue({ data: { jobs: [] } }),
        },
        rateLimit: {
          get: vi.fn().mockResolvedValue({ data: { resources: { core: { limit: 5000, remaining: 4999, reset: 0, used: 1 } } } }),
        },
        issues: {
          create: vi.fn().mockResolvedValue({ data: { number: 1, html_url: "https://github.com/owner/repo/issues/1" } }),
        },
      },
    };
  });

  it("filters builtin failures by ID despite changed display names", async () => {
    const names = { agent: "Agent", activation: "Activation", pre_activation: "Pre-activation", safe_outputs: "Safe outputs", detection: "Detection", conclusion: "Conclusion" };
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify(Object.fromEntries([...Object.keys(names), "custom-job"].map(id => [id, { result: "failure" }]))));
    vi.stubEnv("GH_AW_JOB_DISPLAY_NAMES", JSON.stringify(names));
    global.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue({
      data: { jobs: [...Object.values(names), "custom-job"].map(name => ({ name, conclusion: "failure", html_url: `https://example.com/${name}` })) },
    });
    const result = await getFailedNonBuiltinJobs();

    expect(result).toEqual([{ name: "custom-job", html_url: "https://example.com/custom-job" }]);
  });

  it("does not query jobs or create a failed-jobs issue for an agent-only failure", async () => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ agent: { result: "failure" } }));
    vi.stubEnv("GH_AW_JOB_DISPLAY_NAMES", JSON.stringify({ agent: "Renamed agent" }));
    await main();
    expect(global.github.rest.actions.listJobsForWorkflowRun).not.toHaveBeenCalled();
    expect(global.github.rest.issues.create).not.toHaveBeenCalled();
  });

  it("does not suppress a custom job whose display name is a builtin ID", async () => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ agent: { result: "success" }, build: { result: "failure" } }));
    vi.stubEnv("GH_AW_JOB_DISPLAY_NAMES", JSON.stringify({ agent: "Agent", build: "agent" }));
    global.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue({ data: { jobs: [{ name: "agent", conclusion: "failure", html_url: "https://example.com/build" }] } });
    expect(await getFailedNonBuiltinJobs()).toEqual([{ name: "agent", html_url: "https://example.com/build" }]);
  });

  it("keeps a custom failure with a duplicate display name without linking to the wrong job", async () => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ agent: { result: "failure" }, build: { result: "failure" } }));
    vi.stubEnv("GH_AW_JOB_DISPLAY_NAMES", JSON.stringify({ agent: "Agent", build: "Agent" }));
    global.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue({ data: { jobs: [{ name: "Agent", conclusion: "failure", html_url: "https://example.com/agent" }] } });
    expect(await getFailedNonBuiltinJobs()).toEqual([{ name: "build", html_url: process.env.GH_AW_RUN_URL }]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not uniquely match failed job ID build"));
  });

  it("keeps a failed job ID when its dynamic display name cannot be matched", async () => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ build: { result: "failure" } }));
    expect(await getFailedNonBuiltinJobs()).toEqual([{ name: "build", html_url: process.env.GH_AW_RUN_URL }]);
  });

  it("does not confuse a custom display name with a builtin matrix job", async () => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ agent: { result: "failure" }, build: { result: "failure" } }));
    vi.stubEnv("GH_AW_JOB_DISPLAY_NAMES", JSON.stringify({ agent: "Agent", build: "Agent (linux)" }));
    global.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue({ data: { jobs: [{ name: "Agent (linux)", conclusion: "failure", html_url: "https://example.com/agent" }] } });
    expect(await getFailedNonBuiltinJobs()).toEqual([{ name: "build", html_url: process.env.GH_AW_RUN_URL }]);
  });

  it("reports only failures, preserving renamed, matrix and reusable-job links across pages", async () => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ build: { result: "failure" }, deploy: { result: "failure" }, success: { result: "success" }, cancelled: { result: "cancelled" }, skipped: { result: "skipped" } }));
    vi.stubEnv("GH_AW_JOB_DISPLAY_NAMES", JSON.stringify({ build: "Build project", deploy: "Deploy project" }));
    global.github.rest.actions.listJobsForWorkflowRun.mockResolvedValueOnce({ data: { jobs: Array.from({ length: 100 }, () => ({ name: "Agent", conclusion: "failure", html_url: "https://example.com/agent" })) } }).mockResolvedValueOnce({
      data: {
        jobs: [
          { name: "Build project (linux)", conclusion: "failure", html_url: "https://example.com/linux" },
          { name: "Build project (windows)", conclusion: "failure", html_url: "https://example.com/windows" },
          { name: "Build project (macos)", conclusion: "success", html_url: "https://example.com/macos" },
          { name: "Deploy project / publish", conclusion: "failure", html_url: "https://example.com/deploy" },
        ],
      },
    });
    expect(await getFailedNonBuiltinJobs()).toEqual([
      { name: "Build project (linux)", html_url: "https://example.com/linux" },
      { name: "Build project (windows)", html_url: "https://example.com/windows" },
      { name: "Deploy project / publish", html_url: "https://example.com/deploy" },
    ]);
    expect(global.github.rest.actions.listJobsForWorkflowRun).toHaveBeenNthCalledWith(2, { owner: "owner", repo: "repo", run_id: 123, per_page: 100, page: 2, filter: "latest" });
  });

  it("creates an issue containing custom failures but not the renamed agent", async () => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ agent: { result: "failure" }, build: { result: "failure" } }));
    vi.stubEnv("GH_AW_JOB_DISPLAY_NAMES", JSON.stringify({ agent: "Agent", build: "Build project" }));
    vi.stubEnv("GH_AW_WORKFLOW_NAME", "Smoke Gemini");
    global.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue({
      data: {
        jobs: [
          { name: "Agent", conclusion: "failure", html_url: "https://github.com/owner/repo/actions/runs/123/jobs/456" },
          { name: "Build project", conclusion: "failure", html_url: "https://github.com/owner/repo/actions/runs/123/jobs/789" },
        ],
      },
    });

    await main();
    expect(global.github.rest.issues.create).toHaveBeenCalledWith(
      expect.objectContaining({ title: "[aw] Failed jobs: Smoke Gemini", body: expect.stringContaining("- [`Build project`](https://github.com/owner/repo/actions/runs/123/jobs/789)") })
    );
    expect(global.github.rest.issues.create.mock.calls[0][0].body).not.toContain("https://github.com/owner/repo/actions/runs/123/jobs/456");
  });

  it.each(["push_repo_memory", "push_ledger_changes", "push_evals_state"])("includes persistent git error text in the %s failure issue", async jobId => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ [jobId]: { result: "failure" } }));
    global.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue({
      data: { jobs: [{ id: 789, name: jobId, conclusion: "failure", html_url: "https://github.com/owner/repo/actions/runs/123/job/789" }] },
    });
    global.github.rest.actions.downloadJobLogsForWorkflowRun = vi.fn().mockResolvedValue({
      data: "2026-10-10T00:00:00Z ordinary log output\n2026-10-10T00:00:01Z remote: error: GH013: Repository rule violations\n2026-10-10T00:00:02Z ##[error]Failed to push changes after 11 attempts: non-fast-forward",
    });
    await main();
    const body = global.github.rest.issues.create.mock.calls[0][0].body;
    expect(body).toContain("GH013: Repository rule violations");
    expect(body).toContain("non-fast-forward");
    expect(body).not.toContain("ordinary log output");
    expect(global.github.rest.actions.downloadJobLogsForWorkflowRun).toHaveBeenCalledWith({ owner: "owner", repo: "repo", job_id: 789 });
  });

  it("still reports a state-push failure when its job log is unavailable", async () => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ push_repo_memory: { result: "failure" } }));
    global.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue({
      data: { jobs: [{ id: 789, name: "push_repo_memory", conclusion: "failure", html_url: "https://github.com/owner/repo/actions/runs/123/job/789" }] },
    });
    global.github.rest.actions.downloadJobLogsForWorkflowRun = vi.fn().mockRejectedValue(new Error("403"));
    await main();
    expect(global.github.rest.issues.create).toHaveBeenCalled();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not retrieve error details"));
  });

  it("redacts credentials and neutralizes Markdown from binary job logs", async () => {
    vi.stubEnv("GH_AW_JOB_RESULTS", JSON.stringify({ push_evals_state: { result: "failure" } }));
    global.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue({
      data: { jobs: [{ id: 789, name: "push_evals_state", conclusion: "failure", html_url: null }] },
    });
    const credential = ["user", "password"].join(":");
    global.github.rest.actions.downloadJobLogsForWorkflowRun = vi.fn().mockResolvedValue({
      data: Buffer.from(`fatal: non-fast-forward https://${credential}@example.com/repo Authorization: ${["Basic", "encoded-value"].join(" ")} \`\`\` <!-- @someone -->`),
    });
    await main();
    const body = global.github.rest.issues.create.mock.calls[0][0].body;
    expect(body).toContain("non-fast-forward");
    expect(body).not.toMatch(/password|encoded-value|<!-- @someone -->/);
    expect((body.match(/```/g) || []).length).toBe(2);
  });

  it.each(["", "invalid JSON", "null", "[]"])("rejects invalid job result metadata %j rather than falling back to name-based filtering", async metadata => {
    vi.stubEnv("GH_AW_JOB_RESULTS", metadata);
    await expect(getFailedNonBuiltinJobs()).rejects.toThrow();
    expect(global.github.rest.actions.listJobsForWorkflowRun).not.toHaveBeenCalled();
  });

  it("exports builtin IDs, including legacy aliases", () => {
    expect([...BUILTIN_REPORTED_JOB_IDS]).toEqual(["agent", "conclusion", "activation", "pre_activation", "pre-activation", "safe_outputs", "safe-outputs", "detection"]);
  });
});
