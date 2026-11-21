// @ts-check

/** @returns {string} Label used for gh-aw's own issues. */
function getIssueLabel() {
  return process.env.GH_AW_ISSUE_LABEL || "agentic-workflows";
}

/** @returns {string} Search qualifier that treats the label as a single value. */
function getIssueLabelQuery() {
  return `label:"${getIssueLabel().replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

module.exports = { getIssueLabel, getIssueLabelQuery };
