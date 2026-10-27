// @ts-check
// @safe-outputs-exempt SEC-004
/// <reference types="@actions/github-script" />

const { createReportIssue } = require("./issue_report.cjs");

const fs = require("node:fs");
const { getPromptPath } = require("./messages_core.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const FORECAST_REPORT_PATH = "./.cache/gh-aw/forecast/report.json";
const FORECAST_ERROR_PATH = "./.cache/gh-aw/forecast/error.json";
const FORECAST_ISSUE_TITLE = "[aw] workflow forecast report";
const FORECAST_ERROR_ISSUE_TITLE = "[aw] workflow forecast report (error)";
const FORECAST_ISSUE_TEMPLATE = "forecast_issue.md";

/**
 * @param {unknown} value
 * @returns {string}
 */
function escapeCell(value) {
  return String(value ?? "").replaceAll("|", "\\|");
}

/**
 * @param {unknown} value
 * @returns {string}
 */
function formatAIC(value) {
  const n = Number(value ?? 0);
  if (!Number.isFinite(n) || n <= 0) {
    return "0";
  }
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 }).format(Math.ceil(n));
}

/**
 * @param {unknown} value
 * @returns {number}
 */
function toFiniteNumber(value) {
  const n = Number(value ?? 0);
  return Number.isFinite(n) ? n : 0;
}

/**
 * @param {unknown} value
 * @returns {boolean}
 */
function hasPositiveAIC(value) {
  return toFiniteNumber(value) > 0;
}

/**
 * @param {Record<string, any>} workflow
 * @param {{owner: string, repo: string, serverUrl: string}} options
 * @returns {string}
 */
function renderWorkflowLink(workflow, options) {
  const label = escapeCell(workflow?.workflow_id ?? "");
  const workflowPath = typeof workflow?.workflow_path === "string" ? workflow.workflow_path.trim() : "";
  if (!workflowPath) {
    return label;
  }
  const repoSlug = `${options.owner}/${options.repo}`;
  return `[${label}](${options.serverUrl}/${repoSlug}/actions/workflows/${encodeURIComponent(workflowPath)})`;
}

/**
 * @param {Record<string, any>} workflow
 * @returns {number}
 */
function monthlyCost(workflow) {
  return Number(workflow?.monthly_monte_carlo?.p50_projected_aic ?? workflow?.monthly_projected_aic ?? 0);
}

/**
 * @param {Record<string, any>} workflow
 * @returns {{low:number,p50:number,high:number,stddev:number}}
 */
function getMonthlyForecastStats(workflow) {
  const monthlyMonteCarlo = workflow?.monthly_monte_carlo;
  const monthlyProjected = workflow?.monthly_projected_aic ?? 0;
  return {
    low: toFiniteNumber(monthlyMonteCarlo?.p10_projected_aic ?? monthlyProjected),
    p50: toFiniteNumber(monthlyMonteCarlo?.p50_projected_aic ?? monthlyProjected),
    high: toFiniteNumber(monthlyMonteCarlo?.p90_projected_aic ?? monthlyProjected),
    stddev: toFiniteNumber(monthlyMonteCarlo?.std_dev_aic ?? 0),
  };
}

/**
 * @param {Record<string, any>} workflow
 * @returns {number}
 */
function getLegacyP50(workflow) {
  return toFiniteNumber(workflow?.monte_carlo?.p50_projected_aic ?? workflow?.projected_aic ?? 0);
}

/**
 * @param {Record<string, any>|null} report
 * @param {{owner: string, repo: string, serverUrl: string, runID?: string, generatedAtISO?: string, outcome?: string, errorMessage?: string}} options
 * @returns {string}
 */
function buildForecastIssueBody(report, options) {
  return renderSizedReport(buildForecastIssueReport(report, options)).body.trim();
}

/**
 * @param {Record<string, any>|null} report
 * @param {{owner: string, repo: string, serverUrl: string, runID?: string, generatedAtISO?: string, outcome?: string, errorMessage?: string}} options
 * @returns {import("./report_ast.cjs").Report}
 */
