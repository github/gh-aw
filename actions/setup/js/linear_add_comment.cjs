// @ts-check
/// <reference types="@actions/github-script" />

const { sanitizeContent } = require("./sanitize_content.cjs");
const { LINEAR_ISSUE_PATTERN, linearGraphQL } = require("./linear_graphql.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { ERR_API, ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");

const LINEAR_COMMENT_CREATE = `mutation LinearAddComment($input: CommentCreateInput!) {
  commentCreate(input: $input) {
    success
    comment {
      id
      body
    }
  }
}`;

async function main(config = {}) {
  const target = config.target;
  if (typeof target !== "string" || target.length > 100 || !LINEAR_ISSUE_PATTERN.test(target)) {
    throw new Error(`${ERR_CONFIG}: linear_add_comment requires a valid configured target`);
  }

  return async function handleLinearAddComment(item) {
    if (typeof item?.body !== "string" || !item.body.trim()) {
      throw new Error(`${ERR_VALIDATION}: linear_add_comment body is required`);
    }
    if (item.body.length > 65000) {
      throw new Error(`${ERR_VALIDATION}: linear_add_comment body exceeds 65000 characters`);
    }
    const body = sanitizeContent(item.body);
    if (!body.trim()) {
      throw new Error(`${ERR_VALIDATION}: linear_add_comment body is empty after sanitization`);
    }

    if (isStagedMode(config)) {
      logStagedPreviewInfo(`Would add a comment to Linear issue ${target}`);
      return { success: true, staged: true, target };
    }

    const data = await linearGraphQL(LINEAR_COMMENT_CREATE, {
      input: { issueId: target, body },
    });
    const payload = data?.commentCreate;
    if (payload?.success !== true || !payload.comment) {
      throw new Error(`${ERR_API}: Linear commentCreate did not return a successful comment`);
    }
    return { success: true, id: payload.comment.id, target };
  };
}

module.exports = { LINEAR_COMMENT_CREATE, main };
