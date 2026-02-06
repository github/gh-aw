// @ts-check
/// <reference types="@actions/github-script" />

/** @type {typeof import("fs")} */
const fs = require("fs");
/** @type {typeof import("crypto")} */
const crypto = require("crypto");
const { updateActivationComment } = require("./update_activation_comment.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { addExpirationComment } = require("./expiration_helpers.cjs");
const { removeDuplicateTitleFromDescription } = require("./remove_duplicate_title.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { replaceTemporaryIdReferences } = require("./temporary_id.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { createExpirationLine, generateFooterWithExpiration } = require("./ephemerals.cjs");
const { generateWorkflowIdMarker } = require("./generate_footer.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_pull_request";

/**
 * Generate a patch preview with max 500 lines and 2000 chars for issue body
 * @param {string} patchContent - The full patch content
 * @returns {string} Formatted patch preview
 */
function generatePatchPreview(patchContent) {
  if (!patchContent || !patchContent.trim()) {
    return "";
  }

  const lines = patchContent.split("\n");
  const maxLines = 500;
  const maxChars = 2000;

  // Apply line limit first
  let preview = lines.length <= maxLines ? patchContent : lines.slice(0, maxLines).join("\n");
  const lineTruncated = lines.length > maxLines;

  // Apply character limit
  const charTruncated = preview.length > maxChars;
  if (charTruncated) {
    preview = preview.slice(0, maxChars);
  }

  const truncated = lineTruncated || charTruncated;
  const summary = truncated ? `Show patch preview (${Math.min(maxLines, lines.length)} of ${lines.length} lines)` : `Show patch (${lines.length} lines)`;

  return `\n\n<details><summary>${summary}</summary>\n\n\`\`\`diff\n${preview}${truncated ? "\n... (truncated)" : ""}\n\`\`\`\n\n</details>`;
}

/**
 * Main handler factory for create_pull_request
 * Returns a message handler function that processes individual create_pull_request messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const titlePrefix = config.title_prefix || "";
  const envLabels = config.labels ? (Array.isArray(config.labels) ? config.labels : config.labels.split(",")).map(label => String(label).trim()).filter(label => label) : [];
  const draftDefault = config.draft !== undefined ? config.draft : true;
  const ifNoChanges = config.if_no_changes || "warn";
  const allowEmpty = config.allow_empty || false;
  const autoMerge = config.auto_merge || false;
  const expiresHours = config.expires ? parseInt(String(config.expires), 10) : 0;
  const maxCount = config.max || 1; // PRs are typically limited to 1
  const baseBranch = config.base_branch || "";
  const maxSizeKb = config.max_patch_size ? parseInt(String(config.max_patch_size), 10) : 1024;
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

  // Environment validation - fail early if required variables are missing
  const workflowId = process.env.GH_AW_WORKFLOW_ID;
  if (!workflowId) {
    throw new Error("GH_AW_WORKFLOW_ID environment variable is required");
  }

  if (!baseBranch) {
    throw new Error("base_branch configuration is required");
  }

  // Extract triggering issue number from context (for auto-linking PRs to issues)
  const triggeringIssueNumber = context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined;

  // Check if we're in staged mode
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";

  core.info(`Base branch: ${baseBranch}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }
  if (envLabels.length > 0) {
    core.info(`Default labels: ${envLabels.join(", ")}`);
  }
  if (titlePrefix) {
    core.info(`Title prefix: ${titlePrefix}`);
  }
  core.info(`Draft default: ${draftDefault}`);
  core.info(`If no changes: ${ifNoChanges}`);
  core.info(`Allow empty: ${allowEmpty}`);
  core.info(`Auto-merge: ${autoMerge}`);
  if (expiresHours > 0) {
    core.info(`Pull requests expire after: ${expiresHours} hours`);
  }
  core.info(`Max count: ${maxCount}`);
  core.info(`Max patch size: ${maxSizeKb} KB`);

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single create_pull_request message
   * @param {Object} message - The create_pull_request message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status and PR details
   */
  return async function handleCreatePullRequest(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping create_pull_request: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const pullRequestItem = message;

    core.info(`Processing create_pull_request: title=${pullRequestItem.title || "No title"}, bodyLength=${pullRequestItem.body?.length || 0}`);

    // Resolve and validate target repository
    const repoResult = resolveAndValidateRepo(pullRequestItem, defaultTargetRepo, allowedRepos, "pull request");
    if (!repoResult.success) {
      core.warning(`Skipping pull request: ${repoResult.error}`);
      return {
        success: false,
        error: repoResult.error,
      };
    }
    const { repo: itemRepo, repoParts } = repoResult;
    core.info(`Target repository: ${itemRepo}`);

    // Check if patch file exists and has valid content
    if (!fs.existsSync("/tmp/gh-aw/aw.patch")) {
      // If allow-empty is enabled, we can proceed without a patch file
      if (allowEmpty) {
        core.info("No patch file found, but allow-empty is enabled - will create empty PR");
      } else {
        const message = "No patch file found - cannot create pull request without changes";

        // If in staged mode, still show preview
        if (isStaged) {
          let summaryContent = "## 🎭 Staged Mode: Create Pull Request Preview\n\n";
          summaryContent += "The following pull request would be created if staged mode was disabled:\n\n";
          summaryContent += `**Status:** ⚠️ No patch file found\n\n`;
          summaryContent += `**Message:** ${message}\n\n`;

          // Write to step summary
          await core.summary.addRaw(summaryContent).write();
          core.info("📝 Pull request creation preview written to step summary (no patch file)");
          return { success: true, staged: true };
        }

        switch (ifNoChanges) {
          case "error":
            return { success: false, error: message };

          case "ignore":
            // Silent success - no console output
            return { success: false, skipped: true };

          case "warn":
          default:
            core.warning(message);
            return { success: false, error: message, skipped: true };
        }
      }
    }

    let patchContent = "";
    let isEmpty = true;

    if (fs.existsSync("/tmp/gh-aw/aw.patch")) {
      patchContent = fs.readFileSync("/tmp/gh-aw/aw.patch", "utf8");
      isEmpty = !patchContent || !patchContent.trim();
    }

    // Check for actual error conditions (but allow empty patches as valid noop)
    if (patchContent.includes("Failed to generate patch")) {
      // If allow-empty is enabled, ignore patch errors and proceed
      if (allowEmpty) {
        core.info("Patch file contains error, but allow-empty is enabled - will create empty PR");
        patchContent = "";
        isEmpty = true;
      } else {
        const message = "Patch file contains error message - cannot create pull request without changes";

        // If in staged mode, still show preview
        if (isStaged) {
          let summaryContent = "## 🎭 Staged Mode: Create Pull Request Preview\n\n";
          summaryContent += "The following pull request would be created if staged mode was disabled:\n\n";
          summaryContent += `**Status:** ⚠️ Patch file contains error\n\n`;
          summaryContent += `**Message:** ${message}\n\n`;

          // Write to step summary
          await core.summary.addRaw(summaryContent).write();
          core.info("📝 Pull request creation preview written to step summary (patch error)");
          return { success: true, staged: true };
        }

        switch (ifNoChanges) {
          case "error":
            return { success: false, error: message };

          case "ignore":
            // Silent success - no console output
            return { success: false, skipped: true };

          case "warn":
          default:
            core.warning(message);
            return { success: false, error: message, skipped: true };
        }
      }
    }

    // Validate patch size (unless empty)
    if (!isEmpty) {
      // maxSizeKb is already extracted from config at the top
      const patchSizeBytes = Buffer.byteLength(patchContent, "utf8");
      const patchSizeKb = Math.ceil(patchSizeBytes / 1024);

      core.info(`Patch size: ${patchSizeKb} KB (maximum allowed: ${maxSizeKb} KB)`);

      if (patchSizeKb > maxSizeKb) {
        const message = `Patch size (${patchSizeKb} KB) exceeds maximum allowed size (${maxSizeKb} KB)`;

        // If in staged mode, still show preview with error
        if (isStaged) {
          let summaryContent = "## 🎭 Staged Mode: Create Pull Request Preview\n\n";
          summaryContent += "The following pull request would be created if staged mode was disabled:\n\n";
          summaryContent += `**Status:** ❌ Patch size exceeded\n\n`;
          summaryContent += `**Message:** ${message}\n\n`;

          // Write to step summary
          await core.summary.addRaw(summaryContent).write();
          core.info("📝 Pull request creation preview written to step summary (patch size error)");
          return { success: true, staged: true };
        }

        return { success: false, error: message };
      }

      core.info("Patch size validation passed");
    }

    if (isEmpty && !isStaged && !allowEmpty) {
      const message = "Patch file is empty - no changes to apply (noop operation)";

      switch (ifNoChanges) {
        case "error":
          return { success: false, error: "No changes to push - failing as configured by if-no-changes: error" };

        case "ignore":
          // Silent success - no console output
          return { success: false, skipped: true };

        case "warn":
        default:
          core.warning(message);
          return { success: false, error: message, skipped: true };
      }
    }

    if (!isEmpty) {
      core.info("Patch content validation passed");
    } else if (allowEmpty) {
      core.info("Patch file is empty - processing empty PR creation (allow-empty is enabled)");
    } else {
      core.info("Patch file is empty - processing noop operation");
    }

    // If in staged mode, emit step summary instead of creating PR
    if (isStaged) {
      let summaryContent = "## 🎭 Staged Mode: Create Pull Request Preview\n\n";
      summaryContent += "The following pull request would be created if staged mode was disabled:\n\n";

      summaryContent += `**Title:** ${pullRequestItem.title || "No title provided"}\n\n`;
      summaryContent += `**Branch:** ${pullRequestItem.branch || "auto-generated"}\n\n`;
      summaryContent += `**Base:** ${baseBranch}\n\n`;

      if (pullRequestItem.body) {
        summaryContent += `**Body:**\n${pullRequestItem.body}\n\n`;
      }

      if (fs.existsSync("/tmp/gh-aw/aw.patch")) {
        const patchStats = fs.readFileSync("/tmp/gh-aw/aw.patch", "utf8");
        if (patchStats.trim()) {
          summaryContent += `**Changes:** Patch file exists with ${patchStats.split("\n").length} lines\n\n`;
          summaryContent += `<details><summary>Show patch preview</summary>\n\n\`\`\`diff\n${patchStats.slice(0, 2000)}${patchStats.length > 2000 ? "\n... (truncated)" : ""}\n\`\`\`\n\n</details>\n\n`;
        } else {
          summaryContent += `**Changes:** No changes (empty patch)\n\n`;
        }
      }

      // Write to step summary
      await core.summary.addRaw(summaryContent).write();
      core.info("📝 Pull request creation preview written to step summary");
      return { success: true, staged: true };
    }

    // Extract title, body, and branch from the JSON item
    let title = pullRequestItem.title.trim();
    let processedBody = pullRequestItem.body;

    // Replace temporary ID references in the body with resolved issue/PR numbers
    // This allows PRs to reference issues created earlier in the same workflow
    // by using temporary IDs like #aw_123abc456def
    if (resolvedTemporaryIds && Object.keys(resolvedTemporaryIds).length > 0) {
      // Convert object to Map for compatibility with replaceTemporaryIdReferences
      const tempIdMap = new Map(Object.entries(resolvedTemporaryIds));
      processedBody = replaceTemporaryIdReferences(processedBody, tempIdMap, itemRepo);
      core.info(`Resolved ${tempIdMap.size} temporary ID references in PR body`);
    }

    // Remove duplicate title from description if it starts with a header matching the title
    processedBody = removeDuplicateTitleFromDescription(title, processedBody);

    // Auto-add "Fixes #N" closing keyword if triggered from an issue and not already present.
    // This ensures the triggering issue is auto-closed when the PR is merged.
    // Agents are instructed to include this but don't reliably do so.
    if (triggeringIssueNumber) {
      const hasClosingKeyword = /(?:fix|fixes|fixed|close|closes|closed|resolve|resolves|resolved)\s+#\d+/i.test(processedBody);
      if (!hasClosingKeyword) {
        processedBody = processedBody.trimEnd() + `\n\n- Fixes #${triggeringIssueNumber}`;
        core.info(`Auto-added "Fixes #${triggeringIssueNumber}" closing keyword to PR body as bullet point`);
      }
    }

    let bodyLines = processedBody.split("\n");
    let branchName = pullRequestItem.branch ? pullRequestItem.branch.trim() : null;

    // If no title was found, use a default
    if (!title) {
      title = "Agent Output";
    }

    // Apply title prefix from config
    if (titlePrefix && !title.startsWith(titlePrefix)) {
      title = titlePrefix + title;
    }

    // Add AI disclaimer with workflow name and run url
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
    const workflowId = process.env.GH_AW_WORKFLOW_ID || "";
    const runId = context.runId;
    const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
    const runUrl = context.payload.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${repoParts.owner}/${repoParts.repo}/actions/runs/${runId}`;

    // Add fingerprint comment if present
    const trackerIDComment = getTrackerID("markdown");
    if (trackerIDComment) {
      bodyLines.push(trackerIDComment);
    }

    // Generate footer with expiration using helper
    const footer = generateFooterWithExpiration({
      footerText: `> AI generated by [${workflowName}](${runUrl})`,
      expiresHours,
      entityType: "Pull Request",
      suffix: expiresHours > 0 ? "\n\n<!-- gh-aw-expires-type: pull-request -->" : undefined,
    });

    bodyLines.push(``, ``, footer);

    // Add standalone workflow-id marker for searchability (consistent with comments)
    if (workflowId) {
      bodyLines.push(``, generateWorkflowIdMarker(workflowId));
    }

    bodyLines.push("");

    // Prepare the body content
    const body = bodyLines.join("\n").trim();

    // Build labels array - merge config labels with message labels
    let labels = [...envLabels];
    if (pullRequestItem.labels && Array.isArray(pullRequestItem.labels)) {
      labels = [...labels, ...pullRequestItem.labels];
    }
    labels = labels
      .filter(label => !!label)
      .map(label => String(label).trim())
      .filter(label => label);

    // Use draft setting from message if provided, otherwise use config default
    const draft = pullRequestItem.draft !== undefined ? pullRequestItem.draft : draftDefault;

    core.info(`Creating pull request with title: ${title}`);
    core.info(`Labels: ${JSON.stringify(labels)}`);
    core.info(`Draft: ${draft}`);
    core.info(`Body length: ${body.length}`);

    const randomHex = crypto.randomBytes(8).toString("hex");
    // Use branch name from JSONL if provided, otherwise generate unique branch name
    if (!branchName) {
      core.info("No branch name provided in JSONL, generating unique branch name");
      // Generate unique branch name using cryptographic random hex
      branchName = `${workflowId}-${randomHex}`;
    } else {
      branchName = `${branchName}-${randomHex}`;
      core.info(`Using branch name from JSONL with added salt: ${branchName}`);
    }

    core.info(`Generated branch name: ${branchName}`);
    core.info(`Base branch: ${baseBranch}`);

    // Create a new branch using git CLI, ensuring it's based on the correct base branch

    // First, fetch the base branch specifically (since we use shallow checkout)
    core.info(`Fetching base branch: ${baseBranch}`);

    // Fetch without creating/updating local branch to avoid conflicts with current branch
    // This works even when we're already on the base branch
    await exec.exec(`git fetch origin ${baseBranch}`);

    // Checkout the base branch (using origin/${baseBranch} if local doesn't exist)
    try {
      await exec.exec(`git checkout ${baseBranch}`);
    } catch (checkoutError) {
      // If local branch doesn't exist, create it from origin
      core.info(`Local branch ${baseBranch} doesn't exist, creating from origin/${baseBranch}`);
      await exec.exec(`git checkout -b ${baseBranch} origin/${baseBranch}`);
    }

    // Handle branch creation/checkout
    core.info(`Branch should not exist locally, creating new branch from base: ${branchName}`);
    await exec.exec(`git checkout -b ${branchName}`);
    core.info(`Created new branch from base: ${branchName}`);

    // Apply the patch using git CLI (skip if empty)
    if (!isEmpty) {
      core.info("Applying patch...");

      // Log first 500 lines of patch for debugging
      const patchLines = patchContent.split("\n");
      const previewLineCount = Math.min(500, patchLines.length);
      core.info(`Patch preview (first ${previewLineCount} of ${patchLines.length} lines):`);
      for (let i = 0; i < previewLineCount; i++) {
        core.info(patchLines[i]);
      }

      // Patches are created with git format-patch, so use git am to apply them
      try {
        await exec.exec("git am /tmp/gh-aw/aw.patch");
        core.info("Patch applied successfully");
      } catch (patchError) {
        core.error(`Failed to apply patch: ${patchError instanceof Error ? patchError.message : String(patchError)}`);

        // Investigate why the patch failed by logging git status and the failed patch
        try {
          core.info("Investigating patch failure...");

          // Log git status to see the current state
          const statusResult = await exec.getExecOutput("git", ["status"]);
          core.info("Git status output:");
          core.info(statusResult.stdout);

          // Log the failed patch diff
          const patchResult = await exec.getExecOutput("git", ["am", "--show-current-patch=diff"]);
          core.info("Failed patch content:");
          core.info(patchResult.stdout);
        } catch (investigateError) {
          core.warning(`Failed to investigate patch failure: ${investigateError instanceof Error ? investigateError.message : String(investigateError)}`);
        }

        return { success: false, error: "Failed to apply patch" };
      }

      // Push the applied commits to the branch (with fallback to issue creation on failure)
      try {
        // Check if remote branch already exists (optional precheck)
        let remoteBranchExists = false;
        try {
          const { stdout } = await exec.getExecOutput(`git ls-remote --heads origin ${branchName}`);
          if (stdout.trim()) {
            remoteBranchExists = true;
          }
        } catch (checkError) {
          core.info(`Remote branch check failed (non-fatal): ${checkError instanceof Error ? checkError.message : String(checkError)}`);
        }

        if (remoteBranchExists) {
          core.warning(`Remote branch ${branchName} already exists - appending random suffix`);
          const extraHex = crypto.randomBytes(4).toString("hex");
          const oldBranch = branchName;
          branchName = `${branchName}-${extraHex}`;
          // Rename local branch
          await exec.exec(`git branch -m ${oldBranch} ${branchName}`);
          core.info(`Renamed branch to ${branchName}`);
        }

        await exec.exec(`git push origin ${branchName}`);
        core.info("Changes pushed to branch");
      } catch (pushError) {
        // Push failed - create fallback issue instead of PR
        core.error(`Git push failed: ${pushError instanceof Error ? pushError.message : String(pushError)}`);
        core.warning("Git push operation failed - creating fallback issue instead of pull request");

        const runId = context.runId;
        const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
        const runUrl = context.payload.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${repoParts.owner}/${repoParts.repo}/actions/runs/${runId}`;

        // Read patch content for preview
        let patchPreview = "";
        if (fs.existsSync("/tmp/gh-aw/aw.patch")) {
          const patchContent = fs.readFileSync("/tmp/gh-aw/aw.patch", "utf8");
          patchPreview = generatePatchPreview(patchContent);
        }

        const fallbackBody = `${body}

---

> [!NOTE]
> This was originally intended as a pull request, but the git push operation failed.
>
> **Workflow Run:** [View run details and download patch artifact](${runUrl})
>
> The patch file is available in the \`agent-artifacts\` artifact in the workflow run linked above.

To apply the patch locally:

\`\`\`sh
# Download the artifact from the workflow run ${runUrl}
# (Use GitHub MCP tools if gh CLI is not available)
gh run download ${runId} -n agent-artifacts

# The patch file will be at agent-artifacts/tmp/gh-aw/aw.patch after download
# Apply the patch
git am agent-artifacts/tmp/gh-aw/aw.patch
\`\`\`
${patchPreview}`;

        try {
          const { data: issue } = await github.rest.issues.create({
            owner: repoParts.owner,
            repo: repoParts.repo,
            title: title,
            body: fallbackBody,
            labels: labels,
          });

          core.info(`Created fallback issue #${issue.number}: ${issue.html_url}`);

          // Update the activation comment with issue link (if a comment was created)
          await updateActivationComment(github, context, core, issue.html_url, issue.number, "issue");

          // Write summary to GitHub Actions summary
          await core.summary
            .addRaw(
              `

## Push Failure Fallback
- **Push Error:** ${pushError instanceof Error ? pushError.message : String(pushError)}
- **Fallback Issue:** [#${issue.number}](${issue.html_url})
- **Patch Artifact:** Available in workflow run artifacts
- **Note:** Push failed, created issue as fallback
`
            )
            .write();

          return {
            success: true,
            fallback_used: true,
            push_failed: true,
            issue_number: issue.number,
            issue_url: issue.html_url,
            branch_name: branchName,
            repo: itemRepo,
          };
        } catch (issueError) {
          const error = `Failed to push and failed to create fallback issue. Push error: ${pushError instanceof Error ? pushError.message : String(pushError)}. Issue error: ${issueError instanceof Error ? issueError.message : String(issueError)}`;
          core.error(error);
          return {
            success: false,
            error,
          };
        }
      }
    } else {
      core.info("Skipping patch application (empty patch)");

      // For empty patches with allow-empty, we still need to push the branch
      if (allowEmpty) {
        core.info("allow-empty is enabled - will create branch and push with empty commit");
        // Push the branch with an empty commit to allow PR creation
        try {
          // Create an empty commit to ensure there's a commit difference
          await exec.exec(`git commit --allow-empty -m "Initialize"`);
          core.info("Created empty commit");

          // Check if remote branch already exists (optional precheck)
          let remoteBranchExists = false;
          try {
            const { stdout } = await exec.getExecOutput(`git ls-remote --heads origin ${branchName}`);
            if (stdout.trim()) {
              remoteBranchExists = true;
            }
          } catch (checkError) {
            core.info(`Remote branch check failed (non-fatal): ${checkError instanceof Error ? checkError.message : String(checkError)}`);
          }

          if (remoteBranchExists) {
            core.warning(`Remote branch ${branchName} already exists - appending random suffix`);
            const extraHex = crypto.randomBytes(4).toString("hex");
            const oldBranch = branchName;
            branchName = `${branchName}-${extraHex}`;
            // Rename local branch
            await exec.exec(`git branch -m ${oldBranch} ${branchName}`);
            core.info(`Renamed branch to ${branchName}`);
          }

          await exec.exec(`git push origin ${branchName}`);
          core.info("Empty branch pushed successfully");
        } catch (pushError) {
          const error = `Failed to push empty branch: ${pushError instanceof Error ? pushError.message : String(pushError)}`;
          core.error(error);
          return {
            success: false,
            error,
          };
        }
      } else {
        // For empty patches without allow-empty, handle if-no-changes configuration
        const message = "No changes to apply - noop operation completed successfully";

        switch (ifNoChanges) {
          case "error":
            return { success: false, error: "No changes to apply - failing as configured by if-no-changes: error" };

          case "ignore":
            // Silent success - no console output
            return { success: false, skipped: true };

          case "warn":
          default:
            core.warning(message);
            return { success: false, error: message, skipped: true };
        }
      }
    }

    // Try to create the pull request, with fallback to issue creation
    try {
      const { data: pullRequest } = await github.rest.pulls.create({
        owner: repoParts.owner,
        repo: repoParts.repo,
        title: title,
        body: body,
        head: branchName,
        base: baseBranch,
        draft: draft,
      });

      core.info(`Created pull request #${pullRequest.number}: ${pullRequest.html_url}`);

      // Add labels if specified
      if (labels.length > 0) {
        await github.rest.issues.addLabels({
          owner: repoParts.owner,
          repo: repoParts.repo,
          issue_number: pullRequest.number,
          labels: labels,
        });
        core.info(`Added labels to pull request: ${JSON.stringify(labels)}`);
      }

      // Enable auto-merge if configured
      if (autoMerge) {
        try {
          await github.graphql(
            `mutation($prId: ID!) {
              enablePullRequestAutoMerge(input: {pullRequestId: $prId}) {
                pullRequest {
                  id
                }
              }
            }`,
            {
              prId: pullRequest.node_id,
            }
          );
          core.info(`Enabled auto-merge for pull request #${pullRequest.number}`);
        } catch (autoMergeError) {
          core.warning(`Failed to enable auto-merge for PR #${pullRequest.number}: ${autoMergeError instanceof Error ? autoMergeError.message : String(autoMergeError)}`);
        }
      }

      // Update the activation comment with PR link (if a comment was created)
      await updateActivationComment(github, context, core, pullRequest.html_url, pullRequest.number);

      // Write summary to GitHub Actions summary
      await core.summary
        .addRaw(
          `

## Pull Request
- **Pull Request**: [#${pullRequest.number}](${pullRequest.html_url})
- **Branch**: \`${branchName}\`
- **Base Branch**: \`${baseBranch}\`
`
        )
        .write();

      // Return success with PR details
      return {
        success: true,
        pull_request_number: pullRequest.number,
        pull_request_url: pullRequest.html_url,
        branch_name: branchName,
        temporary_id: pullRequestItem.temporary_id, // Pass through if present
        repo: itemRepo,
      };
    } catch (prError) {
      const errorMessage = prError instanceof Error ? prError.message : String(prError);
      core.warning(`Failed to create pull request: ${errorMessage}`);

      // Check if the error is the specific "GitHub actions is not permitted to create or approve pull requests" error
      if (errorMessage.includes("GitHub Actions is not permitted to create or approve pull requests")) {
        core.error("Permission error: GitHub Actions is not permitted to create or approve pull requests");
        // Set output variable for conclusion job to handle
        core.setOutput(
          "error_message",
          "GitHub Actions is not permitted to create or approve pull requests. Please enable 'Allow GitHub Actions to create and approve pull requests' in repository settings: https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository#preventing-github-actions-from-creating-or-approving-pull-requests"
        );
        return {
          success: false,
          error: errorMessage,
          error_type: "permission_denied",
        };
      }

      core.info("Falling back to creating an issue instead");

      // Create issue as fallback with enhanced body content
      const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
      const branchUrl = context.payload.repository ? `${context.payload.repository.html_url}/tree/${branchName}` : `${githubServer}/${repoParts.owner}/${repoParts.repo}/tree/${branchName}`;

      // Read patch content for preview
      let patchPreview = "";
      if (fs.existsSync("/tmp/gh-aw/aw.patch")) {
        const patchContent = fs.readFileSync("/tmp/gh-aw/aw.patch", "utf8");
        patchPreview = generatePatchPreview(patchContent);
      }

      const fallbackBody = `${body}

---

**Note:** This was originally intended as a pull request, but PR creation failed. The changes have been pushed to the branch [\`${branchName}\`](${branchUrl}).

**Original error:** ${errorMessage}

You can manually create a pull request from the branch if needed.${patchPreview}`;

      try {
        const { data: issue } = await github.rest.issues.create({
          owner: repoParts.owner,
          repo: repoParts.repo,
          title: title,
          body: fallbackBody,
          labels: labels,
        });

        core.info(`Created fallback issue #${issue.number}: ${issue.html_url}`);

        // Update the activation comment with issue link (if a comment was created)
        await updateActivationComment(github, context, core, issue.html_url, issue.number, "issue");

        // Return success with fallback flag
        return {
          success: true,
          fallback_used: true,
          issue_number: issue.number,
          issue_url: issue.html_url,
          branch_name: branchName,
          repo: itemRepo,
        };
      } catch (issueError) {
        const error = `Failed to create both pull request and fallback issue. PR error: ${errorMessage}. Issue error: ${issueError instanceof Error ? issueError.message : String(issueError)}`;
        core.error(error);
        return {
          success: false,
          error,
        };
      }
    }
  }; // End of handleCreatePullRequest
} // End of main

module.exports = { main };
