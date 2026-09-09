// @ts-check
/// <reference types="@actions/github-script" />
// @safe-outputs-exempt SEC-004 — PR/issue body values in this handler are static internal templates (plus allowlisted changed-file paths), not untrusted user content.

const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_CONFIG, ERR_SYSTEM } = require("./error_codes.cjs");

/**
 * Files that the 'update' command can modify outside of .github/workflows/.
 * Only these files will be staged and included in the update PR.
 */
const KNOWN_FILES_UPDATE = [".github/aw/actions-lock.json"];

/**
 * Files that the 'upgrade' command can modify outside of .github/workflows/.
 * Only these files will be staged and included in the upgrade PR.
 */
const KNOWN_FILES_UPGRADE = [
  ".github/aw/actions-lock.json",
  ".github/aw/instructions.md",
  ".github/skills/agentic-workflows/SKILL.md",
  ".github/skills/agentic-workflow-designer/SKILL.md",
  ".github/agents/agentic-workflows.md",
  ".github/agents/interactive-agent-designer.agent.md",
  // Old agent files that may be deleted by deleteOldAgentFiles:
  ".github/agents/agentic-workflows.agent.md",
  ".github/agents/create-agentic-workflow.agent.md",
  ".github/agents/debug-agentic-workflow.agent.md",
  ".github/agents/create-shared-agentic-workflow.agent.md",
  ".github/agents/create-shared-agentic-workflow.md",
  ".github/agents/create-agentic-workflow.md",
  ".github/agents/setup-agentic-workflows.md",
  ".github/agents/update-agentic-workflows.md",
  ".github/agents/upgrade-agentic-workflows.md",
  ".github/aw/upgrade-agentic-workflow.md",
  // Deprecated schema file that may be deleted by fix command:
  ".github/aw/schemas/agentic-workflow.json",
];

/**
 * Format a UTC Date as YYYY-MM-DD-HH-MM-SS for use in branch names.
 * Colons are not allowed in artifact filenames or branch names on some systems.
 *
 * @param {Date} date
 * @returns {string}
 */
