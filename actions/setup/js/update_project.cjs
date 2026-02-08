// @ts-check
/// <reference types="@actions/github-script" />

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { loadTemporaryIdMap, resolveIssueNumber, isTemporaryId, normalizeTemporaryId } = require("./temporary_id.cjs");

/**
 * Normalize agent output keys for update_project.
 *
 * Agents sometimes emit camelCase keys even when the schema documents snake_case.
 * We accept a small set of aliases for backward compatibility and resilience.
 *
 * @param {any} value
 * @returns {any}
 */
function normalizeUpdateProjectOutput(value) {
  if (!value || typeof value !== "object") return value;

  const output = { ...value };

  if (output.content_type === undefined && output.contentType !== undefined) output.content_type = output.contentType;
  if (output.content_number === undefined && output.contentNumber !== undefined) output.content_number = output.contentNumber;

  if (output.draft_title === undefined && output.draftTitle !== undefined) output.draft_title = output.draftTitle;
  if (output.draft_body === undefined && output.draftBody !== undefined) output.draft_body = output.draftBody;
  if (output.draft_issue_id === undefined && output.draftIssueId !== undefined) output.draft_issue_id = output.draftIssueId;
  if (output.temporary_id === undefined && output.temporaryId !== undefined) output.temporary_id = output.temporaryId;

  if (output.field_definitions === undefined && output.fieldDefinitions !== undefined) output.field_definitions = output.fieldDefinitions;

  return output;
}

/**
 * Log detailed GraphQL error information
 * @param {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} error - GraphQL error
 * @param {string} operation - Operation description
 */
function logGraphQLError(error, operation) {
  core.info(`GraphQL Error during: ${operation}`);
  core.info(`Message: ${getErrorMessage(error)}`);

  const errorList = Array.isArray(error.errors) ? error.errors : [];
  const hasInsufficientScopes = errorList.some(e => e?.type === "INSUFFICIENT_SCOPES");
  const hasNotFound = errorList.some(e => e?.type === "NOT_FOUND");

  if (hasInsufficientScopes) {
    core.info(
      "This looks like a token permission problem for Projects v2. The GraphQL fields used by update_project require a token with Projects access (classic PAT: scope 'project'; fine-grained PAT: Organization permission 'Projects' and access to the org). Fix: set safe-outputs.update-project.github-token to a secret PAT that can access the target org project."
    );
  } else if (hasNotFound && /projectV2\b/.test(getErrorMessage(error))) {
    core.info(
      "GitHub returned NOT_FOUND for ProjectV2. This can mean either: (1) the project number is wrong for Projects v2, (2) the project is a classic Projects board (not Projects v2), or (3) the token does not have access to that org/user project."
    );
  }

  if (error.errors) {
    core.info(`Errors array (${error.errors.length} error(s)):`);
    error.errors.forEach((err, idx) => {
      core.info(`  [${idx + 1}] ${err.message}`);
      if (err.type) core.info(`      Type: ${err.type}`);
      if (err.path) core.info(`      Path: ${JSON.stringify(err.path)}`);
      if (err.locations) core.info(`      Locations: ${JSON.stringify(err.locations)}`);
    });
  }

  if (error.request) core.info(`Request: ${JSON.stringify(error.request, null, 2)}`);
  if (error.data) core.info(`Response data: ${JSON.stringify(error.data, null, 2)}`);
}
/**
 * Parse project number from URL
 * @param {unknown} projectUrl - Project URL
 * @returns {string} Project number
 */
function parseProjectInput(projectUrl) {
  if (!projectUrl || typeof projectUrl !== "string") {
    throw new Error(`Invalid project input: expected string, got ${typeof projectUrl}. The "project" field is required and must be a full GitHub project URL.`);
  }

  const urlMatch = projectUrl.match(/github\.com\/(?:users|orgs)\/[^/]+\/projects\/(\d+)/);
  if (!urlMatch) {
    throw new Error(`Invalid project URL: "${projectUrl}". The "project" field must be a full GitHub project URL (e.g., https://github.com/orgs/myorg/projects/123).`);
  }

  return urlMatch[1];
}

/**
 * Parse project URL into components
 * @param {unknown} projectUrl - Project URL
 * @returns {{ scope: string, ownerLogin: string, projectNumber: string }} Project info
 */
function parseProjectUrl(projectUrl) {
  if (!projectUrl || typeof projectUrl !== "string") {
    throw new Error(`Invalid project input: expected string, got ${typeof projectUrl}. The "project" field is required and must be a full GitHub project URL.`);
  }

  const match = projectUrl.match(/github\.com\/(users|orgs)\/([^/]+)\/projects\/(\d+)/);
  if (!match) {
    throw new Error(`Invalid project URL: "${projectUrl}". The "project" field must be a full GitHub project URL (e.g., https://github.com/orgs/myorg/projects/123).`);
  }

  return {
    scope: match[1],
    ownerLogin: match[2],
    projectNumber: match[3],
  };
}
/**
 * List accessible Projects v2 for org or user
 * @param {{ scope: string, ownerLogin: string, projectNumber: string }} projectInfo - Project info
 * @param {Object} github - GitHub client (Octokit instance) to use for GraphQL queries
 * @returns {Promise<{ nodes: Array<{ id: string, number: number, title: string, closed?: boolean, url: string }>, totalCount?: number, diagnostics: { rawNodesCount: number, nullNodesCount: number, rawEdgesCount: number, nullEdgeNodesCount: number } }>} List result
 */
async function listAccessibleProjectsV2(projectInfo, github) {
  const baseQuery = `projectsV2(first: 100) {
    totalCount
    nodes {
      id
      number
      title
      closed
      url
    }
    edges {
      node {
        id
        number
        title
        closed
        url
      }
    }
  }`;

  const query =
    projectInfo.scope === "orgs"
      ? `query($login: String!) {
        organization(login: $login) {
          ${baseQuery}
        }
      }`
      : `query($login: String!) {
      user(login: $login) {
        ${baseQuery}
      }
    }`;

  const result = await github.graphql(query, { login: projectInfo.ownerLogin });
  const conn = projectInfo.scope === "orgs" ? result?.organization?.projectsV2 : result?.user?.projectsV2;

  const rawNodes = Array.isArray(conn?.nodes) ? conn.nodes : [];
  const rawEdges = Array.isArray(conn?.edges) ? conn.edges : [];
  const nodeNodes = rawNodes.filter(Boolean);
  const edgeNodes = rawEdges.map(e => e?.node).filter(Boolean);

  const unique = new Map();
  for (const n of [...nodeNodes, ...edgeNodes]) {
    if (n && typeof n.id === "string") {
      unique.set(n.id, n);
    }
  }

  return {
    nodes: Array.from(unique.values()),
    totalCount: conn?.totalCount,
    diagnostics: {
      rawNodesCount: rawNodes.length,
      nullNodesCount: rawNodes.length - nodeNodes.length,
      rawEdgesCount: rawEdges.length,
      nullEdgeNodesCount: rawEdges.filter(e => !e || !e.node).length,
    },
  };
}
/**
 * Summarize list of projects
 * @param {Array<{ number: number, title: string, closed?: boolean }>} projects - Projects list
 * @param {number} [limit=20] - Max number to show
 * @returns {string} Summary string
 */
