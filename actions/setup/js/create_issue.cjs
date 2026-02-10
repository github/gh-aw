// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Module-level storage for issues that need copilot assignment
 * This is populated by the create_issue handler when GH_AW_ASSIGN_COPILOT is true
 * and consumed by the handler manager to set the issues_to_assign_copilot output
 * @type {Array<string>}
 */
let issuesToAssignCopilotGlobal = [];

/**
 * Get the list of issues that need copilot assignment
 * @returns {Array<string>} Array of "repo:number" strings
 */
function getIssuesToAssignCopilot() {
  return issuesToAssignCopilotGlobal;
}

/**
 * Reset the list of issues that need copilot assignment
 * Used for testing
 */
function resetIssuesToAssignCopilot() {
  issuesToAssignCopilotGlobal = [];
}

const { sanitizeLabelContent } = require("./sanitize_label_content.cjs");
const { sanitizeTitle, applyTitlePrefix } = require("./sanitize_title.cjs");
const { generateFooter, generateWorkflowIdMarker } = require("./generate_footer.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { generateTemporaryId, isTemporaryId, normalizeTemporaryId, replaceTemporaryIdReferences } = require("./temporary_id.cjs");
const { parseAllowedRepos, getDefaultTargetRepo, validateRepo, parseRepoSlug } = require("./repo_helpers.cjs");
const { removeDuplicateTitleFromDescription } = require("./remove_duplicate_title.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { renderTemplate } = require("./messages_core.cjs");
const { createExpirationLine, addExpirationToFooter } = require("./ephemerals.cjs");
const { MAX_SUB_ISSUES, getSubIssueCount } = require("./sub_issue_helpers.cjs");
const { closeOlderIssues } = require("./close_older_issues.cjs");
const fs = require("fs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_issue";

/** @type {number} Maximum number of sub-issues allowed per parent issue */
const MAX_SUB_ISSUES_PER_PARENT = MAX_SUB_ISSUES;

/** @type {number} Maximum number of parent issues to check when searching */
const MAX_PARENT_ISSUES_TO_CHECK = 10;

/**
 * Searches for an existing parent issue that can accept more sub-issues
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} markerComment - The HTML comment marker to search for
 * @returns {Promise<number|null>} - Parent issue number or null if none found
 */
async function searchForExistingParent(owner, repo, markerComment) {
  try {
    const searchQuery = `repo:${owner}/${repo} is:issue "${markerComment}" in:body`;
    const searchResults = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: MAX_PARENT_ISSUES_TO_CHECK,
      sort: "created",
      order: "desc",
    });

    if (searchResults.data.total_count === 0) {
      return null;
    }

    // Check each found issue to see if it can accept more sub-issues
    for (const issue of searchResults.data.items) {
      core.info(`Found potential parent issue #${issue.number}: ${issue.title}`);

      if (issue.state !== "open") {
        core.info(`Parent issue #${issue.number} is ${issue.state}, skipping`);
        continue;
      }

      const subIssueCount = await getSubIssueCount(owner, repo, issue.number);
      if (subIssueCount === null) {
        continue; // Skip if we couldn't get the count
      }

      if (subIssueCount < MAX_SUB_ISSUES_PER_PARENT) {
        core.info(`Using existing parent issue #${issue.number} (has ${subIssueCount}/${MAX_SUB_ISSUES_PER_PARENT} sub-issues)`);
        return issue.number;
      }

      core.info(`Parent issue #${issue.number} is full (${subIssueCount}/${MAX_SUB_ISSUES_PER_PARENT} sub-issues), skipping`);
    }

    return null;
  } catch (error) {
    core.warning(`Could not search for existing parent issues: ${getErrorMessage(error)}`);
    return null;
  }
}

/**
 * Finds an existing parent issue for a group, or creates a new one if needed
 * @param {object} params - Parameters for finding/creating parent issue
 * @param {string} params.groupId - The group identifier
 * @param {string} params.owner - Repository owner
 * @param {string} params.repo - Repository name
 * @param {string} params.titlePrefix - Title prefix to use
 * @param {string[]} params.labels - Labels to apply to parent issue
 * @param {string} params.workflowName - Workflow name
 * @param {string} params.workflowSourceURL - URL to the workflow source
 * @param {number} [params.expiresHours=0] - Hours until expiration (0 means no expiration)
 * @returns {Promise<number|null>} - Parent issue number or null if creation failed
 */
async function findOrCreateParentIssue({ groupId, owner, repo, titlePrefix, labels, workflowName, workflowSourceURL, expiresHours = 0 }) {
  const markerComment = `<!-- gh-aw-group: ${groupId} -->`;

  // Search for existing parent issue with the group marker
  core.info(`Searching for existing parent issue for group: ${groupId}`);
  const existingParent = await searchForExistingParent(owner, repo, markerComment);
  if (existingParent) {
    return existingParent;
  }

  // No suitable parent issue found, create a new one
  core.info(`Creating new parent issue for group: ${groupId}`);
  try {
    const template = createParentIssueTemplate(groupId, titlePrefix, workflowName, workflowSourceURL, expiresHours);
    const { data: parentIssue } = await github.rest.issues.create({
      owner,
      repo,
      title: template.title,
      body: template.body,
      labels: labels,
    });

    core.info(`Created new parent issue #${parentIssue.number}: ${parentIssue.html_url}`);
    return parentIssue.number;
  } catch (error) {
    core.error(`Failed to create parent issue: ${getErrorMessage(error)}`);
    return null;
  }
}

/**
 * Creates a parent issue template for grouping sub-issues
 * @param {string} groupId - The group identifier (workflow ID)
 * @param {string} titlePrefix - Title prefix to use
 * @param {string} workflowName - Name of the workflow
 * @param {string} workflowSourceURL - URL to the workflow source
 * @param {number} [expiresHours=0] - Hours until expiration (0 means no expiration)
 * @returns {object} - Template with title and body
 */
function createParentIssueTemplate(groupId, titlePrefix, workflowName, workflowSourceURL, expiresHours = 0) {
  const title = `${titlePrefix}${groupId} - Issue Group`;

  // Load issue template
  const issueTemplatePath = "/opt/gh-aw/prompts/issue_group_parent.md";
  const issueTemplate = fs.readFileSync(issueTemplatePath, "utf8");

  // Create template context
  const templateContext = {
    group_id: groupId,
    workflow_name: workflowName,
    workflow_source_url: workflowSourceURL || "#",
  };

  // Render the issue template
  let body = renderTemplate(issueTemplate, templateContext);

  // Add footer with workflow information
  const footer = `\n\n> Workflow: [${workflowName}](${workflowSourceURL})`;

  // Add expiration to footer if configured using ephemerals helper
  const footerWithExpiration = addExpirationToFooter(footer, expiresHours, "Parent Issue");

  body = `${body}${footerWithExpiration}`;

  return { title, body };
}

/**
 * Main handler factory for create_issue
 * Returns a message handler function that processes individual create_issue messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const envLabels = config.labels ? (Array.isArray(config.labels) ? config.labels : config.labels.split(",")).map(label => String(label).trim()).filter(Boolean) : [];
  const envAssignees = config.assignees ? (Array.isArray(config.assignees) ? config.assignees : config.assignees.split(",")).map(assignee => String(assignee).trim()).filter(Boolean) : [];
  const titlePrefix = config.title_prefix ?? "";
  const expiresHours = config.expires ? parseInt(String(config.expires), 10) : 0;
  const maxCount = config.max ?? 10;
  const allowedRepos = parseAllowedRepos(config.allowed_repos);
  const defaultTargetRepo = getDefaultTargetRepo(config);
  const groupEnabled = config.group === true || config.group === "true";
  const closeOlderIssuesEnabled = config.close_older_issues === true || config.close_older_issues === "true";

  // Check if copilot assignment is enabled
  const assignCopilot = process.env.GH_AW_ASSIGN_COPILOT === "true";

  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }
  if (envLabels.length > 0) {
    core.info(`Default labels: ${envLabels.join(", ")}`);
  }
  if (envAssignees.length > 0) {
    core.info(`Default assignees: ${envAssignees.join(", ")}`);
  }
  if (titlePrefix) {
    core.info(`Title prefix: ${titlePrefix}`);
  }
  if (expiresHours > 0) {
    core.info(`Issues expire after: ${expiresHours} hours`);
  }
  core.info(`Max count: ${maxCount}`);
  if (groupEnabled) {
    core.info(`Issue grouping enabled: issues will be grouped as sub-issues`);
  }
  if (closeOlderIssuesEnabled) {
    core.info(`Close older issues enabled: older issues with same workflow-id marker will be closed`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  // Track created issues for outputs
  const createdIssues = [];

  // Map to track temporary_id -> {repo, number} relationships across messages
  const temporaryIdMap = new Map();

  // Cache for parent issue per group ID
  const parentIssueCache = new Map();

  // Extract triggering context for footer generation
  const triggeringIssueNumber = context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined;
  const triggeringPRNumber = context.payload?.pull_request?.number || (context.payload?.issue?.pull_request ? context.payload.issue.number : undefined);
  const triggeringDiscussionNumber = context.payload?.discussion?.number;
  const parentIssueNumber = context.payload?.issue?.number;

  /**
   * Message handler function that processes a single create_issue message
   * @param {Object} message - The create_issue message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status and issue details
   */
  return async function handleCreateIssue(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping create_issue: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    // Merge external resolved temp IDs with our local map
    if (resolvedTemporaryIds) {
      for (const [tempId, resolved] of Object.entries(resolvedTemporaryIds)) {
        if (!temporaryIdMap.has(tempId)) {
          temporaryIdMap.set(tempId, resolved);
        }
      }
    }

    // Determine target repository for this issue
    const itemRepo = message.repo ? String(message.repo).trim() : defaultTargetRepo;

    // Validate the repository is allowed
    const repoValidation = validateRepo(itemRepo, defaultTargetRepo, allowedRepos);
    if (!repoValidation.valid) {
      // When valid is false, error is guaranteed to be non-null
      const errorMessage = repoValidation.error;
      if (!errorMessage) {
        throw new Error("Internal error: repoValidation.error should not be null when valid is false");
      }
      core.warning(`Skipping issue: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }

    // Use the qualified repo from validation (handles bare names like "gh-aw" -> "github/gh-aw")
    const qualifiedItemRepo = repoValidation.qualifiedRepo;

    // Parse the repository slug
    const repoParts = parseRepoSlug(qualifiedItemRepo);
    if (!repoParts) {
      const error = `Invalid repository format '${itemRepo}'. Expected 'owner/repo'.`;
      core.warning(`Skipping issue: ${error}`);
      return {
        success: false,
        error,
      };
    }

    // Get or generate the temporary ID for this issue
    const temporaryId = message.temporary_id ?? generateTemporaryId();
    core.info(`Processing create_issue: title=${message.title}, bodyLength=${message.body?.length ?? 0}, temporaryId=${temporaryId}, repo=${qualifiedItemRepo}`);

    // Resolve parent: check if it's a temporary ID reference
    let effectiveParentIssueNumber;
    let effectiveParentRepo = qualifiedItemRepo; // Default to same repo
    if (message.parent !== undefined) {
      // Strip # prefix if present to allow flexible temporary ID format
      const parentStr = String(message.parent).trim();
      const parentWithoutHash = parentStr.startsWith("#") ? parentStr.substring(1) : parentStr;

      if (isTemporaryId(parentWithoutHash)) {
        // It's a temporary ID, look it up in the map
        const resolvedParent = temporaryIdMap.get(normalizeTemporaryId(parentWithoutHash));
        if (resolvedParent) {
          effectiveParentIssueNumber = resolvedParent.number;
          effectiveParentRepo = resolvedParent.repo;
          core.info(`Resolved parent temporary ID '${message.parent}' to ${effectiveParentRepo}#${effectiveParentIssueNumber}`);
        } else {
          core.warning(`Parent temporary ID '${message.parent}' not found in map. Ensure parent issue is created before sub-issues.`);
        }
      } else {
        // Check if it looks like a malformed temporary ID
        if (parentWithoutHash.startsWith("aw_")) {
          core.warning(`Invalid temporary ID format for parent: '${message.parent}'. Temporary IDs must be in format 'aw_' followed by exactly 12 hexadecimal characters (0-9, a-f). Example: 'aw_abc123def456'`);
        } else {
          // It's a real issue number
          const parsed = parseInt(parentWithoutHash, 10);
          if (!isNaN(parsed)) {
            effectiveParentIssueNumber = parsed;
          } else {
            core.warning(`Invalid parent value: ${message.parent}. Expected either a valid temporary ID (format: aw_XXXXXXXXXXXX where X is a hex digit) or a numeric issue number.`);
          }
        }
      }
    } else {
      // Only use context parent if we're in the same repo as context
      const contextRepo = `${context.repo.owner}/${context.repo.repo}`;
      if (qualifiedItemRepo === contextRepo) {
        effectiveParentIssueNumber = parentIssueNumber;
      }
    }

    // Build labels array
    const labels = [...envLabels, ...(Array.isArray(message.labels) ? message.labels : [])]
      .filter(Boolean)
      .map(label => String(label).trim())
      .filter(Boolean)
      .map(label => sanitizeLabelContent(label))
      .filter(Boolean)
      .map(label => (label.length > 64 ? label.substring(0, 64) : label))
      .filter((label, index, arr) => arr.indexOf(label) === index);

    // Build assignees array (merge config default assignees with message-specific assignees)
    let assignees = [...envAssignees, ...(Array.isArray(message.assignees) ? message.assignees : [])]
      .filter(Boolean)
      .map(assignee => String(assignee).trim())
      .filter(Boolean)
      .filter((assignee, index, arr) => arr.indexOf(assignee) === index);

    // Check if copilot is in the assignees list
    const hasCopilot = assignees.includes("copilot");

    // Filter out "copilot" from assignees - it will be assigned separately using GraphQL
    // Copilot is not a valid GitHub user and must be assigned via the agent assignment API
    assignees = assignees.filter(assignee => assignee !== "copilot");

    let title = message.title?.trim() ?? "";

    // Replace temporary ID references in the body using already-created issues
    let processedBody = replaceTemporaryIdReferences(message.body ?? "", temporaryIdMap, qualifiedItemRepo);

    // Remove duplicate title from description if it starts with a header matching the title
    processedBody = removeDuplicateTitleFromDescription(title, processedBody);

    const bodyLines = processedBody.split("\n");

    if (!title) {
      title = message.body ?? "Agent Output";
    }

    // Sanitize title for Unicode security and remove any duplicate prefixes
    title = sanitizeTitle(title, titlePrefix);

    // Apply title prefix (only if it doesn't already exist)
    title = applyTitlePrefix(title, titlePrefix);

    // Add parent reference
    if (effectiveParentIssueNumber) {
      core.info("Detected issue context, parent issue " + effectiveParentRepo + "#" + effectiveParentIssueNumber);
      // Use full repo reference if cross-repo, short reference if same repo
      if (effectiveParentRepo === qualifiedItemRepo) {
        bodyLines.push(`Related to #${effectiveParentIssueNumber}`);
      } else {
        bodyLines.push(`Related to ${effectiveParentRepo}#${effectiveParentIssueNumber}`);
      }
    }

    const workflowName = process.env.GH_AW_WORKFLOW_NAME ?? "Workflow";
    const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE ?? "";
    const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL ?? "";
    const workflowId = process.env.GH_AW_WORKFLOW_ID ?? "";
    const { runId } = context;
    const githubServer = process.env.GITHUB_SERVER_URL ?? "https://github.com";
    const runUrl = context.payload.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}`;

    // Add tracker-id comment if present
    const trackerIDComment = getTrackerID("markdown");
    if (trackerIDComment) {
      bodyLines.push(trackerIDComment);
    }

    // Generate footer and add expiration using helper
    const footer = addExpirationToFooter(generateFooter(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber).trimEnd(), expiresHours, "Issue");

    bodyLines.push(``, ``, footer);

    // Add standalone workflow-id marker for searchability (consistent with comments)
    if (workflowId) {
      bodyLines.push(``, generateWorkflowIdMarker(workflowId));
    }

    bodyLines.push("");
    const body = bodyLines.join("\n").trim();

    core.info(`Creating issue in ${qualifiedItemRepo} with title: ${title}`);
    core.info(`Labels: ${labels.join(", ")}`);
    if (assignees.length > 0) {
      core.info(`Assignees: ${assignees.join(", ")}`);
    }
    core.info(`Body length: ${body.length}`);

    try {
      const { data: issue } = await github.rest.issues.create({
        owner: repoParts.owner,
        repo: repoParts.repo,
        title,
        body,
        labels,
        assignees,
      });

      core.info(`Created issue ${qualifiedItemRepo}#${issue.number}: ${issue.html_url}`);
      createdIssues.push({ ...issue, _repo: qualifiedItemRepo });

      // Store the mapping of temporary_id -> {repo, number}
      temporaryIdMap.set(normalizeTemporaryId(temporaryId), { repo: qualifiedItemRepo, number: issue.number });
      core.info(`Stored temporary ID mapping: ${temporaryId} -> ${qualifiedItemRepo}#${issue.number}`);

      // Track issue for copilot assignment if needed
      if (hasCopilot && assignCopilot) {
        issuesToAssignCopilotGlobal.push(`${qualifiedItemRepo}:${issue.number}`);
        core.info(`Queued issue ${qualifiedItemRepo}#${issue.number} for copilot assignment`);
      }

      // Close older issues if enabled
      if (closeOlderIssuesEnabled) {
        if (workflowId) {
          core.info(`Attempting to close older issues for ${qualifiedItemRepo}#${issue.number} using workflow-id: ${workflowId}`);
          try {
            const closedIssues = await closeOlderIssues(github, repoParts.owner, repoParts.repo, workflowId, { number: issue.number, html_url: issue.html_url }, workflowName, runUrl);
            if (closedIssues.length > 0) {
              core.info(`Closed ${closedIssues.length} older issue(s)`);
            }
          } catch (error) {
            // Log error but don't fail the workflow
            core.warning(`Failed to close older issues: ${getErrorMessage(error)}`);
          }
        } else {
          core.warning("Close older issues enabled but GH_AW_WORKFLOW_ID environment variable not set - skipping");
        }
      }

      // Handle grouping - find or create parent issue and link sub-issue
      if (groupEnabled && !effectiveParentIssueNumber) {
        // Use workflow name as the group ID
        const groupId = workflowName;
        core.info(`Grouping enabled - finding or creating parent issue for group: ${groupId}`);

        // Check cache first
        let groupParentNumber = parentIssueCache.get(groupId);

        if (!groupParentNumber) {
          // Not in cache, find or create parent
          // Parent issue expires 1 day (24 hours) after sub-issues
          const parentExpiresHours = expiresHours > 0 ? expiresHours + 24 : 0;
          groupParentNumber = await findOrCreateParentIssue({
            groupId,
            owner: repoParts.owner,
            repo: repoParts.repo,
            titlePrefix,
            labels,
            workflowName,
            workflowSourceURL,
            expiresHours: parentExpiresHours,
          });

          if (groupParentNumber) {
            // Cache the parent issue number for this group
            parentIssueCache.set(groupId, groupParentNumber);
          }
        }

        if (groupParentNumber) {
          effectiveParentIssueNumber = groupParentNumber;
          effectiveParentRepo = qualifiedItemRepo;
          core.info(`Using parent issue #${effectiveParentIssueNumber} for group: ${groupId}`);
        } else {
          core.warning(`Failed to find or create parent issue for group: ${groupId}`);
        }
      }

      // Sub-issue linking only works within the same repository
      if (effectiveParentIssueNumber && effectiveParentRepo === qualifiedItemRepo) {
        core.info(`Attempting to link issue #${issue.number} as sub-issue of #${effectiveParentIssueNumber}`);
        try {
          // First, get the node IDs for both parent and child issues
          core.info(`Fetching node ID for parent issue #${effectiveParentIssueNumber}...`);
          const getIssueNodeIdQuery = `
            query($owner: String!, $repo: String!, $issueNumber: Int!) {
              repository(owner: $owner, name: $repo) {
                issue(number: $issueNumber) {
                  id
                }
              }
            }
          `;

          // Get parent issue node ID
          const parentResult = await github.graphql(getIssueNodeIdQuery, {
            owner: repoParts.owner,
            repo: repoParts.repo,
            issueNumber: effectiveParentIssueNumber,
          });
          const parentNodeId = parentResult.repository.issue.id;
          core.info(`Parent issue node ID: ${parentNodeId}`);

          // Get child issue node ID
          core.info(`Fetching node ID for child issue #${issue.number}...`);
          const childResult = await github.graphql(getIssueNodeIdQuery, {
            owner: repoParts.owner,
            repo: repoParts.repo,
            issueNumber: issue.number,
          });
          const childNodeId = childResult.repository.issue.id;
          core.info(`Child issue node ID: ${childNodeId}`);

          // Link the child issue as a sub-issue of the parent
          core.info(`Executing addSubIssue mutation...`);
          const addSubIssueMutation = `
            mutation($issueId: ID!, $subIssueId: ID!) {
              addSubIssue(input: {
                issueId: $issueId,
                subIssueId: $subIssueId
              }) {
                subIssue {
                  id
                  number
                }
              }
            }
          `;

          await github.graphql(addSubIssueMutation, {
            issueId: parentNodeId,
            subIssueId: childNodeId,
          });

          core.info("✓ Successfully linked issue #" + issue.number + " as sub-issue of #" + effectiveParentIssueNumber);
        } catch (error) {
          core.info(`Warning: Could not link sub-issue to parent: ${getErrorMessage(error)}`);
          core.info(`Error details: ${error instanceof Error ? error.stack : String(error)}`);
          // Fallback: add a comment if sub-issue linking fails
          try {
            core.info(`Attempting fallback: adding comment to parent issue #${effectiveParentIssueNumber}...`);
            await github.rest.issues.createComment({
              owner: repoParts.owner,
              repo: repoParts.repo,
              issue_number: effectiveParentIssueNumber,
              body: `Created related issue: #${issue.number}`,
            });
            core.info("✓ Added comment to parent issue #" + effectiveParentIssueNumber + " (sub-issue linking not available)");
          } catch (commentError) {
            core.info(`Warning: Could not add comment to parent issue: ${commentError instanceof Error ? commentError.message : String(commentError)}`);
          }
        }
      } else if (effectiveParentIssueNumber && effectiveParentRepo !== qualifiedItemRepo) {
        core.info(`Skipping sub-issue linking: parent is in different repository (${effectiveParentRepo})`);
      }

      // Return result with temporary ID mapping info
      return {
        success: true,
        repo: qualifiedItemRepo,
        number: issue.number,
        url: issue.html_url,
        temporaryId: temporaryId,
        _repo: qualifiedItemRepo, // For tracking in the closure
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      if (errorMessage.includes("Issues has been disabled in this repository")) {
        core.info(`⚠ Cannot create issue "${title}" in ${qualifiedItemRepo}: Issues are disabled for this repository`);
        core.info("Consider enabling issues in repository settings if you want to create issues automatically");
        return {
          success: false,
          error: "Issues disabled for repository",
        };
      }
      core.error(`✗ Failed to create issue "${title}" in ${qualifiedItemRepo}: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main, createParentIssueTemplate, searchForExistingParent, getSubIssueCount, getIssuesToAssignCopilot, resetIssuesToAssignCopilot };
