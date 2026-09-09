// @ts-check
/// <reference types="@actions/github-script" />

const { sanitizeTitle } = require("./sanitize_title.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { LINEAR_ISSUE_PATTERN, linearGraphQL } = require("./linear_graphql.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { ERR_API, ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");

const LINEAR_UPDATE_ISSUE = `mutation LinearUpdateIssue($id: String!, $input: IssueUpdateInput!) {
  issueUpdate(id: $id, input: $input) {
    success
    issue {
      id
      identifier
      title
    }
  }
}`;

async function main(config = {}) {
  const target = config.target;
  if (typeof target !== "string" || target.length > 100 || !LINEAR_ISSUE_PATTERN.test(target)) {
    throw new Error(`${ERR_CONFIG}: linear_update_issue requires a valid configured target`);
  }
  if (config.allow_title !== true && config.allow_body !== true) {
    throw new Error(`${ERR_CONFIG}: linear_update_issue must enable title or body updates`);
  }

  return async function handleLinearUpdateIssue(item) {
    if (item?.title === undefined && item?.body === undefined) {
      throw new Error(`${ERR_VALIDATION}: linear_update_issue requires title or body`);
    }
    if (item.title !== undefined && config.allow_title !== true) {
      throw new Error(`${ERR_VALIDATION}: linear_update_issue title updates are not enabled`);
    }
    if (item.body !== undefined && config.allow_body !== true) {
      throw new Error(`${ERR_VALIDATION}: linear_update_issue body updates are not enabled`);
    }
    if (item.title !== undefined && (typeof item.title !== "string" || !item.title.trim() || item.title.length > 128)) {
      throw new Error(`${ERR_VALIDATION}: linear_update_issue title must be a non-empty string of at most 128 characters`);
    }
    if (item.body !== undefined && (typeof item.body !== "string" || item.body.length > 65000)) {
      throw new Error(`${ERR_VALIDATION}: linear_update_issue body must be a string of at most 65000 characters`);
    }

    const input = {};
    if (item.title !== undefined) {
      input.title = sanitizeTitle(item.title);
      if (!input.title) {
        throw new Error(`${ERR_VALIDATION}: linear_update_issue title is empty after sanitization`);
      }
    }
    if (item.body !== undefined) {
      input.description = sanitizeContent(item.body);
    }

    if (isStagedMode(config)) {
      logStagedPreviewInfo(`Would update Linear issue ${target}`);
      return { success: true, staged: true, target };
    }

    const data = await linearGraphQL(LINEAR_UPDATE_ISSUE, { id: target, input });
    const payload = data?.issueUpdate;
    if (payload?.success !== true || !payload.issue) {
      throw new Error(`${ERR_API}: Linear issueUpdate did not return a successful issue`);
    }
    return {
      success: true,
      id: payload.issue.id,
      identifier: payload.issue.identifier,
      title: payload.issue.title,
    };
  };
}

module.exports = { LINEAR_UPDATE_ISSUE, main };
