// @ts-check
import { beforeEach, describe, expect, it, vi } from "vitest";

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  summary: {
    addRaw: vi.fn(),
    write: vi.fn(),
  },
};

const mockContext = {
  repo: {
    owner: "octo",
    repo: "repo",
  },
  serverUrl: "https://github.com",
};

global.core = mockCore;
global.context = mockContext;

describe("create_forecast_issue", () => {
  let mockGithub;
  let mockFs;

  beforeEach(() => {
    vi.clearAllMocks();
    vi.resetModules();
    mockCore.summary.addRaw.mockReturnValue(mockCore.summary);
    mockCore.summary.write.mockResolvedValue(undefined);
    process.env.GITHUB_RUN_ID = "123456";
    process.env.GH_AW_PROMPTS_DIR = new URL("../md", import.meta.url).pathname;
    mockFs = {
      existsSync: vi.fn(),
      readFileSync: vi.fn(),
    };
    vi.doMock("node:fs", () => mockFs);
    mockGithub = {
      rest: {
        issues: {
          create: vi.fn().mockResolvedValue({
            data: {
              number: 42,
              html_url: "https://github.com/octo/repo/issues/42",
            },
          }),
        },
      },
    };
    global.github = mockGithub;
  });

  it("renders markdown forecast issue body with pretty AIC and source run footnote", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const body = module.buildForecastIssueBody(
      {
        period: "month",
        workflows: [
          {
            workflow_id: "wf|a",
            workflow_path: ".github/workflows/wf-a.yml",
            engines: ["copilot"],
            sampled_runs: 3,
            p50_aic_per_run: 4000,
            p95_aic_per_run: 8000,
            weekly_monte_carlo: { p50_projected_aic: 12345.6 },
            monthly_monte_carlo: {
              p10_projected_aic: 48000,
              p50_projected_aic: 52000,
              p90_projected_aic: 61000,
              std_dev_aic: 3210,
            },
          },
          {
            workflow_id: "wf-b",
            workflow_path: ".github/workflows/wf-b.yml",
            engines: ["claude", "copilot"],
            sampled_runs: 5,
            p50_aic_per_run: 0,
            p95_aic_per_run: 0,
            weekly_projected_aic: 0,
            monthly_projected_aic: 0,
          },
        ],
      },
      {
        owner: "octo",
        repo: "repo",
        serverUrl: "https://github.com",
        runID: "123456",
        generatedAtISO: "2026-01-01T00:00:00.000Z",
      }
    );

    expect(body).toContain("| Workflow | Runs | P50/Run | Monthly (Low) | Monthly (P50) | Monthly (High) | Monthly (Stdev) |");
    expect(body).toContain("| [wf\\|a](https://github.com/octo/repo/actions/workflows/.github%2Fworkflows%2Fwf-a.yml) | 3 | 4,000 | 48,000 | 52,000 | 61,000 | 3,210 |");
    expect(body).toContain("### AW without data");
    expect(body).toContain("| [wf-b](https://github.com/octo/repo/actions/workflows/.github%2Fworkflows%2Fwf-b.yml) | 0 |");
    expect(body).toContain("AIC = 0 is treated as missing data and excluded from forecast computation.");
    expect(body).toContain("### How to read this report");
    expect(body).toContain("Monte Carlo P10 / P50 / P90 total-AIC bounds");
    expect(body).toContain("Monte Carlo standard deviation");
    expect(body).toContain("Monthly values come from the Monte Carlo distribution");
    expect(body).toContain("*Forecast source run: [\\#123456](https://github.com/octo/repo/actions/runs/123456).*");
    expect(body).toContain("Consult the billing dashboards for accurate usage and charges.");
    expect(body).not.toContain("sampled runs but forecast AIC is 0");
  });

  it("keeps dynamic workflow labels and error details literal rather than importing Markdown", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const body = module.buildForecastIssueBody(
      { workflows: [{ workflow_id: "wf | injected\n\n## Injected", sampled_runs: 1, p50_aic_per_run: 1, monthly_projected_aic: 2 }] },
      {
        owner: "octo",
        repo: "repo",
        serverUrl: "https://github.com",
        outcome: "failure",
        errorMessage: "</details>\n\n## Error injection\n<!-- gh-aw-failure-issue: true -->",
      }
    );
    expect(body).toContain("wf \\| injected \\#\\# Injected");
    expect(body).not.toMatch(/^## (?:Injected|Error injection)/m);
    expect(body).not.toContain("<!-- gh-aw-failure-issue:");
    expect(body).toContain("&lt;/details&gt;");
  });

  it("rounds forecast AIC values up to the next integer", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const body = module.buildForecastIssueBody(
      {
        period: "month",
        workflows: [
          {
            workflow_id: "wf-round",
            sampled_runs: 1,
            p50_aic_per_run: 1.1,
            p95_aic_per_run: 2.01,
            weekly_projected_aic: 3.001,
            monthly_projected_aic: 4.0001,
          },
        ],
      },
      {
        owner: "octo",
        repo: "repo",
        serverUrl: "https://github.com",
        generatedAtISO: "2026-01-01T00:00:00.000Z",
      }
    );

    expect(body).toContain("| wf-round | 1 | 2 | 5 | 5 | 5 | 0 |");
  });

  it("lists workflows without data when every projected AIC is zero", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const body = module.buildForecastIssueBody(
      {
        period: "month",
        workflows: [
          { workflow_id: "wf-1", sampled_runs: 2, projected_aic: 0 },
          { workflow_id: "wf-2", sampled_runs: 0, projected_aic: 0 },
        ],
      },
      {
        owner: "octo",
        repo: "repo",
        serverUrl: "https://github.com",
        generatedAtISO: "2026-01-01T00:00:00.000Z",
      }
    );

    expect(body).toContain("### AW without data");
    expect(body).toContain("| wf-1 | 0 |");
    expect(body).toContain("| wf-2 | 0 |");
    expect(body).not.toContain("All projected AIC values are 0 even after cache warm-up.");
  });

  it("renders run samples section in step summary, not issue body", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const report = {
      period: "month",
      workflows: [
        {
          workflow_id: "wf-c",
          workflow_path: ".github/workflows/wf-c.yml",
          sampled_runs: 2,
          p50_aic_per_run: 1000,
          p95_aic_per_run: 2000,
          weekly_projected_aic: 5000,
          monthly_projected_aic: 20000,
          run_samples: [
            { run_id: 111, date: "2026-01-10", aic: 900 },
            { run_id: 222, date: "2026-01-11", aic: 1100 },
          ],
        },
      ],
    };
    const options = {
      owner: "octo",
      repo: "repo",
      serverUrl: "https://github.com",
      generatedAtISO: "2026-01-01T00:00:00.000Z",
    };
    const body = module.buildForecastIssueBody(report, options);
    const summary = module.buildForecastStepSummary(report, options);

    expect(body).not.toContain("Sampled runs used in computation");
    expect(body).not.toContain("| Workflow | Run ID | Date | AIC |");

    expect(summary).toContain("### Workflow run samples");
    expect(summary).toContain("<details>");
    expect(summary).toContain("Sampled runs used in computation");
    expect(summary).toContain("| [wf-c](https://github.com/octo/repo/actions/workflows/.github%2Fworkflows%2Fwf-c.yml) | #111 | 2026-01-10 | 900 |");
    expect(summary).toContain("| [wf-c](https://github.com/octo/repo/actions/workflows/.github%2Fworkflows%2Fwf-c.yml) | #222 | 2026-01-11 | 1,100 |");
  });

  it("returns empty step summary when no run samples exist", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const summary = module.buildForecastStepSummary(
      {
        period: "month",
        workflows: [
          {
            workflow_id: "wf-c",
            workflow_path: ".github/workflows/wf-c.yml",
            sampled_runs: 2,
            p50_aic_per_run: 1000,
            p95_aic_per_run: 2000,
            weekly_projected_aic: 5000,
            monthly_projected_aic: 20000,
            run_samples: [],
          },
        ],
      },
      {
        owner: "octo",
        repo: "repo",
        serverUrl: "https://github.com",
        generatedAtISO: "2026-01-01T00:00:00.000Z",
      }
    );
    expect(summary).toBe("");
  });

  it("excludes zero-AIC run samples from step summary", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const summary = module.buildForecastStepSummary(
      {
        period: "month",
        workflows: [
          {
            workflow_id: "wf-c",
            workflow_path: ".github/workflows/wf-c.yml",
            run_samples: [
              { run_id: 111, date: "2026-01-10", aic: 0 },
              { run_id: 222, date: "2026-01-11", aic: 1200 },
            ],
          },
        ],
      },
      {
        owner: "octo",
        repo: "repo",
        serverUrl: "https://github.com",
        generatedAtISO: "2026-01-01T00:00:00.000Z",
      }
    );

    expect(summary).toContain("| [wf-c](https://github.com/octo/repo/actions/workflows/.github%2Fworkflows%2Fwf-c.yml) | #222 | 2026-01-11 | 1,200 |");
    expect(summary).not.toContain("| #111 |");
  });

  it("renders TOTAL row when multiple workflows are present", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const body = module.buildForecastIssueBody(
      {
        period: "month",
        workflows: [
          {
            workflow_id: "wf-1",
            sampled_runs: 3,
            p50_aic_per_run: 1000,
            p95_aic_per_run: 2000,
            weekly_projected_aic: 7000,
            monthly_projected_aic: 30000,
          },
          {
            workflow_id: "wf-2",
            sampled_runs: 2,
            p50_aic_per_run: 500,
            p95_aic_per_run: 1000,
            weekly_projected_aic: 3000,
            monthly_projected_aic: 12000,
          },
        ],
      },
      {
        owner: "octo",
        repo: "repo",
        serverUrl: "https://github.com",
        generatedAtISO: "2026-01-01T00:00:00.000Z",
      }
    );

    expect(body).toContain("| **TOTAL** |  |  |  | **42,000** |  |  |");
  });

  it("sorts workflows by monthly cost descending", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const body = module.buildForecastIssueBody(
      {
        period: "month",
        workflows: [
          { workflow_id: "low", sampled_runs: 1, monthly_projected_aic: 100 },
          { workflow_id: "high", sampled_runs: 1, monthly_projected_aic: 5000 },
        ],
      },
      {
        owner: "octo",
        repo: "repo",
        serverUrl: "https://github.com",
        generatedAtISO: "2026-01-01T00:00:00.000Z",
      }
    );
    expect(body.indexOf("| high |")).toBeLessThan(body.indexOf("| low |"));
  });

  it("creates an error issue when report file is missing", async () => {
    mockFs.existsSync.mockReturnValue(false);

    const module = await import("./create_forecast_issue.cjs");
    await module.main();

    expect(mockCore.warning).toHaveBeenCalledWith("Forecast report JSON not found at ./.cache/gh-aw/forecast/report.json.");
    expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
      expect.objectContaining({
        title: module.FORECAST_ERROR_ISSUE_TITLE,
      })
    );
  });

  it("does not emit warning for empty report when forecast step is cancelled", async () => {
    process.env.FORECAST_STEP_OUTCOME = "cancelled";
    mockFs.existsSync.mockReturnValue(true);
    mockFs.readFileSync.mockImplementation(path => {
      if (path === "./.cache/gh-aw/forecast/report.json") {
        return "   ";
      }
      if (path === "./.cache/gh-aw/forecast/error.json") {
        return '{"outcome":"cancelled","message":"Forecast step finished with outcome: cancelled."}';
      }
      return "";
    });

    const module = await import("./create_forecast_issue.cjs");
    await module.main();

    expect(mockCore.warning).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Forecast step outcome was cancelled."));
    expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
      expect.objectContaining({
        title: module.FORECAST_ERROR_ISSUE_TITLE,
      })
    );
  });

  it("renders timeout diagnostics in issue body when outcome is timeout", async () => {
    const module = await import("./create_forecast_issue.cjs");
    const body = module.buildForecastIssueBody(null, {
      owner: "octo",
      repo: "repo",
      serverUrl: "https://github.com",
      runID: "123456",
      outcome: "timeout",
      errorMessage: "Forecast computation timed out after 10 minutes.",
      generatedAtISO: "2026-01-01T00:00:00.000Z",
    });
    expect(body).toContain("Forecast outcome: timeout.");
    expect(body).toContain("Forecast computation timed out after 10 minutes.");
  });
});
