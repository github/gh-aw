// @ts-check
/// <reference types="@actions/github-script" />

const { createReportIssue } = require("./issue_report.cjs");

const { getErrorMessage, isRateLimitError } = require("./error_helpers.cjs");
const { resolveExecutionOwnerRepo } = require("./repo_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderSizedReport } = require("./report_size.cjs");

const ISSUE_TITLE = "[aw] agentic status report";
const REPORT_COUNT = 1000;
const HEADING_DEMOTION_LEVELS = 2;
const DEFAULT_REPORT_OUTPUT_DIR = "./.cache/gh-aw/activity-report-logs";

/** @typedef {{ key: string, heading: string, startDate: string, optionalOnRateLimit: boolean }} ActivityRange */

/** @type {ActivityRange[]} */
const REPORT_RANGES = [
  { key: "24h", heading: "Last 24 hours", startDate: "-1d", optionalOnRateLimit: false },
  { key: "7d", heading: "Last 7 days", startDate: "-1w", optionalOnRateLimit: false },
];

/**
 * @param {string} text
 * @returns {boolean}
 */
function hasRateLimitText(text) {
  return /\bapi rate limit\b|\brate limit exceeded\b|\bsecondary rate limit\b|\b429\b/i.test(text);
}

/**
 * Run the logs command for a configured report range.
 *
 * @param {string} bin
 * @param {string[]} prefixArgs
 * @param {string} repoSlug
 * @param {ActivityRange} range
 * @param {string} outputDir
 * @returns {Promise<{ heading: string, body: string, children: import("./report_ast.cjs").ReportBlock[] }>}
 */
async function runRangeReport(bin, prefixArgs, repoSlug, range, outputDir) {
  /** @param {import("./report_ast.cjs").ReportBlock[]} children */
  const section = children => ({
    heading: range.heading,
    children,
    get body() {
      return renderSizedReport({ type: "report", title: ISSUE_TITLE, children }).body.trimEnd();
    },
  });
  /** @param {string} value @returns {import("./report_ast.cjs").ReportBlock} */
  const note = value => ({ type: "paragraph", children: [{ type: "emphasis", children: [{ type: "text", value }] }] });
  const args = [...prefixArgs, "logs", "--repo", repoSlug, "--start-date", range.startDate, "--count", String(REPORT_COUNT), "--output", outputDir, "--format", "markdown"];
  core.info(`Running: ${bin} ${args.join(" ")}`);

  try {
    const result = await exec.getExecOutput(bin, args, { ignoreReturnCode: true });
    const output = `${result.stdout || ""}\n${result.stderr || ""}`.trim();
    const rateLimited = hasRateLimitText(output);

    if (result.exitCode === 0 && result.stdout.trim()) {
      return section(parseReportMarkdown(normalizeReportMarkdown(sanitizeContent(result.stdout.trim())), { minimumHeadingLevel: 3, agentTainted: true }));
    }

    if (rateLimited && range.optionalOnRateLimit) {
      core.warning(`Skipping ${range.heading} report due to GitHub API rate limiting`);
      return section([note("Skipped due to GitHub API rate limiting.")]);
    }

    if (rateLimited) {
      return section([note("Could not generate this section due to GitHub API rate limiting.")]);
    }

    return section([note(`Report command failed (exit code ${result.exitCode}).`), { type: "code_block", language: "", value: output || "No command output was captured." }]);
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    const rateLimited = isRateLimitError(error) || hasRateLimitText(errorMessage);

    if (rateLimited && range.optionalOnRateLimit) {
      core.warning(`Skipping ${range.heading} report due to GitHub API rate limiting`);
      return section([note("Skipped due to GitHub API rate limiting.")]);
    }

    if (rateLimited) {
      return section([note("Could not generate this section due to GitHub API rate limiting.")]);
    }

    return section([note(`Report command failed: ${errorMessage}`)]);
  }
}

/**
 * Normalize report markdown for issue rendering.
 * Demotes headings so top-level report headings start at H3.
 *
 * @param {string} markdown
 * @returns {string}
 */
function normalizeReportMarkdown(markdown) {
  return markdown.replace(/^(#{1,6})\s+/gm, (_, hashes) => {
    const headingLevel = hashes.length;
    const demotedHeadingLevel = Math.min(6, headingLevel + HEADING_DEMOTION_LEVELS);
    return `${"#".repeat(demotedHeadingLevel)} `;
  });
}

/**
 * Generate an agentic workflow activity report issue.
 * @returns {Promise<void>}
 */
async function main() {
  const cmdPrefixStr = process.env.GH_AW_CMD_PREFIX || "gh aw";
  const reportOutputDir = process.env.GH_AW_ACTIVITY_REPORT_OUTPUT_DIR || DEFAULT_REPORT_OUTPUT_DIR;
  const [bin, ...prefixArgs] = cmdPrefixStr.split(" ").filter(Boolean);
  const { owner, repo } = resolveExecutionOwnerRepo();
  const repoSlug = `${owner}/${repo}`;

  core.info(`Generating agentic workflow activity report for ${repoSlug}`);

  const sections = [];
  for (const range of REPORT_RANGES) {
    sections.push(await runRangeReport(bin, prefixArgs, repoSlug, range, reportOutputDir));
  }

  /** @type {import("./report_ast.cjs").Report} */
  const report = {
    type: "report",
    title: ISSUE_TITLE,
    agentTainted: false,
    children: [
      { type: "heading", level: 3, children: [{ type: "text", value: "Agentic workflow activity report", agentTainted: false }] },
      {
        type: "paragraph",
        children: [
          { type: "text", value: "Repository: ", agentTainted: false },
          { type: "code", value: repoSlug, agentTainted: false },
          { type: "soft_break" },
          { type: "text", value: `Generated at: ${new Date().toISOString()}`, agentTainted: false },
        ],
      },
      ...sections.map(/** @returns {import("./report_ast.cjs").ReportBlock} */ section => ({ type: "details", summary: section.heading, children: section.children, agentTainted: false })),
    ],
  };

  const createdIssue = await createReportIssue(
    github,
    {
      owner,
      repo,
      title: ISSUE_TITLE,
      labels: ["agentic-workflows"],
    },
    report
  );

  core.info(`Created issue #${createdIssue.data.number}: ${createdIssue.data.html_url}`);
}

module.exports = { main, hasRateLimitText, runRangeReport, normalizeReportMarkdown };