function summarizeProjectsV2(projects, limit = 20) {
  if (!Array.isArray(projects) || projects.length === 0) {
    return "(none)";
  }

  const normalized = projects
    .filter(p => p && typeof p.number === "number" && typeof p.title === "string")
    .slice(0, limit)
    .map(p => `#${p.number} ${p.closed ? "(closed) " : ""}${p.title}`);

  return normalized.length > 0 ? normalized.join("; ") : "(none)";
}

/**
 * Summarize empty projects list with diagnostics
 * @param {{ totalCount?: number, diagnostics?: { rawNodesCount: number, nullNodesCount: number, rawEdgesCount: number, nullEdgeNodesCount: number } }} list - List result
 * @returns {string} Summary string
 */
function summarizeEmptyProjectsV2List(list) {
  const total = typeof list.totalCount === "number" ? list.totalCount : undefined;
  const d = list?.diagnostics;
  const diag = d ? ` nodes=${d.rawNodesCount} (null=${d.nullNodesCount}), edges=${d.rawEdgesCount} (nullNode=${d.nullEdgeNodesCount})` : "";

  if (typeof total === "number" && total > 0) {
    return `(none; totalCount=${total} but returned 0 readable project nodes${diag}. This often indicates the token can see the org/user but lacks Projects v2 access, or the org enforces SSO and the token is not authorized.)`;
  }

  return `(none${diag})`;
}
/**
 * Resolve a project by number
 * @param {{ scope: string, ownerLogin: string, projectNumber: string }} projectInfo - Project info
 * @param {number} projectNumberInt - Project number
 * @param {Object} github - GitHub client (Octokit instance) to use for GraphQL queries
 * @returns {Promise<{ id: string, number: number, title: string, url: string }>} Project details
 */
async function resolveProjectV2(projectInfo, projectNumberInt, github) {
  try {
    const query =
      projectInfo.scope === "orgs"
        ? `query($login: String!, $number: Int!) {
          organization(login: $login) {
            projectV2(number: $number) {
              id
              number
              title
              url
            }
          }
        }`
        : `query($login: String!, $number: Int!) {
          user(login: $login) {
            projectV2(number: $number) {
              id
              number
              title
              url
            }
          }
        }`;

    const direct = await github.graphql(query, {
      login: projectInfo.ownerLogin,
      number: projectNumberInt,
    });

    const project = projectInfo.scope === "orgs" ? direct?.organization?.projectV2 : direct?.user?.projectV2;

    if (project) return project;
  } catch (error) {
    core.warning(`Direct projectV2(number) query failed; falling back to projectsV2 list search: ${getErrorMessage(error)}`);
  }

  // Wrap fallback query in try-catch to handle transient API errors gracefully
  let list;
  try {
    list = await listAccessibleProjectsV2(projectInfo, github);
  } catch (fallbackError) {
    // Both direct query and fallback list query failed - this could be a transient API error
    const who = projectInfo.scope === "orgs" ? `org ${projectInfo.ownerLogin}` : `user ${projectInfo.ownerLogin}`;
    throw new Error(`Unable to resolve project #${projectNumberInt} for ${who}. Both direct projectV2 query and fallback projectsV2 list query failed. This may be a transient GitHub API error. Error: ${getErrorMessage(fallbackError)}`);
  }

  const nodes = Array.isArray(list.nodes) ? list.nodes : [];
  const found = nodes.find(p => p && typeof p.number === "number" && p.number === projectNumberInt);

  if (found) return found;

  const summary = nodes.length > 0 ? summarizeProjectsV2(nodes) : summarizeEmptyProjectsV2List(list);
  const total = typeof list.totalCount === "number" ? ` (totalCount=${list.totalCount})` : "";
  const who = projectInfo.scope === "orgs" ? `org ${projectInfo.ownerLogin}` : `user ${projectInfo.ownerLogin}`;

  throw new Error(`Project #${projectNumberInt} not found or not accessible for ${who}.${total} Accessible Projects v2: ${summary}`);
}
/**
 * Check if a field name conflicts with unsupported GitHub built-in field types
 * @param {string} fieldName - Original field name
 * @param {string} normalizedFieldName - Normalized field name
 * @returns {boolean} True if field name conflicts with unsupported built-in type
 */
function isUnsupportedBuiltInFieldType(fieldName, normalizedFieldName) {
  // GitHub has built-in field types (e.g., REPOSITORY) that cannot be created or updated via API
  // These field names are reserved and will be automatically created as unsupported built-in types
  const unsupportedBuiltInTypes = ["REPOSITORY"];
  const normalizedUpperFieldName = normalizedFieldName.toUpperCase();

  if (unsupportedBuiltInTypes.includes(normalizedUpperFieldName)) {
    core.warning(
      `Field "${fieldName}" conflicts with unsupported GitHub built-in field type ${normalizedUpperFieldName}. ` +
        `GitHub reserves this field name for built-in functionality that is not available via the API. ` +
        `Please use a different field name (e.g., "repo", "source_repository", "linked_repo") instead.`
    );
    return true;
  }
  return false;
}

/**
 * Check for field type mismatch and handle unsupported built-in types
 * @param {string} fieldName - Original field name
 * @param {any} field - Existing field object
 * @param {string} expectedDataType - Expected field data type
 * @returns {boolean} True if field should be skipped (due to unsupported type)
 */
function checkFieldTypeMismatch(fieldName, field, expectedDataType) {
  if (!field || !field.dataType || !expectedDataType) {
    return false;
  }

  const actualType = field.dataType;
  if (actualType === expectedDataType) {
    return false;
  }

  // GitHub has built-in field types that are not supported for updates
  const unsupportedBuiltInTypes = ["REPOSITORY"];

  // Special handling for unsupported built-in types
  if (unsupportedBuiltInTypes.includes(actualType)) {
    core.warning(
      `Field type mismatch for "${fieldName}": Expected ${expectedDataType} but found ${actualType}. ` +
        `The field "${actualType}" is a GitHub built-in type that is not supported for updates via the API. ` +
        `To fix this, delete the field in the GitHub Projects UI and rename it to avoid conflicts ` +
        `(e.g., use "repo", "source_repository", or "linked_repo" instead of "repository").`
    );
    return true; // Skip this field
  }

  // Regular type mismatch warning
  core.warning(
    `Field type mismatch for "${fieldName}": Expected ${expectedDataType} but found ${actualType}. ` +
      `The field was likely created with the wrong type. To fix this, delete the field in the GitHub Projects UI and let it be recreated, ` +
      `or manually change the field type if supported.`
  );
  return false; // Continue with existing field type
}

/**
 * Find an existing draft issue by title in a project
 * @param {Object} github - GitHub client (Octokit instance)
 * @param {string} projectId - Project ID
 * @param {string} targetTitle - Title to search for
 * @returns {Promise<{id: string} | null>} Draft item or null if not found
 */
async function findExistingDraftByTitle(github, projectId, targetTitle) {
  let hasNextPage = true;
  let endCursor = null;

  while (hasNextPage) {
    const result = await github.graphql(
      `query($projectId: ID!, $after: String) {
        node(id: $projectId) {
          ... on ProjectV2 {
            items(first: 100, after: $after) {
              nodes {
                id
                content {
                  __typename
                  ... on DraftIssue {
                    id
                    title
                  }
                }
              }
              pageInfo {
                hasNextPage
                endCursor
              }
            }
          }
        }
      }`,
      { projectId, after: endCursor }
    );

    const found = result.node.items.nodes.find(item => item.content?.__typename === "DraftIssue" && item.content.title === targetTitle);
    if (found) return found;

    hasNextPage = result.node.items.pageInfo.hasNextPage;
    endCursor = result.node.items.pageInfo.endCursor;
  }

  return null;
}

