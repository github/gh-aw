// @ts-check

const { createCountGatedHandler } = require("./handler_scaffold.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { createJiraClient, textToADF } = require("./jira_client.cjs");
const { getBodyFooterMessage } = require("./messages_footer.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { SIZE_NOTICE } = require("./report_size.cjs");
const { isTemporaryId, normalizeTemporaryId } = require("./temporary_id.cjs");
const { redactDiagnosticText } = require("./diagnostic_sanitization.cjs");

function normalizeJiraText(value) {
  let current = redactDiagnosticText(value);
  for (let pass = 0; pass < 16; pass++) {
    // ADF text is literal: preserve language characters and entity-looking strings.
    const normalized = current
      .replace(/\r\n?/g, "\n")
      .replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "")
      .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f\u00ad\u034f\u200b-\u200f\u202a-\u202e\u2060-\u2064\u2066-\u2069\ufeff]|\uDB40[\uDC00-\uDC7F]/g, "")
      .normalize("NFC");
    const redacted = redactDiagnosticText(normalized);
    if (redacted === current) return redacted;
    current = redacted;
  }
  throw new TypeError("Jira text contains excessively nested controls");
}

function requiredString(value, field, maxLength = 255) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`${field} must be a non-empty string`);
  }
  const text = value.trim();
  if (text.length > maxLength) {
    throw new Error(`${field} must be at most ${maxLength} characters`);
  }
  return text;
}

function optionalString(value, field, maxLength = 32767) {
  if (value === undefined) {
    return undefined;
  }
  return requiredString(value, field, maxLength);
}

// ADF text is literal; Markdown normalization would corrupt entities and language characters.
/** @param {string} body @param {string | undefined} template @returns {string} */
function appendJiraBodyFooter(body, template) {
  if (!template) return body;
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const runUrl = buildWorkflowRunUrl(context, context.repo);
  const footer = getBodyFooterMessage(template, { workflowName, runUrl }).trimEnd();
  if (!footer) return body;
  const composed = [body.trimEnd(), footer].filter(Boolean).join("\n\n");
  if (composed.length <= 32767) return composed;
  const fitted = [SIZE_NOTICE, footer].join("\n\n");
  if (fitted.length > 32767) throw new Error("Protected Jira footer exceeds the maximum body length of 32767 characters");
  return fitted;
}

function jiraHandler(handlerType, handle) {
  return createCountGatedHandler({
    handlerType,
    setup: async (config, _maxCount, isStaged) => {
      core.debug(`${handlerType}: initializing handler (staged=${isStaged})`);
      const client = isStaged ? null : createJiraClient();
      return async (message, resolvedTemporaryIds = {}) => {
        core.debug(`${handlerType}: processing request`);
        try {
          const result = await handle(message || {}, client, isStaged, config, resolvedTemporaryIds);
          core.debug(`${handlerType}: request completed successfully`);
          return result;
        } catch (error) {
          const errorMessage = error instanceof Error ? error.message : "Jira operation failed";
          core.debug(`${handlerType}: request failed`);
          core.error(errorMessage);
          return { success: false, error: errorMessage };
        }
      };
    },
  });
}

function resolveJiraIssueKey(value, resolvedTemporaryIds, allowStaged) {
  const issueKey = requiredString(value, "issue_key");
  if (!isTemporaryId(issueKey)) {
    return issueKey;
  }
  const normalized = normalizeTemporaryId(issueKey);
  const resolved = resolvedTemporaryIds[normalized];
  if (resolved?.provider === "jira" && resolved.resourceType === "issue" && typeof resolved.issueKey === "string") {
    return resolved.issueKey;
  }
  if (allowStaged && resolved?.provider === "jira" && resolved.resourceType === "issue" && resolved.staged === true) {
    return `#${normalized}`;
  }
  throw new Error(`temporary Jira issue ID '#${normalized}' has not been resolved by jira_create_issue in this run`);
}

