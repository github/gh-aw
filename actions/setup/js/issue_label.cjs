// @ts-check

/** @returns {string} Label used for gh-aw's own issues. */
function getIssueLabel() {
  return process.env.GH_AW_ISSUE_LABEL || "agentic-workflows";
}

/** @returns {string} Search qualifier that treats the label as a single value. */
function getIssueLabelQuery() {
  return `label:"${getIssueLabel().replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

/**
 * @param {string} owner
 * @param {string} repo
 * @returns {string[]} Labels for a newly published run report.
 */
function getRunIssueLabels(owner, repo) {
  const labels = [getIssueLabel()];
  if (`${owner}/${repo}`.toLowerCase() === "github/gh-aw" && !labels.includes("backlog:run-alerts")) {
    labels.push("backlog:run-alerts");
  }
  return labels;
}

module.exports = { getIssueLabel, getIssueLabelQuery, getRunIssueLabels };