function formatTimestamp(date) {
  /** @param {number} n */
  const pad = n => String(n).padStart(2, "0");
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}-${pad(date.getUTCHours())}-${pad(date.getUTCMinutes())}-${pad(date.getUTCSeconds())}`;
}

/**
 * Run maintenance operations handled by run_operation:
 * - 'gh aw update', 'gh aw upgrade', 'gh aw disable', 'gh aw enable'
 * creating a pull request when needed for update/upgrade operations.
 *
 * For update/upgrade: runs with --no-compile so lock files are not modified.
 * A pull request is opened for any changed files. The PR body instructs
 * reviewers to recompile lock files after merging.
 *
 * For disable/enable: simply runs the command; no PR is created.
 *
 * Required environment variables:
 *   GH_TOKEN           - GitHub token for gh CLI auth and git push
 *   GH_AW_OPERATION    - 'update', 'upgrade', 'disable', or 'enable'
 *   GH_AW_CMD_PREFIX   - Command prefix: './gh-aw' (dev) or 'gh aw' (release)
 *   GH_AW_UPGRADE_OPTIONS - JSON array of supported options passed to upgrade
 *
 * @returns {Promise<void>}
 */
async function main() {
  const operation = process.env.GH_AW_OPERATION;
  if (!operation) {
    core.info("Skipping: no operation specified");
    return;
  }

  const cmdPrefixStr = process.env.GH_AW_CMD_PREFIX || "gh aw";
  const [bin, ...prefixArgs] = cmdPrefixStr.split(" ").filter(Boolean);

  // Handle enable/disable operations: run the command and finish (no PR needed)
  if (operation === "disable" || operation === "enable") {
    const fullCmd = [bin, ...prefixArgs, operation].join(" ");
    core.info(`Running: ${fullCmd}`);
    const exitCode = await exec.exec(bin, [...prefixArgs, operation]);
    if (exitCode !== 0) {
      throw new Error(`${ERR_SYSTEM}: Command '${fullCmd}' failed with exit code ${exitCode}`);
    }
    core.info(`✓ All agentic workflows have been ${operation}d`);
    return;
  }

  // For update/upgrade, validate operation and proceed with PR creation if files changed
  if (operation !== "update" && operation !== "upgrade") {
    core.info(`Skipping: unknown operation '${operation}'`);
    return;
  }

  const isUpgrade = operation === "upgrade";

  // Run gh aw update or gh aw upgrade without extra flags so all files are
  // updated (codemods, action pins, lock files, etc.).  Changed files under
  // .github/workflows/ are detected afterwards but excluded from staging so
  // the GitHub Actions actor – which is not permitted to commit workflow
  // files – does not attempt to include them in the pull request.
  const fullCmd = [bin, ...prefixArgs, operation].join(" ");
  core.info(`Running: ${fullCmd}`);
  const exitCode = await exec.exec(bin, [...prefixArgs, operation]);
  if (exitCode !== 0) {
    throw new Error(`${ERR_SYSTEM}: Command '${fullCmd}' failed with exit code ${exitCode}`);
  }

  // Stage only files known to be modified by the update/upgrade command.
  // Using an allowlist (rather than git-status discovery) prevents temporary
  // files created during the operation from being accidentally committed.
  const knownFiles = isUpgrade ? KNOWN_FILES_UPGRADE : KNOWN_FILES_UPDATE;
  for (const file of knownFiles) {
    try {
      await exec.exec("git", ["add", "-A", "--", file]);
    } catch (error) {
      core.warning(`Failed to stage '${file}': ${getErrorMessage(error)}`);
    }
  }

  // Check what was actually staged
  const { stdout: stagedOutput } = await exec.getExecOutput("git", ["diff", "--cached", "--name-only"]);
  if (!stagedOutput.trim()) {
    core.info("✓ No changes detected - nothing to create a PR for");
    return;
  }

  const stagedFiles = stagedOutput
    .split("\n")
    .map(f => f.trim())
    .filter(Boolean);

  core.info(`Found ${stagedFiles.length} file(s) to include in PR:`);
  for (const f of stagedFiles) {
    core.info(`  ${f}`);
  }

  // Configure git identity
  await exec.exec("git", ["config", "user.email", "github-actions[bot]@users.noreply.github.com"]);
  await exec.exec("git", ["config", "user.name", "github-actions[bot]"]);

  // Create a new branch with a filesystem-safe timestamp (no colons)
  const branchName = `aw/${operation}-${formatTimestamp(new Date())}`;
  core.info(`Creating branch: ${branchName}`);
  await exec.exec("git", ["checkout", "-b", branchName]);

  // Commit the changes
  const commitMessage = isUpgrade ? "chore: upgrade agentic workflows" : "chore: update agentic workflows";
  await exec.exec("git", ["commit", "-m", commitMessage]);

  // Push to the new branch using a token-authenticated remote
  const owner = context.repo.owner;
  const repo = context.repo.repo;
  const token = process.env.GH_TOKEN || process.env.GITHUB_TOKEN;
  if (!token) {
    throw new Error(`${ERR_CONFIG}: Missing GitHub token: set GH_TOKEN or GITHUB_TOKEN to push changes and create a pull request for agentic workflow update/upgrade operations.`);
  }
  core.setSecret(token);
  const githubServerUrl = process.env.GITHUB_SERVER_URL || "https://github.com";
  let githubHost;
  try {
    githubHost = new URL(githubServerUrl).hostname || "github.com";
  } catch {
    githubHost = "github.com";
  }
  const remoteUrl = `https://x-access-token:${token}@${githubHost}/${owner}/${repo}.git`;

  try {
    await exec.exec("git", ["remote", "remove", "aw-push"]);
  } catch {
    // Remote doesn't exist yet — removal failure is ignored, that's fine.
  }
  await exec.exec("git", ["remote", "add", "aw-push", remoteUrl], { silent: true });

  try {
    await exec.exec("git", ["push", "aw-push", branchName]);
  } finally {
    // Always clean up the temporary remote
    try {
      await exec.exec("git", ["remote", "remove", "aw-push"]);
    } catch {
      // Non-fatal
    }
  }

  // Build PR title and body
  const prTitle = isUpgrade ? "[aw] Upgrade available" : "[aw] Updates available";
  const fileList = stagedFiles.map(f => `- \`${f}\``).join("\n");
  const operationLabel = isUpgrade ? "Upgrade" : "Update";
  const prBody = `## Agentic Workflows ${operationLabel}

The \`gh aw ${operation}\` command was run automatically and produced the following changes:

${fileList}

### ⚠️ Lock Files Need Recompilation

After merging this PR, **recompile the lock files** using one of these methods:

1. **Via @copilot**: Add a comment \`@copilot compile agentic workflows\` on this PR
2. **Via CLI**: Run \`gh aw compile --validate\` in your local checkout after merging
`;

  // Create the PR using gh CLI
  core.info(`Creating PR: "${prTitle}"`);
  const { stdout: prOutput } = await exec.getExecOutput("gh", ["pr", "create", "--title", prTitle, "--body", prBody, "--head", branchName, "--label", "agentic-workflows"], {
    env: { ...process.env, GH_TOKEN: token },
  });

  const prUrl = prOutput.trim();
  core.info(`✓ Created PR: ${prUrl}`);
  core.notice(`Created PR: ${prUrl}`);

  await core.summary
    .addHeading(prTitle, 2)
    .addRaw(`Pull request created: [${prUrl}](${prUrl})\n\n`)
    .addRaw(`**Changed files included in PR:**\n\n${fileList}\n\n`)
    .addRaw(`> **Note**: Recompile lock files after merging via \`@copilot compile agentic workflows\` or \`gh aw compile\`.`)
    .write();
}