const createIssue = jiraHandler("jira_create_issue", async (message, client, isStaged, config, resolvedTemporaryIds) => {
  const temporaryId = requiredString(message.temporary_id, "temporary_id");
  if (!isTemporaryId(temporaryId)) {
    throw new Error("jira_create_issue requires a server-generated temporary_id");
  }
  const normalizedTemporaryId = normalizeTemporaryId(temporaryId);
  if (resolvedTemporaryIds[normalizedTemporaryId]) {
    throw new Error(`temporary_id '${temporaryId}' was already used in this run`);
  }
  const projectKey = requiredString(message.project_key, "project_key");
  const issueType = requiredString(message.issue_type, "issue_type");
  const rawSummary = requiredString(message.summary, "summary");
  const rawDescription = optionalString(message.description, "description");
  const description = rawDescription === undefined ? undefined : requiredString(appendJiraBodyFooter(rawDescription, config.body_footer), "description", 32767);
  const summary = requiredString(normalizeJiraText(rawSummary).replace(/\s+/g, " ").trim(), "summary");
  const normalizedDescription = description === undefined ? undefined : normalizeJiraText(description).trim();
  if (normalizedDescription !== undefined && !normalizedDescription) throw new Error("description must be non-empty after normalization");
  if (normalizedDescription !== undefined) requiredString(normalizedDescription, "description", 32767);

  if (isStaged) {
    logStagedPreviewInfo(`Jira create issue — Project: ${projectKey}; Type: ${issueType}; Summary: ${summary}${normalizedDescription ? `; Description: ${normalizedDescription}` : ""}`);
    return {
      success: true,
      staged: true,
      project_key: projectKey,
      issue_type: issueType,
      summary,
      temporaryId,
      temporaryIdEntry: { provider: "jira", resourceType: "issue", staged: true },
    };
  }

  const fields = {
    project: { key: projectKey },
    issuetype: { name: issueType },
    summary,
    ...(normalizedDescription === undefined ? {} : { description: textToADF(normalizedDescription) }),
  };
  const result = await client.request("/issue", { method: "POST", body: { fields } });
  if (!result || typeof result.key !== "string") {
    throw new Error("Jira create issue returned an incomplete response");
  }
  return {
    success: true,
    issue_key: result.key,
    temporaryId,
    temporaryIdEntry: { provider: "jira", resourceType: "issue", issueKey: result.key },
    ...(result.id ? { issue_id: String(result.id) } : {}),
    ...(result.self ? { url: String(result.self) } : {}),
    metadata: { issue_key: result.key, ...(result.id ? { issue_id: String(result.id) } : {}) },
  };
});

const updateIssue = jiraHandler("jira_update_issue", async (message, client, isStaged, config, resolvedTemporaryIds) => {
  const issueKey = resolveJiraIssueKey(message.issue_key, resolvedTemporaryIds, isStaged);
  const summary = optionalString(message.summary, "summary", 255);
  const rawDescription = optionalString(message.description, "description");
  const description = rawDescription === undefined ? undefined : requiredString(appendJiraBodyFooter(rawDescription, config.body_footer), "description", 32767);
  if (summary === undefined && description === undefined) {
    throw new Error("jira_update_issue requires summary or description");
  }

  if (isStaged) {
    logStagedPreviewInfo(`Jira update issue — Issue: ${issueKey}${summary ? `; Summary: ${summary}` : ""}${description ? `; Description: ${description}` : ""}`);
    return { success: true, staged: true, issue_key: issueKey };
  }

  await client.request(`/issue/${encodeURIComponent(issueKey)}`, {
    method: "PUT",
    body: {
      fields: {
        ...(summary === undefined ? {} : { summary }),
        ...(description === undefined ? {} : { description: textToADF(description) }),
      },
    },
  });
  return { success: true, issue_key: issueKey, metadata: { issue_key: issueKey } };
});

const addComment = jiraHandler("jira_add_comment", async (message, client, isStaged, config, resolvedTemporaryIds) => {
  const issueKey = resolveJiraIssueKey(message.issue_key, resolvedTemporaryIds, isStaged);
  const body = requiredString(appendJiraBodyFooter(requiredString(message.body, "body", 32767), config.body_footer), "body", 32767);

  if (isStaged) {
    logStagedPreviewInfo(`Jira add comment — Issue: ${issueKey}; Body: ${body}`);
    return { success: true, staged: true, issue_key: issueKey };
  }

  const result = await client.request(`/issue/${encodeURIComponent(issueKey)}/comment`, {
    method: "POST",
    body: { body: textToADF(body) },
  });
  return {
    success: true,
    issue_key: issueKey,
    ...(result?.id ? { comment_id: String(result.id) } : {}),
    ...(result?.self ? { url: String(result.self) } : {}),
    metadata: { issue_key: issueKey, ...(result?.id ? { comment_id: String(result.id) } : {}) },
  };
});

const addLabel = jiraHandler("jira_add_label", async (message, client, isStaged, _config, resolvedTemporaryIds) => {
  const issueKey = resolveJiraIssueKey(message.issue_key, resolvedTemporaryIds, isStaged);
  const label = requiredString(message.label, "label");
  if (!/^[A-Za-z0-9_.-]+$/.test(label)) {
    throw new Error("label must contain only letters, numbers, periods, hyphens, and underscores");
  }

  if (isStaged) {
    logStagedPreviewInfo(`Jira add label — Issue: ${issueKey}; Label: ${label}`);
    return { success: true, staged: true, issue_key: issueKey, label };
  }

  await client.request(`/issue/${encodeURIComponent(issueKey)}`, {
    method: "PUT",
    body: { update: { labels: [{ add: label }] } },
  });
  return { success: true, issue_key: issueKey, label, metadata: { issue_key: issueKey, label } };
});

module.exports = { addComment, addLabel, createIssue, updateIssue };
