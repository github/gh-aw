// @ts-check

const { createCountGatedHandler } = require("./handler_scaffold.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { createJiraClient, textToADF } = require("./jira_client.cjs");

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

function jiraHandler(handlerType, handle) {
  return createCountGatedHandler({
    handlerType,
    setup: async (_config, _maxCount, isStaged) => {
      core.debug(`${handlerType}: initializing handler (staged=${isStaged})`);
      const client = isStaged ? null : createJiraClient();
      return async message => {
        core.debug(`${handlerType}: processing request`);
        try {
          const result = await handle(message || {}, client, isStaged);
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

const createIssue = jiraHandler("jira_create_issue", async (message, client, isStaged) => {
  const projectKey = requiredString(message.project_key, "project_key");
  const issueType = requiredString(message.issue_type, "issue_type");
  const summary = requiredString(message.summary, "summary");
  const description = optionalString(message.description, "description");

  if (isStaged) {
    logStagedPreviewInfo(`Jira create issue — Project: ${projectKey}; Type: ${issueType}; Summary: ${summary}${description ? `; Description: ${description}` : ""}`);
    return { success: true, staged: true, project_key: projectKey, issue_type: issueType, summary };
  }

  const fields = {
    project: { key: projectKey },
    issuetype: { name: issueType },
    summary,
    ...(description === undefined ? {} : { description: textToADF(description) }),
  };
  const result = await client.request("/issue", { method: "POST", body: { fields } });
  if (!result || typeof result.key !== "string") {
    throw new Error("Jira create issue returned an incomplete response");
  }
  return {
    success: true,
    issue_key: result.key,
    ...(result.id ? { issue_id: String(result.id) } : {}),
    ...(result.self ? { url: String(result.self) } : {}),
    metadata: { issue_key: result.key, ...(result.id ? { issue_id: String(result.id) } : {}) },
  };
});

const updateIssue = jiraHandler("jira_update_issue", async (message, client, isStaged) => {
  const issueKey = requiredString(message.issue_key, "issue_key");
  const summary = optionalString(message.summary, "summary", 255);
  const description = optionalString(message.description, "description");
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

const addComment = jiraHandler("jira_add_comment", async (message, client, isStaged) => {
  const issueKey = requiredString(message.issue_key, "issue_key");
  const body = requiredString(message.body, "body", 32767);

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

const addLabel = jiraHandler("jira_add_label", async (message, client, isStaged) => {
  const issueKey = requiredString(message.issue_key, "issue_key");
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