/**
 * XML marker embedded in issue bodies to identify issues created by the
 * agentic-auto-upgrade workflow. Used for deduplication: old matching issues
 * are closed before a new one is opened.
 */
const AUTO_UPGRADE_WORKFLOW_ID = "agentic-auto-upgrade";
const AUTO_UPGRADE_ISSUE_MARKER = `<!-- gh-aw-workflow-id: ${AUTO_UPGRADE_WORKFLOW_ID} -->`;

/**
 * Run the upgrade operation in notification mode: executes `gh aw upgrade`,
 * detects any changed files, then creates a GitHub issue to announce that an
 * upgrade is available.  Before opening the new issue, any previously opened
 * issues carrying the same XML marker are closed so there is never more than
 * one open notification at a time.
 *
 * Permissions required: issues: write only (no contents/pull-requests write).
 *
 * Required environment variables:
 *   GH_TOKEN           - GitHub token for gh CLI auth and GitHub API
 *   GH_AW_CMD_PREFIX   - Command prefix: './gh-aw' (dev) or 'gh aw' (release)
 *
 * @returns {Promise<void>}
 */
async function mainNotifyIssue() {
  const cmdPrefixStr = process.env.GH_AW_CMD_PREFIX || "gh aw";
  const [bin, ...prefixArgs] = cmdPrefixStr.split(" ").filter(Boolean);
  let upgradeOptions;
  try {
    upgradeOptions = JSON.parse(process.env.GH_AW_UPGRADE_OPTIONS || "[]");
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: GH_AW_UPGRADE_OPTIONS is not valid JSON: ${getErrorMessage(error)}`, { cause: error });
  }
  if (!Array.isArray(upgradeOptions) || upgradeOptions.some(option => option !== "--pre-releases")) {
    throw new Error(`${ERR_SYSTEM}: GH_AW_UPGRADE_OPTIONS contains an unsupported option`);
  }

  const owner = context.repo.owner;
  const repo = context.repo.repo;

  // Run gh aw upgrade to apply changes locally
  const fullCmd = [bin, ...prefixArgs, "upgrade", ...upgradeOptions].join(" ");
  core.info(`Running: ${fullCmd}`);
  const exitCode = await exec.exec(bin, [...prefixArgs, "upgrade", ...upgradeOptions]);
  if (exitCode !== 0) {
    throw new Error(`${ERR_SYSTEM}: Command '${fullCmd}' failed with exit code ${exitCode}`);
  }

  // Detect which known upgrade files were modified
  const changedFiles = [];
  for (const file of KNOWN_FILES_UPGRADE) {
    try {
      const { stdout } = await exec.getExecOutput("git", ["diff", "--name-only", "--", file], { silent: true });
      if (stdout.trim()) {
        changedFiles.push(file);
      }
    } catch {
      // File not in repo — ignored, skip it.
    }
  }

  if (changedFiles.length === 0) {
    core.info("✓ No upgrade available - agentic workflows are already up to date");
    return;
  }

  core.info(`Upgrade available. Changed files (${changedFiles.length}):`);
  for (const f of changedFiles) {
    core.info(`  ${f}`);
  }

  // Discard local changes — we only notify via issue, not push
  try {
    await exec.exec("git", ["checkout", "--", "."]);
  } catch (error) {
    core.warning(`Failed to discard local changes: ${getErrorMessage(error)}`);
  }

  // Close any existing open issues with the auto-upgrade XML marker.
  // Strip the comment delimiters to get the plain text used in search.
  const markerContent = AUTO_UPGRADE_ISSUE_MARKER.replace(/^<!--\s*/, "").replace(/\s*-->$/, "");
  const searchQuery = `repo:${owner}/${repo} is:issue is:open "${markerContent}" in:body`;
  core.info(`Searching for existing auto-upgrade issues: ${searchQuery}`);

  let existingIssues = [];
  try {
    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 20,
    });
    existingIssues = (searchResult.data.items || []).filter(item => !item.pull_request && item.body && item.body.includes(AUTO_UPGRADE_ISSUE_MARKER));
  } catch (error) {
    core.warning(`Failed to search for existing issues: ${getErrorMessage(error)}`);
  }

  core.info(`Found ${existingIssues.length} existing auto-upgrade issue(s) to close`);
  for (const issue of existingIssues) {
    try {
      await github.rest.issues.update({
        owner,
        repo,
        issue_number: issue.number,
        state: "closed",
        state_reason: "not_planned",
      });
      core.info(`  Closed #${issue.number}: ${issue.title}`);
    } catch (error) {
      core.warning(`  Failed to close #${issue.number}: ${getErrorMessage(error)}`);
    }
  }

  // Build and create the new upgrade notification issue
  const issueTitle = "[aw] Upgrade available";
  const fileList = changedFiles.map(f => `- \`${f}\``).join("\n");
  const issueBody = `## Agentic Workflow Upgrade Available

A new version of the agentic workflow tooling is available. Run \`gh aw upgrade\` to apply it.

**Files that will be updated:**

${fileList}

### How to apply

- **Via @copilot**: Add a comment \`@copilot upgrade agentic workflows\` on this issue
- **Via CLI**: Run \`gh aw upgrade\` in your local checkout

${AUTO_UPGRADE_ISSUE_MARKER}
`;

  core.info(`Creating upgrade notification issue: "${issueTitle}"`);
  let createdIssue;
  try {
    createdIssue = await github.rest.issues.create({
      owner,
      repo,
      title: issueTitle,
      body: issueBody,
      labels: ["agentic-workflows"],
    });
  } catch (error) {
    // Label may not exist when auto-upgrade is used without maintenance label creation.
    const errorAny = /** @type {any} */ error;
    if (errorAny?.status === 422) {
      core.warning("Failed to create issue with label 'agentic-workflows'; retrying without labels");
      createdIssue = await github.rest.issues.create({
        owner,
        repo,
        title: issueTitle,
        body: issueBody,
      });
    } else {
      throw error;
    }
  }

  const issueUrl = createdIssue.data.html_url;
  core.info(`✓ Created issue: ${issueUrl}`);
  core.notice(`Created upgrade notification issue: ${issueUrl}`);

  await core.summary
    .addHeading(issueTitle, 2)
    .addRaw(`Issue created: [${issueUrl}](${issueUrl})\n\n`)
    .addRaw(`**Files that will be updated:**\n\n${fileList}\n\n`)
    .addRaw(`> **To apply:** run \`gh aw upgrade\` locally or comment \`@copilot upgrade agentic workflows\`.`)
    .write();
}

module.exports = { main, mainNotifyIssue, formatTimestamp };