function buildForecastIssueReport(report, options) {
  /** @param {unknown} value @returns {import("./report_ast.cjs").ReportInline[]} */
  const text = value => [{ type: "text", value: String(value ?? ""), agentTainted: false }];
  /** @param {string} value @returns {import("./report_ast.cjs").ReportBlock} */
  const paragraph = value => ({ type: "paragraph", children: text(value), agentTainted: false });
  /** @param {string} value @returns {import("./report_ast.cjs").ReportBlock} */
  const heading = value => ({ type: "heading", level: 3, children: text(value), agentTainted: false });
  /** @param {Record<string, any>} workflow @returns {import("./report_ast.cjs").ReportInline[]} */
  const workflowLink = workflow => {
    const children = text(workflow?.workflow_id);
    const workflowPath = typeof workflow?.workflow_path === "string" ? workflow.workflow_path.trim() : "";
    return workflowPath ? [{ type: "link", url: `${options.serverUrl}/${options.owner}/${options.repo}/actions/workflows/${encodeURIComponent(workflowPath)}`, children, agentTainted: false }] : children;
  };
  const workflows = Array.isArray(report?.workflows) ? [...report.workflows] : [];
  workflows.sort((a, b) => monthlyCost(b) - monthlyCost(a));

  const categorized = workflows.map(workflow => {
    const p50PerRun = toFiniteNumber(workflow?.p50_aic_per_run);
    const monthly = getMonthlyForecastStats(workflow);
    const hasForecastData = [p50PerRun, monthly.p50, monthly.high, monthly.low].some(hasPositiveAIC);
    return {
      workflow,
      row: [workflowLink(workflow), text(toFiniteNumber(workflow.sampled_runs)), ...[p50PerRun, monthly.low, monthly.p50, monthly.high, monthly.stddev].map(value => text(formatAIC(value)))],
      monthly,
      hasForecastData,
    };
  });
  const tableRows = categorized.filter(item => item.hasForecastData).map(item => item.row);
  const workflowsWithoutData = categorized.filter(item => !item.hasForecastData).map(item => item.workflow);

  // Legacy fallback: derive weekly/monthly from the configured-period P50 when new fields are absent.
  const hasNewFields = workflows.some(w => w?.p50_aic_per_run != null || w?.weekly_projected_aic != null);
  const legacyRows = hasNewFields
    ? null
    : workflows
        .filter(workflow => hasPositiveAIC(getLegacyP50(workflow)))
        .map(workflow => {
          const p50 = getLegacyP50(workflow);
          return [text(workflow.workflow_id), text(toFiniteNumber(workflow.sampled_runs)), text(formatAIC(p50))];
        });
  const legacyNoDataWorkflows = hasNewFields
    ? []
    : workflows.filter(workflow => {
        const p50 = getLegacyP50(workflow);
        return !hasPositiveAIC(p50);
      });

  const allMonthlyZero = tableRows.length > 0 && categorized.filter(item => item.hasForecastData).every(item => item.monthly.p50 === 0);
  const allProjectedZero = legacyRows ? false : allMonthlyZero;
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  let reportTable;
  if (legacyRows) {
    reportTable =
      legacyRows.length > 0
        ? [{ type: "table", headers: ["Workflow", "Sampled runs", "Forecast AIC (P50)"].map(text), rows: legacyRows, agentTainted: false }]
        : [{ type: "paragraph", children: [{ type: "emphasis", children: text("No forecast rows were produced.") }] }];
  } else {
    if (tableRows.length === 0) {
      reportTable = [{ type: "paragraph", children: [{ type: "emphasis", children: text("No forecast rows were produced.") }] }];
    } else {
      const totalMonthly = categorized.filter(item => item.hasForecastData).reduce((sum, item) => sum + item.monthly.p50, 0);
      const dataRows = [...tableRows];
      if (tableRows.length > 1) {
        dataRows.push([[{ type: "strong", children: text("TOTAL") }], [], [], [], [{ type: "strong", children: text(formatAIC(totalMonthly)) }], [], []]);
      }
      reportTable = [{ type: "table", headers: ["Workflow", "Runs", "P50/Run", "Monthly (Low)", "Monthly (P50)", "Monthly (High)", "Monthly (Stdev)"].map(text), rows: dataRows, agentTainted: false }];
    }
  }
  const withoutDataWorkflows = legacyRows ? legacyNoDataWorkflows : workflowsWithoutData;
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const withoutDataSection =
    withoutDataWorkflows.length === 0
      ? []
      : [
          heading("AW without data"),
          { type: "table", headers: ["Workflow", "Runs used"].map(text), rows: withoutDataWorkflows.map(workflow => [workflowLink(workflow), text("0")]), agentTainted: false },
          { type: "list", ordered: false, items: [[paragraph("AIC = 0 is treated as missing data and excluded from forecast computation.")]], agentTainted: false },
        ];

  const repoSlug = `${options.owner}/${options.repo}`;
  const period = report?.period || "month";
  const runID = options.runID || "";
  const runURL = runID ? `${options.serverUrl}/${repoSlug}/actions/runs/${runID}` : "";
  const outcome = (options.outcome || "success").toLowerCase();

  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const reportReadingSection =
    tableRows.length === 0
      ? []
      : [
          heading("How to read this report"),
          {
            type: "list",
            ordered: false,
            items: [
              [paragraph("P50/Run is the median per-run AIC from sampled historical runs.")],
              [paragraph("Monthly (Low/P50/High) are the Monte Carlo P10 / P50 / P90 total-AIC bounds over 30 days.")],
              [paragraph("Monthly (Stdev) is the Monte Carlo standard deviation of the 30-day total-AIC distribution.")],
              [paragraph("Monthly values come from the Monte Carlo distribution and are not a direct P50/Run × runs multiplication.")],
            ],
            agentTainted: false,
          },
        ];

  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const allProjectedZeroNote = allProjectedZero
    ? [
        {
          type: "alert",
          kind: "NOTE",
          children: [
            paragraph("All projected AIC values are 0 even after cache warm-up. This usually means cached run summaries do not include token usage for sampled runs."),
            paragraph("Verify gh aw logs fetched recent runs and that run_summary.json files include token usage."),
          ],
          agentTainted: false,
        },
      ]
    : [];
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const sourceRunLine = runURL
    ? [{ type: "paragraph", children: [{ type: "emphasis", children: [...text("Forecast source run: "), { type: "link", url: runURL, children: text(`#${runID}`), agentTainted: false }, ...text(".")] }], agentTainted: false }]
    : [];
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const errorSection =
    outcome === "success" ? [] : [{ type: "alert", kind: "WARNING", children: [paragraph(`Forecast outcome: ${outcome}.`), paragraph(options.errorMessage || "Forecast computation did not complete successfully.")], agentTainted: false }];

  return {
    type: "report",
    title: outcome === "success" ? FORECAST_ISSUE_TITLE : FORECAST_ERROR_ISSUE_TITLE,
    agentTainted: false,
    children: parseReportMarkdown(fs.readFileSync(getPromptPath(FORECAST_ISSUE_TEMPLATE), "utf8"), {
      context: {
        repository: repoSlug,
        generated_at: options.generatedAtISO || new Date().toISOString(),
        period,
        report_table: reportTable,
        without_data_section: withoutDataSection,
        report_reading_section: reportReadingSection,
        all_projected_zero_note: allProjectedZeroNote,
        run_samples_section: [],
        error_section: errorSection,
        source_run_line: sourceRunLine,
      },
      agentTainted: false,
      minimumHeadingLevel: 2,
    }),
  };
}

