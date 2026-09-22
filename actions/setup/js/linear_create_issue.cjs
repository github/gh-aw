// @ts-check
/// <reference types="@actions/github-script" />

const { sanitizeTitle } = require("./sanitize_title.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { linearGraphQL } = require("./linear_graphql.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { ERR_API, ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");
const { appendConfiguredBodyFooter } = require("./body_footer.cjs");

const LINEAR_UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const LINEAR_PROJECT_ID_PATTERN = /^(?:[0-9a-f]{12}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/i;

const LINEAR_CREATE_ISSUE = `mutation LinearCreateIssue($input: IssueCreateInput!) {
  issueCreate(input: $input) {
    success
    issue {
      id
      identifier
      title
    }
  }
}`;

const LINEAR_RESOLVE_PROJECT = `query ResolveLinearProject($slugId: String!) {
  projects(filter: { slugId: { eq: $slugId } }, first: 1) {
    nodes {
      id
    }
  }
}`;

const LINEAR_RESOLVE_TEAM = `query ResolveLinearTeam($after: String) {
  teams(first: 50, after: $after) {
    nodes {
      id
      key
      name
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}`;

async function resolveLinearTeamId(teamIdentifier) {
  const normalizedIdentifier = teamIdentifier.trim().toLowerCase();
  let after = null;
  let nameMatch;

  do {
    const data = await linearGraphQL(LINEAR_RESOLVE_TEAM, { after });
    const teams = data?.teams;
    for (const team of teams?.nodes || []) {
      if (typeof team?.id !== "string" || !LINEAR_UUID_PATTERN.test(team.id)) {
        continue;
      }
      if (typeof team.key === "string" && team.key.toLowerCase() === normalizedIdentifier) {
        return team.id;
      }
      if (!nameMatch && typeof team.name === "string" && team.name.toLowerCase() === normalizedIdentifier) {
        nameMatch = team.id;
      }
    }

    if (teams?.pageInfo?.hasNextPage !== true) {
      return nameMatch;
    }
    if (typeof teams.pageInfo.endCursor !== "string" || !teams.pageInfo.endCursor) {
      throw new Error(`${ERR_API}: Linear teams query returned invalid pagination data`);
    }
    after = teams.pageInfo.endCursor;
  } while (true);
}

async function main(config = {}) {
  const teamId = config.team_id !== undefined ? config.team_id : process.env.LINEAR_TEAM_ID;
  if (typeof teamId !== "string" || !teamId.trim()) {
    throw new Error(`${ERR_CONFIG}: linear_create_issue requires a team ID or identifier from safe-outputs.linear-create-issue.team-id or LINEAR_TEAM_ID`);
  }
  const projectId = config.project_id !== undefined ? config.project_id : process.env.LINEAR_PROJECT_ID || undefined;
  if (projectId !== undefined && (typeof projectId !== "string" || !LINEAR_PROJECT_ID_PATTERN.test(projectId))) {
    throw new Error(`${ERR_CONFIG}: linear_create_issue requires a valid project ID from safe-outputs.linear-create-issue.project-id or LINEAR_PROJECT_ID`);
  }

  let teamResolution;

  return async function handleLinearCreateIssue(item) {
    if (typeof item?.title !== "string" || !item.title.trim()) {
      throw new Error(`${ERR_VALIDATION}: linear_create_issue title is required`);
    }
    if (typeof item?.body !== "string" || !item.body.trim()) {
      throw new Error(`${ERR_VALIDATION}: linear_create_issue body is required`);
    }
    if (item.title.length > 128 || item.body.length > 65000 || item.body.length < 20) {
      throw new Error(`${ERR_VALIDATION}: linear_create_issue content is outside the configured field limits`);
    }

    const title = sanitizeTitle(item.title);
    const description = appendConfiguredBodyFooter(sanitizeContent(item.body), config.body_footer, { maxLength: 65000 });
    if (!title) {
      throw new Error(`${ERR_VALIDATION}: linear_create_issue title is empty after sanitization`);
    }

    if (isStagedMode(config)) {
      logStagedPreviewInfo(`Would create Linear issue "${title}"`);
      return { success: true, staged: true, title };
    }

    if (!teamResolution) {
      teamResolution = (LINEAR_UUID_PATTERN.test(teamId) ? Promise.resolve(teamId) : resolveLinearTeamId(teamId)).catch(error => {
        teamResolution = undefined;
        throw error;
      });
    }
    const resolvedTeamId = await teamResolution;
    if (!resolvedTeamId) {
      throw new Error(`${ERR_CONFIG}: linear_create_issue could not resolve the configured team identifier`);
    }

    const input = { teamId: resolvedTeamId, title, description };
    if (projectId) {
      if (LINEAR_UUID_PATTERN.test(projectId)) {
        input.projectId = projectId;
      } else {
        const projectData = await linearGraphQL(LINEAR_RESOLVE_PROJECT, { slugId: projectId });
        const resolvedProjectId = projectData?.projects?.nodes?.[0]?.id;
        if (typeof resolvedProjectId !== "string" || !LINEAR_UUID_PATTERN.test(resolvedProjectId)) {
          throw new Error(`${ERR_CONFIG}: linear_create_issue could not resolve the configured project ID`);
        }
        input.projectId = resolvedProjectId;
      }
    }
    const data = await linearGraphQL(LINEAR_CREATE_ISSUE, { input });
    const payload = data?.issueCreate;
    if (payload?.success !== true || !payload.issue) {
      throw new Error(`${ERR_API}: Linear issueCreate did not return a successful issue`);
    }
    return {
      success: true,
      id: payload.issue.id,
      identifier: payload.issue.identifier,
      title: payload.issue.title,
    };
  };
}

module.exports = { LINEAR_CREATE_ISSUE, LINEAR_RESOLVE_PROJECT, LINEAR_RESOLVE_TEAM, main };