/**
 * Update a GitHub Project v2
 * @param {any} output - Safe output configuration
 * @param {Map<string, any>} temporaryIdMap - Map of temporary IDs to resolved issue numbers
 * @param {Object} githubClient - GitHub client (Octokit instance) to use for GraphQL queries
 * @returns {Promise<void|{temporaryId?: string, draftItemId?: string}>} Returns undefined for most operations, or an object with temporary ID mapping for draft issue creation
 */
async function updateProject(output, temporaryIdMap = new Map(), githubClient = null) {
  output = normalizeUpdateProjectOutput(output);

  // Use the provided github client, or fall back to the global github object
  // @ts-ignore - global.github is set by setupGlobals() from github-script context
  const github = githubClient || global.github;
  if (!github) {
    throw new Error("GitHub client is required but not provided. Either pass a github client to updateProject() or ensure global.github is set.");
  }
  const { owner, repo } = context.repo;
  const projectInfo = parseProjectUrl(output.project);
  const projectNumberFromUrl = projectInfo.projectNumber;

  const wantsCreateView =
    output?.operation === "create_view" ||
    (output?.view &&
      output?.content_type === undefined &&
      output?.content_number === undefined &&
      output?.issue === undefined &&
      output?.pull_request === undefined &&
      output?.fields === undefined &&
      output?.draft_title === undefined &&
      output?.draft_body === undefined);

  const wantsCreateFields = output?.operation === "create_fields";

  try {
    core.info(`Looking up project #${projectNumberFromUrl} from URL: ${output.project}`);
    core.info("[1/4] Fetching repository information...");

    let repoResult;
    try {
      repoResult = await github.graphql(
        `query($owner: String!, $repo: String!) {
          repository(owner: $owner, name: $repo) {
            id
            owner {
              id
              __typename
            }
          }
        }`,
        { owner, repo }
      );
    } catch (err) {
      // prettier-ignore
      const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
      logGraphQLError(error, "Fetching repository information");
      throw error;
    }

    const repositoryId = repoResult.repository.id;
    const ownerType = repoResult.repository.owner.__typename;
    core.info(`✓ Repository: ${owner}/${repo} (${ownerType})`);

    let viewerLogin;
    try {
      const viewerResult = await github.graphql(`query {
          viewer {
            login
          }
        }`);
      if (viewerResult?.viewer?.login) {
        viewerLogin = viewerResult.viewer.login;
        core.info(`✓ Authenticated as: ${viewerLogin}`);
      }
    } catch (viewerError) {
      core.warning(`Could not resolve token identity (viewer.login): ${getErrorMessage(viewerError)}`);
    }

    // Projects v2 GraphQL API does not work with the default GITHUB_TOKEN.
    // If we are authenticated as github-actions[bot], fail fast with a clear error
    // rather than attempting project resolution and falling back.
    if (viewerLogin === "github-actions[bot]") {
      throw new Error(
        "GitHub Projects v2 operations require a PAT or GitHub App token with Projects access, but this run is authenticated as github-actions[bot] (default GITHUB_TOKEN). " +
          "Fix: set secrets.GH_AW_PROJECT_GITHUB_TOKEN (or configure safe-outputs.update-project.github-token) so the safe-outputs step uses that token for github-script."
      );
    }

    let projectId;
    core.info(`[2/4] Resolving project from URL (scope=${projectInfo.scope}, login=${projectInfo.ownerLogin}, number=${projectNumberFromUrl})...`);
    let resolvedProjectNumber = projectNumberFromUrl;

    try {
      const projectNumberInt = parseInt(projectNumberFromUrl, 10);
      if (!Number.isFinite(projectNumberInt)) {
        throw new Error(`Invalid project number parsed from URL: ${projectNumberFromUrl}`);
      }
      const project = await resolveProjectV2(projectInfo, projectNumberInt, github);
      projectId = project.id;
      resolvedProjectNumber = String(project.number);
      core.info(`✓ Resolved project #${resolvedProjectNumber} (${projectInfo.ownerLogin}) (ID: ${projectId})`);
    } catch (err) {
      // prettier-ignore
      const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
      logGraphQLError(error, "Resolving project from URL");
      throw error;
    }

    if (wantsCreateView) {
      const view = output?.view;
      if (!view || typeof view !== "object") {
        throw new Error('Invalid view. When operation is "create_view", you must provide view: { name, layout, ... }.');
      }

      const name = typeof view.name === "string" ? view.name.trim() : "";
      if (!name) {
        throw new Error('Invalid view.name. When operation is "create_view", view.name is required and must be a non-empty string.');
      }

      const layout = typeof view.layout === "string" ? view.layout.trim() : "";
      if (!layout || !["table", "board", "roadmap"].includes(layout)) {
        throw new Error("Invalid view.layout. Must be one of: table, board, roadmap.");
      }

      const filter = typeof view.filter === "string" ? view.filter : undefined;
      let visibleFields = Array.isArray(view.visible_fields) ? view.visible_fields : undefined;

      if (visibleFields) {
        const invalid = visibleFields.filter(v => typeof v !== "number" || !Number.isFinite(v));
        if (invalid.length > 0) {
          throw new Error(`Invalid view.visible_fields. Must be an array of numbers (field IDs). Invalid values: ${invalid.map(v => JSON.stringify(v)).join(", ")}`);
        }
      }

      if (layout === "roadmap" && visibleFields && visibleFields.length > 0) {
        core.warning('view.visible_fields is not applicable to layout "roadmap"; ignoring.');
        visibleFields = undefined;
      }

      if (typeof view.description === "string" && view.description.trim()) {
        core.warning("view.description is not supported by the GitHub Projects Views API; ignoring.");
      }

      if (typeof github.request !== "function") {
        throw new Error("GitHub client does not support github.request(); cannot call Projects Views REST API.");
      }

      const route = projectInfo.scope === "orgs" ? "POST /orgs/{org}/projectsV2/{project_number}/views" : "POST /users/{user_id}/projectsV2/{project_number}/views";

      const params =
        projectInfo.scope === "orgs"
          ? {
              org: projectInfo.ownerLogin,
              project_number: parseInt(resolvedProjectNumber, 10),
              name,
              layout,
              ...(filter ? { filter } : {}),
              ...(visibleFields ? { visible_fields: visibleFields } : {}),
            }
          : {
              user_id: projectInfo.ownerLogin,
              project_number: parseInt(resolvedProjectNumber, 10),
              name,
              layout,
              ...(filter ? { filter } : {}),
              ...(visibleFields ? { visible_fields: visibleFields } : {}),
            };

      core.info(`[3/4] Creating project view: ${name} (${layout})...`);
      const response = await github.request(route, params);
      const created = response?.data;

      if (created?.id) core.setOutput("view-id", created.id);
      if (created?.url) core.setOutput("view-url", created.url);
      core.info("✓ View created");
      return;
    }

    if (wantsCreateFields) {
      const fieldsConfig = output?.field_definitions;
      if (!fieldsConfig || !Array.isArray(fieldsConfig)) {
        throw new Error('Invalid field_definitions. When operation is "create_fields", you must provide field_definitions as an array.');
      }

      core.info(`[3/4] Creating ${fieldsConfig.length} project field(s)...`);
      const createdFields = [];

      for (const fieldDef of fieldsConfig) {
        const fieldName = typeof fieldDef.name === "string" ? fieldDef.name.trim() : "";
        if (!fieldName) {
          core.warning("Skipping field with missing name");
          continue;
        }

        const dataType = typeof fieldDef.data_type === "string" ? fieldDef.data_type.toUpperCase() : "";
        if (!["DATE", "TEXT", "NUMBER", "SINGLE_SELECT", "ITERATION"].includes(dataType)) {
          core.warning(`Skipping field "${fieldName}" with invalid data_type "${fieldDef.data_type}". Must be one of: DATE, TEXT, NUMBER, SINGLE_SELECT, ITERATION`);
          continue;
        }

        try {
          let field;
          if (dataType === "SINGLE_SELECT") {
            const options = Array.isArray(fieldDef.options) ? fieldDef.options : [];
            if (options.length === 0) {
              core.warning(`Skipping SINGLE_SELECT field "${fieldName}" with no options`);
              continue;
            }

            const singleSelectOptions = options.map(opt => ({
              name: typeof opt === "string" ? opt : String(opt),
              description: "",
              color: "GRAY",
            }));

            field = (
              await github.graphql(
                `mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!, $options: [ProjectV2SingleSelectFieldOptionInput!]!) {
                  createProjectV2Field(input: {
                    projectId: $projectId,
                    name: $name,
                    dataType: $dataType,
                    singleSelectOptions: $options
                  }) {
                    projectV2Field {
                      ... on ProjectV2SingleSelectField {
                        id
                        name
                        dataType
                        options { id name }
                      }
                      ... on ProjectV2Field {
                        id
                        name
                        dataType
                      }
                    }
                  }
                }`,
                { projectId, name: fieldName, dataType, options: singleSelectOptions }
              )
            ).createProjectV2Field.projectV2Field;
          } else {
            field = (
              await github.graphql(
                `mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!) {
                  createProjectV2Field(input: {
                    projectId: $projectId,
                    name: $name,
                    dataType: $dataType
                  }) {
                    projectV2Field {
                      ... on ProjectV2Field {
                        id
                        name
                        dataType
                      }
                    }
                  }
                }`,
                { projectId, name: fieldName, dataType }
              )
            ).createProjectV2Field.projectV2Field;
          }

          createdFields.push({
            id: field.id,
            name: field.name,
            dataType: field.dataType,
          });
          core.info(`✓ Created field: ${field.name} (${field.dataType})`);
        } catch (createError) {
          core.warning(`Failed to create field "${fieldName}": ${getErrorMessage(createError)}`);
        }
      }

      core.setOutput("created-fields", JSON.stringify(createdFields));
      core.info(`✓ Created ${createdFields.length} field(s)`);
      return;
    }

    core.info("[3/4] Processing content (issue/PR/draft) if specified...");
    const hasContentNumber = output.content_number !== undefined && output.content_number !== null;
    const hasIssue = output.issue !== undefined && output.issue !== null;
    const hasPullRequest = output.pull_request !== undefined && output.pull_request !== null;
    const values = [];

    if (hasContentNumber) values.push({ key: "content_number", value: output.content_number });
    if (hasIssue) values.push({ key: "issue", value: output.issue });
    if (hasPullRequest) values.push({ key: "pull_request", value: output.pull_request });

    if (values.length > 1) {
      const uniqueValues = [...new Set(values.map(v => String(v.value)))];
      const list = values.map(v => `${v.key}=${v.value}`).join(", ");
      const descriptor = uniqueValues.length > 1 ? "different values" : `same value "${uniqueValues[0]}"`;
      core.warning(`Multiple content number fields (${descriptor}): ${list}. Using priority content_number > issue > pull_request.`);
    }

    if (hasIssue) core.warning('Field "issue" deprecated; use "content_number" instead.');
    if (hasPullRequest) core.warning('Field "pull_request" deprecated; use "content_number" instead.');

    if (output.content_type === "draft_issue") {
      if (values.length > 0) {
        core.warning('content_number/issue/pull_request is ignored when content_type is "draft_issue".');
      }

      // Extract and normalize temporary_id and draft_issue_id using shared helpers
      const rawTemporaryId = typeof output.temporary_id === "string" ? output.temporary_id.trim() : "";
      const temporaryId = rawTemporaryId.startsWith("#") ? rawTemporaryId.slice(1) : rawTemporaryId;

      const rawDraftIssueId = typeof output.draft_issue_id === "string" ? output.draft_issue_id.trim() : "";
      const draftIssueId = rawDraftIssueId.startsWith("#") ? rawDraftIssueId.slice(1) : rawDraftIssueId;

      // Validate temporary_id format if it uses the auto-generated prefix
      // Allow user-friendly IDs like "draft-1" but enforce format for auto-generated IDs
      if (temporaryId && temporaryId.startsWith("aw_") && !isTemporaryId(temporaryId)) {
        throw new Error(`Invalid temporary_id format: "${temporaryId}". Expected format: aw_ followed by 12 hex characters (e.g., "aw_abc123def456").`);
      }

      // Validate draft_issue_id format if it uses the auto-generated prefix
      // Allow user-friendly IDs like "draft-1" but enforce format for auto-generated IDs
      if (draftIssueId && draftIssueId.startsWith("aw_") && !isTemporaryId(draftIssueId)) {
        throw new Error(`Invalid draft_issue_id format: "${draftIssueId}". Expected format: aw_ followed by 12 hex characters (e.g., "aw_abc123def456").`);
      }

      const draftTitle = typeof output.draft_title === "string" ? output.draft_title.trim() : "";
      const draftBody = typeof output.draft_body === "string" ? output.draft_body : undefined;

      let itemId;
      let resolvedTemporaryId = temporaryId;

      // Mode 1: Update existing draft via draft_issue_id
      if (draftIssueId) {
        // Use draft_issue_id as the temporary ID for the return value
        // This ensures the mapping is preserved when updating existing drafts
        resolvedTemporaryId = draftIssueId;

        // Try to resolve draft_issue_id from temporaryIdMap using normalized ID
        const normalized = normalizeTemporaryId(draftIssueId);
        const resolved = temporaryIdMap.get(normalized);
        if (resolved && resolved.draftItemId) {
          itemId = resolved.draftItemId;
          core.info(`✓ Resolved draft_issue_id "${draftIssueId}" to item ${itemId}`);
        } else {
          // Fall back to title-based lookup if title is provided
          if (draftTitle) {
            const existingDraftItem = await findExistingDraftByTitle(github, projectId, draftTitle);

            if (existingDraftItem) {
              itemId = existingDraftItem.id;
              core.info(`✓ Found draft issue "${draftTitle}" by title fallback`);
            } else {
              throw new Error(`draft_issue_id "${draftIssueId}" not found in temporary ID map and no draft with title "${draftTitle}" found`);
            }
          } else {
            throw new Error(`draft_issue_id "${draftIssueId}" not found in temporary ID map and no draft_title provided for fallback lookup`);
          }
        }
      }
      // Mode 2: Create new draft or find by title
      else {
        if (!draftTitle) {
          throw new Error('Invalid draft_title. When content_type is "draft_issue" and draft_issue_id is not provided, draft_title is required and must be a non-empty string.');
        }

        // Check for existing draft issue with the same title
        const existingDraftItem = await findExistingDraftByTitle(github, projectId, draftTitle);

        if (existingDraftItem) {
          itemId = existingDraftItem.id;
          core.info(`✓ Found existing draft issue "${draftTitle}" - updating fields instead of creating duplicate`);
        } else {
          const result = await github.graphql(
            `mutation($projectId: ID!, $title: String!, $body: String) {
              addProjectV2DraftIssue(input: {
                projectId: $projectId,
                title: $title,
                body: $body
              }) {
                projectItem {
                  id
                }
              }
            }`,
            { projectId, title: draftTitle, body: draftBody }
          );
          itemId = result.addProjectV2DraftIssue.projectItem.id;
          core.info(`✓ Created new draft issue "${draftTitle}"`);

          // Store temporary_id mapping if provided
          if (temporaryId) {
            const normalized = normalizeTemporaryId(temporaryId);
            temporaryIdMap.set(normalized, { draftItemId: itemId });
            core.info(`✓ Stored temporary_id mapping: ${temporaryId} -> ${itemId}`);
          }
        }
      }

      const fieldsToUpdate = output.fields ? { ...output.fields } : {};
      if (Object.keys(fieldsToUpdate).length > 0) {
        const projectFields = (
          await github.graphql(
            "query($projectId: ID!) {\n            node(id: $projectId) {\n              ... on ProjectV2 {\n                fields(first: 20) {\n                  nodes {\n                    ... on ProjectV2Field {\n                      id\n                      name\n                      dataType\n                    }\n                    ... on ProjectV2SingleSelectField {\n                      id\n                      name\n                      dataType\n                      options {\n                        id\n                        name\n                        color\n                      }\n                    }\n                    ... on ProjectV2IterationField {\n                      id\n                      name\n                      dataType\n                      configuration {\n                        iterations {\n                          id\n                          title\n                          startDate\n                          duration\n                        }\n                      }\n                    }\n                  }\n                }\n              }\n            }\n          }",
            { projectId }
          )
        ).node.fields.nodes;
        for (const [fieldName, fieldValue] of Object.entries(fieldsToUpdate)) {
          const normalizedFieldName = fieldName
            .split(/[\s_-]+/)
            .map(word => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
            .join(" ");
          let valueToSet,
            field = projectFields.find(f => f.name.toLowerCase() === normalizedFieldName.toLowerCase());

          // Check if field name conflicts with unsupported built-in types
          if (isUnsupportedBuiltInFieldType(fieldName, normalizedFieldName)) {
            continue;
          }

          // Detect expected field type based on field name and value heuristics
          const datePattern = /^\d{4}-\d{2}-\d{2}$/;
          const isDateField = fieldName.toLowerCase().includes("_date") || fieldName.toLowerCase().includes("date");
          const isTextField = "classification" === fieldName.toLowerCase() || ("string" == typeof fieldValue && fieldValue.includes("|"));
          let expectedDataType;
          if (isDateField && typeof fieldValue === "string" && datePattern.test(fieldValue)) {
            expectedDataType = "DATE";
          } else if (isTextField) {
            expectedDataType = "TEXT";
          } else {
            expectedDataType = "SINGLE_SELECT";
          }

          // Check for type mismatch if field already exists
          if (checkFieldTypeMismatch(fieldName, field, expectedDataType)) {
            continue; // Skip fields with unsupported built-in types
          }

          if (!field)
            if (fieldName.toLowerCase().includes("_date") || fieldName.toLowerCase().includes("date")) {
              // Check if field name suggests it's a date field (e.g., start_date, end_date, due_date)
              // Date field values must match ISO 8601 format (YYYY-MM-DD)
              if (typeof fieldValue === "string" && datePattern.test(fieldValue)) {
                try {
                  field = (
                    await github.graphql(
                      "mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!) {\n                    createProjectV2Field(input: {\n                      projectId: $projectId,\n                      name: $name,\n                      dataType: $dataType\n                    }) {\n                      projectV2Field {\n                        ... on ProjectV2Field {\n                          id\n                          name\n                          dataType\n                        }\n                      }\n                    }\n                  }",
                      { projectId, name: normalizedFieldName, dataType: "DATE" }
                    )
                  ).createProjectV2Field.projectV2Field;
                } catch (createError) {
                  core.warning(`Failed to create date field "${fieldName}": ${getErrorMessage(createError)}`);
                  continue;
                }
              } else {
                core.warning(`Field "${fieldName}" looks like a date field but value "${fieldValue}" is not in YYYY-MM-DD format. Skipping field creation.`);
                continue;
              }
            } else if ("classification" === fieldName.toLowerCase() || ("string" == typeof fieldValue && fieldValue.includes("|")))
              try {
                field = (
                  await github.graphql(
                    "mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!) {\n                    createProjectV2Field(input: {\n                      projectId: $projectId,\n                      name: $name,\n                      dataType: $dataType\n                    }) {\n                      projectV2Field {\n                        ... on ProjectV2Field {\n                          id\n                          name\n                        }\n                        ... on ProjectV2SingleSelectField {\n                          id\n                          name\n                          options { id name }\n                        }\n                      }\n                    }\n                  }",
                    { projectId, name: normalizedFieldName, dataType: "TEXT" }
                  )
                ).createProjectV2Field.projectV2Field;
              } catch (createError) {
                core.warning(`Failed to create field "${fieldName}": ${getErrorMessage(createError)}`);
                continue;
              }
            else
              try {
                field = (
                  await github.graphql(
                    "mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!, $options: [ProjectV2SingleSelectFieldOptionInput!]!) {\n                    createProjectV2Field(input: {\n                      projectId: $projectId,\n                      name: $name,\n                      dataType: $dataType,\n                      singleSelectOptions: $options\n                    }) {\n                      projectV2Field {\n                        ... on ProjectV2SingleSelectField {\n                          id\n                          name\n                          options { id name }\n                        }\n                        ... on ProjectV2Field {\n                          id\n                          name\n                        }\n                      }\n                    }\n                  }",
                    { projectId, name: normalizedFieldName, dataType: "SINGLE_SELECT", options: [{ name: String(fieldValue), description: "", color: "GRAY" }] }
                  )
                ).createProjectV2Field.projectV2Field;
              } catch (createError) {
                core.warning(`Failed to create field "${fieldName}": ${getErrorMessage(createError)}`);
                continue;
              }
          if (field.dataType === "DATE") valueToSet = { date: String(fieldValue) };
          else if (field.dataType === "NUMBER") {
            // NUMBER fields use ProjectV2FieldValue input type with number property
            // The number value must be a valid float or integer
            // Convert string values to numbers if needed
            const numValue = typeof fieldValue === "number" ? fieldValue : parseFloat(String(fieldValue));
            if (isNaN(numValue)) {
              core.warning(`Invalid number value "${fieldValue}" for field "${fieldName}"`);
              continue;
            }
            valueToSet = { number: numValue };
          } else if (field.dataType === "ITERATION") {
            // ITERATION fields use ProjectV2FieldValue input type with iterationId property
            // The value should match an iteration title or ID
            if (!field.configuration || !field.configuration.iterations) {
              core.warning(`Iteration field "${fieldName}" has no configured iterations`);
              continue;
            }
            // Try to find iteration by title (case-insensitive match)
            const iteration = field.configuration.iterations.find(iter => iter.title.toLowerCase() === String(fieldValue).toLowerCase());
            if (!iteration) {
              const availableIterations = field.configuration.iterations.map(i => i.title).join(", ");
              core.warning(`Iteration "${fieldValue}" not found in field "${fieldName}". Available iterations: ${availableIterations}`);
              continue;
            }
            valueToSet = { iterationId: iteration.id };
          } else if (field.options) {
            let option = field.options.find(o => o.name === fieldValue);
            if (!option) {
              // GitHub's GraphQL API does not support adding new options to existing single-select fields
              // The updateProjectV2Field mutation does not exist - users must add options manually via UI
              const availableOptions = field.options.map(o => o.name).join(", ");
              core.warning(`Option "${fieldValue}" not found in field "${fieldName}". Available options: ${availableOptions}. To add this option, please update the field manually in the GitHub Projects UI.`);
              continue;
            }
            valueToSet = { singleSelectOptionId: option.id };
          } else valueToSet = { text: String(fieldValue) };
          await github.graphql(
            "mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $value: ProjectV2FieldValue!) {\n              updateProjectV2ItemFieldValue(input: {\n                projectId: $projectId,\n                itemId: $itemId,\n                fieldId: $fieldId,\n                value: $value\n              }) {\n                projectV2Item {\n                  id\n                }\n              }\n            }",
            { projectId, itemId, fieldId: field.id, value: valueToSet }
          );
        }
      }

      core.setOutput("item-id", itemId);
      if (resolvedTemporaryId) {
        core.setOutput("temporary-id", resolvedTemporaryId);
      }

      // Return draft item info for handler manager to collect temporary ID mapping
      return {
        temporaryId: resolvedTemporaryId,
        draftItemId: itemId,
      };
    }
    let contentNumber = null;
    if (hasContentNumber || hasIssue || hasPullRequest) {
      const rawContentNumber = hasContentNumber ? output.content_number : hasIssue ? output.issue : output.pull_request;
      const sanitizedContentNumber = null == rawContentNumber ? "" : "number" == typeof rawContentNumber ? rawContentNumber.toString() : String(rawContentNumber).trim();

      if (sanitizedContentNumber) {
        // Try to resolve as temporary ID first
        const resolved = resolveIssueNumber(sanitizedContentNumber, temporaryIdMap);

        if (resolved.wasTemporaryId) {
          if (resolved.errorMessage || !resolved.resolved) {
            throw new Error(`Failed to resolve temporary ID in content_number: ${resolved.errorMessage || "Unknown error"}`);
          }
          core.info(`✓ Resolved temporary ID ${sanitizedContentNumber} to issue #${resolved.resolved.number}`);
          contentNumber = resolved.resolved.number;
        } else {
          // Not a temporary ID - validate as numeric
          if (!/^\d+$/.test(sanitizedContentNumber)) {
            throw new Error(`Invalid content number "${rawContentNumber}". Provide a positive integer or a valid temporary ID (format: aw_XXXXXXXXXXXX).`);
          }
          contentNumber = Number.parseInt(sanitizedContentNumber, 10);
        }
      } else {
        core.warning("Content number field provided but empty; skipping project item update.");
      }
    }
    if (null !== contentNumber) {
      const contentType = "pull_request" === output.content_type ? "PullRequest" : "issue" === output.content_type || output.issue ? "Issue" : "PullRequest",
        contentQuery =
          "Issue" === contentType
            ? "query($owner: String!, $repo: String!, $number: Int!) {\n            repository(owner: $owner, name: $repo) {\n              issue(number: $number) {\n                id\n              }\n            }\n          }"
            : "query($owner: String!, $repo: String!, $number: Int!) {\n            repository(owner: $owner, name: $repo) {\n              pullRequest(number: $number) {\n                id\n              }\n            }\n          }",
        contentResult = await github.graphql(contentQuery, { owner, repo, number: contentNumber }),
        contentData = "Issue" === contentType ? contentResult.repository.issue : contentResult.repository.pullRequest,
        contentId = contentData.id,
        existingItem = await (async function (projectId, contentId) {
          let hasNextPage = !0,
            endCursor = null;
          for (; hasNextPage; ) {
            const result = await github.graphql(
                "query($projectId: ID!, $after: String) {\n              node(id: $projectId) {\n                ... on ProjectV2 {\n                  items(first: 100, after: $after) {\n                    nodes {\n                      id\n                      content {\n                        ... on Issue {\n                          id\n                        }\n                        ... on PullRequest {\n                          id\n                        }\n                      }\n                    }\n                    pageInfo {\n                      hasNextPage\n                      endCursor\n                    }\n                  }\n                }\n              }\n            }",
                { projectId, after: endCursor }
              ),
              found = result.node.items.nodes.find(item => item.content && item.content.id === contentId);
            if (found) return found;
            ((hasNextPage = result.node.items.pageInfo.hasNextPage), (endCursor = result.node.items.pageInfo.endCursor));
          }
          return null;
        })(projectId, contentId);
      let itemId;
      if (existingItem) ((itemId = existingItem.id), core.info("✓ Item already on board"));
      else {
        itemId = (
          await github.graphql(
            "mutation($projectId: ID!, $contentId: ID!) {\n            addProjectV2ItemById(input: {\n              projectId: $projectId,\n              contentId: $contentId\n            }) {\n              item {\n                id\n              }\n            }\n          }",
            { projectId, contentId }
          )
        ).addProjectV2ItemById.item.id;
      }
      const fieldsToUpdate = output.fields ? { ...output.fields } : {};
      if (Object.keys(fieldsToUpdate).length > 0) {
        const projectFields = (
          await github.graphql(
            "query($projectId: ID!) {\n            node(id: $projectId) {\n              ... on ProjectV2 {\n                fields(first: 20) {\n                  nodes {\n                    ... on ProjectV2Field {\n                      id\n                      name\n                      dataType\n                    }\n                    ... on ProjectV2SingleSelectField {\n                      id\n                      name\n                      dataType\n                      options {\n                        id\n                        name\n                        color\n                      }\n                    }\n                    ... on ProjectV2IterationField {\n                      id\n                      name\n                      dataType\n                      configuration {\n                        iterations {\n                          id\n                          title\n                          startDate\n                          duration\n                        }\n                      }\n                    }\n                  }\n                }\n              }\n            }\n          }",
            { projectId }
          )
        ).node.fields.nodes;
        for (const [fieldName, fieldValue] of Object.entries(fieldsToUpdate)) {
          const normalizedFieldName = fieldName
            .split(/[\s_-]+/)
            .map(word => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
            .join(" ");
          let valueToSet,
            field = projectFields.find(f => f.name.toLowerCase() === normalizedFieldName.toLowerCase());

          // Check if field name conflicts with unsupported built-in types
          if (isUnsupportedBuiltInFieldType(fieldName, normalizedFieldName)) {
            continue;
          }

          // Detect expected field type based on field name and value heuristics
          const datePattern = /^\d{4}-\d{2}-\d{2}$/;
          const isDateField = fieldName.toLowerCase().includes("_date") || fieldName.toLowerCase().includes("date");
          const isTextField = "classification" === fieldName.toLowerCase() || ("string" == typeof fieldValue && fieldValue.includes("|"));
          let expectedDataType;
          if (isDateField && typeof fieldValue === "string" && datePattern.test(fieldValue)) {
            expectedDataType = "DATE";
          } else if (isTextField) {
            expectedDataType = "TEXT";
          } else {
            expectedDataType = "SINGLE_SELECT";
          }

          // Check for type mismatch if field already exists
          if (checkFieldTypeMismatch(fieldName, field, expectedDataType)) {
            continue; // Skip fields with unsupported built-in types
          }

          if (!field)
            if (fieldName.toLowerCase().includes("_date") || fieldName.toLowerCase().includes("date")) {
              // Check if field name suggests it's a date field (e.g., start_date, end_date, due_date)
              // Date field values must match ISO 8601 format (YYYY-MM-DD)
              if (typeof fieldValue === "string" && datePattern.test(fieldValue)) {
                try {
                  field = (
                    await github.graphql(
                      "mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!) {\n                    createProjectV2Field(input: {\n                      projectId: $projectId,\n                      name: $name,\n                      dataType: $dataType\n                    }) {\n                      projectV2Field {\n                        ... on ProjectV2Field {\n                          id\n                          name\n                          dataType\n                        }\n                      }\n                    }\n                  }",
                      { projectId, name: normalizedFieldName, dataType: "DATE" }
                    )
                  ).createProjectV2Field.projectV2Field;
                } catch (createError) {
                  core.warning(`Failed to create date field "${fieldName}": ${getErrorMessage(createError)}`);
                  continue;
                }
              } else {
                core.warning(`Field "${fieldName}" looks like a date field but value "${fieldValue}" is not in YYYY-MM-DD format. Skipping field creation.`);
                continue;
              }
            } else if ("classification" === fieldName.toLowerCase() || ("string" == typeof fieldValue && fieldValue.includes("|")))
              try {
                field = (
                  await github.graphql(
                    "mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!) {\n                    createProjectV2Field(input: {\n                      projectId: $projectId,\n                      name: $name,\n                      dataType: $dataType\n                    }) {\n                      projectV2Field {\n                        ... on ProjectV2Field {\n                          id\n                          name\n                        }\n                        ... on ProjectV2SingleSelectField {\n                          id\n                          name\n                          options { id name }\n                        }\n                      }\n                    }\n                  }",
                    { projectId, name: normalizedFieldName, dataType: "TEXT" }
                  )
                ).createProjectV2Field.projectV2Field;
              } catch (createError) {
                core.warning(`Failed to create field "${fieldName}": ${getErrorMessage(createError)}`);
                continue;
              }
            else
              try {
                field = (
                  await github.graphql(
                    "mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!, $options: [ProjectV2SingleSelectFieldOptionInput!]!) {\n                    createProjectV2Field(input: {\n                      projectId: $projectId,\n                      name: $name,\n                      dataType: $dataType,\n                      singleSelectOptions: $options\n                    }) {\n                      projectV2Field {\n                        ... on ProjectV2SingleSelectField {\n                          id\n                          name\n                          options { id name }\n                        }\n                        ... on ProjectV2Field {\n                          id\n                          name\n                        }\n                      }\n                    }\n                  }",
                    { projectId, name: normalizedFieldName, dataType: "SINGLE_SELECT", options: [{ name: String(fieldValue), description: "", color: "GRAY" }] }
                  )
                ).createProjectV2Field.projectV2Field;
              } catch (createError) {
                core.warning(`Failed to create field "${fieldName}": ${getErrorMessage(createError)}`);
                continue;
              }
          // Check dataType first to properly handle DATE fields before checking for options
          // This prevents date fields from being misidentified as single-select fields
          if (field.dataType === "DATE") {
            // Date fields use ProjectV2FieldValue input type with date property
            // The date value must be in ISO 8601 format (YYYY-MM-DD) with no time component
            // Unlike other field types that may require IDs, date fields accept the date string directly
            valueToSet = { date: String(fieldValue) };
          } else if (field.dataType === "NUMBER") {
            // NUMBER fields use ProjectV2FieldValue input type with number property
            // The number value must be a valid float or integer
            // Convert string values to numbers if needed
            const numValue = typeof fieldValue === "number" ? fieldValue : parseFloat(String(fieldValue));
            if (isNaN(numValue)) {
              core.warning(`Invalid number value "${fieldValue}" for field "${fieldName}"`);
              continue;
            }
            valueToSet = { number: numValue };
          } else if (field.dataType === "ITERATION") {
            // ITERATION fields use ProjectV2FieldValue input type with iterationId property
            // The value should match an iteration title or ID
            if (!field.configuration || !field.configuration.iterations) {
              core.warning(`Iteration field "${fieldName}" has no configured iterations`);
              continue;
            }
            // Try to find iteration by title (case-insensitive match)
            const iteration = field.configuration.iterations.find(iter => iter.title.toLowerCase() === String(fieldValue).toLowerCase());
            if (!iteration) {
              const availableIterations = field.configuration.iterations.map(i => i.title).join(", ");
              core.warning(`Iteration "${fieldValue}" not found in field "${fieldName}". Available iterations: ${availableIterations}`);
              continue;
            }
            valueToSet = { iterationId: iteration.id };
          } else if (field.options) {
            let option = field.options.find(o => o.name === fieldValue);
            if (!option) {
              // GitHub's GraphQL API does not support adding new options to existing single-select fields
              // The updateProjectV2Field mutation does not exist - users must add options manually via UI
              const availableOptions = field.options.map(o => o.name).join(", ");
              core.warning(`Option "${fieldValue}" not found in field "${fieldName}". Available options: ${availableOptions}. To add this option, please update the field manually in the GitHub Projects UI.`);
              continue;
            }
            valueToSet = { singleSelectOptionId: option.id };
          } else valueToSet = { text: String(fieldValue) };
          await github.graphql(
            "mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $value: ProjectV2FieldValue!) {\n              updateProjectV2ItemFieldValue(input: {\n                projectId: $projectId,\n                itemId: $itemId,\n                fieldId: $fieldId,\n                value: $value\n              }) {\n                projectV2Item {\n                  id\n                }\n              }\n            }",
            { projectId, itemId, fieldId: field.id, value: valueToSet }
          );
        }
      }

      core.setOutput("item-id", itemId);
    }
  } catch (error) {
    if (getErrorMessage(error) && getErrorMessage(error).includes("does not have permission to create projects")) {
      const usingCustomToken = !!process.env.GH_AW_PROJECT_GITHUB_TOKEN;
      core.error(
        `Failed to manage project: ${getErrorMessage(error)}\n\nTroubleshooting:\n  • Create the project manually at https://github.com/orgs/${owner}/projects/new.\n  • Or supply a PAT (classic with project + repo scopes, or fine-grained with Projects: Read+Write) via GH_AW_PROJECT_GITHUB_TOKEN.\n  • Or use a GitHub App with Projects: Read+Write permission.\n  • Ensure the workflow grants projects: write.\n\n` +
          (usingCustomToken ? "GH_AW_PROJECT_GITHUB_TOKEN is set but lacks access." : "Using default GITHUB_TOKEN - this cannot access Projects v2 API. You must configure GH_AW_PROJECT_GITHUB_TOKEN.")
      );
    } else {
      core.error(`Failed to manage project: ${getErrorMessage(error)}`);
    }
    throw error;
  }
}

/**
 * Main entry point - handler factory that returns a message handler function
 * @param {Object} config - Handler configuration
 * @param {number} [config.max] - Maximum number of update_project items to process
 * @param {Array<Object>} [config.views] - Views to create from configuration
 * @param {Array<Object>} [config.field_definitions] - Field definitions to create from configuration
 * @param {Object} githubClient - GitHub client (Octokit instance) to use for API calls
 * @returns {Promise<Function>} Message handler function
 */
async function main(config = {}, githubClient = null) {
  // Use the provided github client, or fall back to the global github object
  // The global github object is available when running via github-script action
  // @ts-ignore - global.github is set by setupGlobals() from github-script context
  const github = githubClient || global.github;

  if (!github) {
    throw new Error("GitHub client is required but not provided. Either pass a github client to main() or ensure global.github is set by github-script action.");
  }

  // Extract configuration
  // Default is intentionally configurable via safe-outputs.update-project.max,
  // but we keep a sane global default to avoid surprising truncation.
  const DEFAULT_MAX_COUNT = 100;
  const rawMax = config?.max;
  const parsedMax = typeof rawMax === "number" ? rawMax : Number(rawMax);
  const maxCount = Number.isFinite(parsedMax) && parsedMax > 0 ? parsedMax : DEFAULT_MAX_COUNT;
  const configuredViews = Array.isArray(config.views) ? config.views : [];
  const configuredFieldDefinitions = Array.isArray(config.field_definitions) ? config.field_definitions : [];

  if (configuredViews.length > 0) {
    core.info(`Found ${configuredViews.length} configured view(s) in frontmatter`);
  }
  if (configuredFieldDefinitions.length > 0) {
    core.info(`Found ${configuredFieldDefinitions.length} configured field definition(s) in frontmatter`);
  }
  core.info(`Max count: ${maxCount}`);

  // Track state
  let processedCount = 0;
  let firstProjectUrl = null;
  let viewsCreated = false;
  let fieldsCreated = false;

  /**
   * Message handler function that processes a single update_project message
   * @param {Object} message - The update_project message to process
   * @param {Map<string, {repo?: string, number?: number, projectUrl?: string, draftItemId?: string}>} temporaryIdMap - Unified map of temporary IDs
   * @param {Object} resolvedTemporaryIds - Plain object version of temporaryIdMap for backward compatibility
   * @returns {Promise<Object>} Result with success/error status, and optionally temporaryId/draftItemId for draft issue creation
   */
  return async function handleUpdateProject(message, temporaryIdMap, resolvedTemporaryIds = {}) {
    message = normalizeUpdateProjectOutput(message);

    // Check max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping update_project: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    try {
      // Validate that project field is explicitly provided in the message
      // The project field is required in agent output messages and must be a full GitHub project URL
      let effectiveProjectUrl = message.project;

      if (!effectiveProjectUrl || typeof effectiveProjectUrl !== "string" || effectiveProjectUrl.trim() === "") {
        const errorMsg = 'Missing required "project" field. The agent must explicitly include the project URL in the output message.';
        core.error(errorMsg);

        // Provide helpful context based on content_type
        if (message.content_type === "draft_issue") {
          core.error('For draft_issue content_type, you must include: {"type": "update_project", "project": "https://github.com/orgs/myorg/projects/42", "content_type": "draft_issue", "draft_title": "...", "fields": {...}}');
        } else if (message.content_type === "issue" || message.content_type === "pull_request") {
          core.error(
            `For ${message.content_type} content_type, you must include: {"type": "update_project", "project": "https://github.com/orgs/myorg/projects/42", "content_type": "${message.content_type}", "content_number": 123, "fields": {...}}`
          );
        } else {
          core.error('Example: {"type": "update_project", "project": "https://github.com/orgs/myorg/projects/42", "content_type": "draft_issue", "draft_title": "Task Title", "fields": {"Status": "Todo"}}');
        }

        return {
          success: false,
          error: errorMsg,
        };
      }

      // Validation passed - increment processed count
      processedCount++;

      // Resolve temporary project ID if present
      if (effectiveProjectUrl && typeof effectiveProjectUrl === "string") {
        // Strip # prefix if present
        const projectStr = effectiveProjectUrl.trim();
        const projectWithoutHash = projectStr.startsWith("#") ? projectStr.substring(1) : projectStr;

        // Check if it's a temporary ID (aw_XXXXXXXXXXXX)
        if (/^aw_[0-9a-f]{12}$/i.test(projectWithoutHash)) {
          // Look up in the unified temporaryIdMap
          const resolved = temporaryIdMap.get(projectWithoutHash.toLowerCase());
          if (resolved && resolved.projectUrl) {
            core.info(`Resolved temporary project ID ${projectStr} to ${resolved.projectUrl}`);
            effectiveProjectUrl = resolved.projectUrl;
          } else {
            throw new Error(`Temporary project ID '${projectStr}' not found. Ensure create_project was called before update_project.`);
          }
        }
      }

      // Create effective message with resolved project URL
      const resolvedMessage = { ...message, project: effectiveProjectUrl };

      // Store the first project URL for view creation
      if (!firstProjectUrl && effectiveProjectUrl) {
        firstProjectUrl = effectiveProjectUrl;
      }

      // Create configured fields once before processing the first message
      // This ensures configured fields exist even if the agent doesn't explicitly emit operation=create_fields.
      if (!fieldsCreated && configuredFieldDefinitions.length > 0 && firstProjectUrl) {
        const operation = typeof resolvedMessage?.operation === "string" ? resolvedMessage.operation : "";
        if (operation !== "create_fields") {
          fieldsCreated = true;
          core.info(`Creating ${configuredFieldDefinitions.length} configured field(s) on project: ${firstProjectUrl}`);

          const fieldsOutput = {
            type: "update_project",
            project: firstProjectUrl,
            operation: "create_fields",
            field_definitions: configuredFieldDefinitions,
          };

          try {
            await updateProject(fieldsOutput, temporaryIdMap, github);
            core.info("✓ Created configured fields");
          } catch (err) {
            // prettier-ignore
            const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
            core.error("Failed to create configured fields");
            logGraphQLError(error, "Creating configured fields");
          }
        }
      }

      // If the agent requests create_fields but omitted field_definitions, fall back to configured definitions.
      const effectiveMessage = { ...resolvedMessage };
      if (effectiveMessage?.operation === "create_fields" && !effectiveMessage.field_definitions && configuredFieldDefinitions.length > 0) {
        effectiveMessage.field_definitions = configuredFieldDefinitions;
      }

      // Process the update_project message
      const updateResult = await updateProject(effectiveMessage, temporaryIdMap, github);

      // After processing the first message, create configured views if any
      // Views are created after the first item is processed to ensure the project exists
      if (!viewsCreated && configuredViews.length > 0 && firstProjectUrl) {
        viewsCreated = true;
        core.info(`Creating ${configuredViews.length} configured view(s) on project: ${firstProjectUrl}`);

        for (let i = 0; i < configuredViews.length; i++) {
          const viewConfig = configuredViews[i];
          try {
            // Create a synthetic output item for view creation
            const viewOutput = {
              type: "update_project",
              project: firstProjectUrl,
              operation: "create_view",
              view: {
                name: viewConfig.name,
                layout: viewConfig.layout,
                filter: viewConfig.filter,
                visible_fields: viewConfig.visible_fields,
                description: viewConfig.description,
              },
            };

            await updateProject(viewOutput, temporaryIdMap, github);
            core.info(`✓ Created view ${i + 1}/${configuredViews.length}: ${viewConfig.name} (${viewConfig.layout})`);
          } catch (err) {
            // prettier-ignore
            const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
            core.error(`Failed to create configured view ${i + 1}: ${viewConfig.name}`);
            logGraphQLError(error, `Creating configured view: ${viewConfig.name}`);
          }
        }
      }

      // Return result with temporary ID mapping if draft issue was created
      const result = {
        success: true,
      };

      // Pass through temporary ID mapping from draft issue creation
      if (updateResult && updateResult.temporaryId && updateResult.draftItemId) {
        result.temporaryId = updateResult.temporaryId;
        result.draftItemId = updateResult.draftItemId;
      }

      return result;
    } catch (err) {
      // prettier-ignore
      const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
      logGraphQLError(error, "update_project");
      return {
        success: false,
        error: getErrorMessage(error),
      };
    }
  };
}

module.exports = { updateProject, parseProjectInput, main };