/**
 * @param {Record<string, any>|null} report
 * @param {{owner: string, repo: string, serverUrl: string, generatedAtISO?: string}} options
 * @returns {string}
 */
function buildForecastStepSummary(report, options) {
  const workflows = Array.isArray(report?.workflows) ? [...report.workflows] : [];
  workflows.sort((a, b) => monthlyCost(b) - monthlyCost(a));
  const samplesSection = buildRunSamplesSection(workflows, options);
  if (!samplesSection) {
    return "";
  }

  return ["### Workflow run samples", "", samplesSection.trim(), ""].join("\n");
}

/**
 * Builds a collapsed <details> block listing every sampled run used in the forecast.
 * Returns an empty string when no workflow has run samples.
 * @param {Array<Record<string, any>>} workflows
 * @param {{owner: string, repo: string, serverUrl: string}} options
 * @returns {string}
 */
function buildRunSamplesSection(workflows, options) {
  const hasAny = workflows.some(w => Array.isArray(w?.run_samples) && w.run_samples.some(sample => hasPositiveAIC(sample?.aic)));
  if (!hasAny) return "";

  const lines = ["<details>", "<summary>Sampled runs used in computation</summary>", "", "| Workflow | Run ID | Date | AIC |", "| --- | ---: | --- | ---: |"];
  for (const wf of workflows) {
    const samples = Array.isArray(wf?.run_samples) ? wf.run_samples.filter(sample => hasPositiveAIC(sample?.aic)) : [];
    const workflowLabel = renderWorkflowLink(wf, options);
    for (const s of samples) {
      const runID = s?.run_id ?? "";
      const date = s?.date ?? "";
      const aic = formatAIC(s?.aic ?? 0);
      const runURL = typeof s?.run_url === "string" && s.run_url !== "" ? `[#${runID}](${s.run_url})` : `#${runID}`;
      lines.push(`| ${workflowLabel} | ${runURL} | ${date} | ${aic} |`);
    }
  }
  lines.push("", "</details>", "");
  return lines.join("\n");
}

/**
 * @returns {Promise<void>}
 */
async function main() {
  /** @type {Record<string, any>|null} */
  let report = null;
  let outcome = "success";
  let errorMessage = "";
  const stepOutcome = String(process.env.FORECAST_STEP_OUTCOME || "").toLowerCase();
  const forecastStepFailed = stepOutcome !== "" && stepOutcome !== "success";

  if (fs.existsSync(FORECAST_REPORT_PATH)) {
    let reportBody = "";
    try {
      reportBody = fs.readFileSync(FORECAST_REPORT_PATH, "utf8").trim();
    } catch (error) {
      outcome = "error";
      errorMessage = `Failed to read forecast report JSON at ${FORECAST_REPORT_PATH}: ${getErrorMessage(error)}`;
      core.warning(errorMessage);
    }

    if (reportBody) {
      try {
        report = JSON.parse(reportBody);
      } catch (error) {
        outcome = "error";
        errorMessage = `Failed to parse forecast report JSON at ${FORECAST_REPORT_PATH}: ${getErrorMessage(error)}`;
        core.warning(errorMessage);
      }
    } else if (!errorMessage) {
      outcome = "error";
      errorMessage = `Forecast report JSON is empty at ${FORECAST_REPORT_PATH}.`;
      if (forecastStepFailed) {
        outcome = stepOutcome;
        core.info(`${errorMessage} Forecast step outcome was ${stepOutcome}.`);
      } else {
        core.warning(errorMessage);
      }
    }
  } else {
    outcome = "error";
    errorMessage = `Forecast report JSON not found at ${FORECAST_REPORT_PATH}.`;
    if (forecastStepFailed) {
      outcome = stepOutcome;
      core.info(`${errorMessage} Forecast step outcome was ${stepOutcome}.`);
    } else {
      core.warning(errorMessage);
    }
  }

  if (fs.existsSync(FORECAST_ERROR_PATH)) {
    try {
      const errorPayload = JSON.parse(fs.readFileSync(FORECAST_ERROR_PATH, "utf8"));
      outcome = String(errorPayload?.outcome || outcome).toLowerCase();
      if (typeof errorPayload?.message === "string" && errorPayload.message.trim() !== "") {
        errorMessage = errorPayload.message.trim();
      }
    } catch (error) {
      core.warning(`Failed to parse forecast error JSON at ${FORECAST_ERROR_PATH}: ${getErrorMessage(error)}`);
    }
  }

  if (stepOutcome && outcome === "success") {
    if (stepOutcome !== "success") {
      outcome = stepOutcome;
      errorMessage = errorMessage || `Forecast step finished with outcome: ${stepOutcome}.`;
    }
  }

  const isErrorOutcome = outcome !== "success";

  const issueReport = buildForecastIssueReport(report, {
    owner: context.repo.owner,
    repo: context.repo.repo,
    serverUrl: context.serverUrl,
    runID: process.env.GITHUB_RUN_ID || "",
    outcome,
    errorMessage,
  });
  const summary = buildForecastStepSummary(report, {
    owner: context.repo.owner,
    repo: context.repo.repo,
    serverUrl: context.serverUrl,
  });
  if (summary) {
    await core.summary.addRaw(summary).write();
  }

  const createdIssue = await createReportIssue(
    github,
    {
      owner: context.repo.owner,
      repo: context.repo.repo,
      title: isErrorOutcome ? FORECAST_ERROR_ISSUE_TITLE : FORECAST_ISSUE_TITLE,
      labels: ["agentic-workflows"],
    },
    issueReport
  );

  core.info(`Created issue #${createdIssue.data.number}: ${createdIssue.data.html_url}`);
}

module.exports = {
  main,
  buildForecastIssueBody,
  buildForecastStepSummary,
  buildRunSamplesSection,
  formatAIC,
  escapeCell,
  FORECAST_REPORT_PATH,
  FORECAST_ERROR_PATH,
  FORECAST_ISSUE_TITLE,
  FORECAST_ERROR_ISSUE_TITLE,
  FORECAST_ISSUE_TEMPLATE,
};
