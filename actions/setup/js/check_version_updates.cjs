// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Check compile-agentic version against the remote update configuration.
 *
 * This script:
 * 1. Reads the compiled version from GH_AW_COMPILED_VERSION env var.
 * 2. Skips the check if the version is not in vMAJOR.MINOR.PATCH official release format.
 * 3. Fetches .github/aw/compat.json from the gh-aw-actions repository via raw.githubusercontent.com.
 *    - Uses withRetry to handle transient network failures.
 * 4. If the download fails or config is invalid JSON, the check is skipped (soft failure).
 * 5. Validates that the compiled version is not in the blocked list.
 * 6. Validates that the compiled version meets the minimum supported version.
 *
 * Fails the activation job when validation fails.
 */

const { withRetry, isTransientError } = require("./error_recovery.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { renderTemplateFromFile, getPromptPath } = require("./messages_core.cjs");

const CONFIG_URL = "https://raw.githubusercontent.com/github/gh-aw-actions/main/.github/aw/compat.json";
const FETCH_TIMEOUT_MS = 120_000;
const BLOCKED_VERSION_ISSUE_TITLE_PREFIX = "[aw] Workflows blocked by compile-agentic";
const GITHUB_API_VERSION = "2022-11-28";

/**
 * Parse an official version string (must be in vMAJOR.MINOR.PATCH format).
 * Versions without a leading "v" are not treated as official releases and return null.
 * Versions with unknown syntax also return null.
 *
 * @param {string} version
 * @returns {number[]|null}
 */
function parseVersion(version) {
  if (!version.startsWith("v")) return null;
  const parts = version.slice(1).split(".");
  if (parts.length !== 3) return null;
  const nums = parts.map(Number);
  if (nums.some(isNaN)) return null;
  return nums;
}

/**
 * Compare two official version strings (both must be in vMAJOR.MINOR.PATCH format).
 * Returns a negative number if a < b, 0 if equal, positive if a > b.
 * Returns 0 (treat as equal/unknown) if either version cannot be parsed.
 *
 * @param {string} a
 * @param {string} b
 * @returns {number}
 */
function compareVersions(a, b) {
  const pa = parseVersion(a);
  const pb = parseVersion(b);
  if (!pa || !pb) return 0;
  for (let i = 0; i < 3; i++) {
    if (pa[i] !== pb[i]) return pa[i] - pb[i];
  }
  return 0;
}

/**
 * Build the stable issue title used to deduplicate blocked compiler notifications.
 *
 * @param {string} compiledVersion
 * @returns {string}
 */
function buildBlockedVersionIssueTitle(compiledVersion) {
  return `${BLOCKED_VERSION_ISSUE_TITLE_PREFIX} ${compiledVersion}`;
}

/**
 * Return a Markdown-safe inline-code representation.
 *
 * @param {string} value
 * @returns {string}
 */
function markdownCode(value) {
  return `\`${String(value).replace(/`/g, "\\`")}\``;
}

/**
 * Build a link to the current workflow run when the GitHub Actions context is available.
 *
 * @returns {string}
 */
function getRunUrl() {
  const repoFullName = process.env.GITHUB_REPOSITORY || (typeof context !== "undefined" && context.repo ? `${context.repo.owner}/${context.repo.repo}` : "");
  const runId = process.env.GITHUB_RUN_ID || String(typeof context !== "undefined" && context.runId ? context.runId : "");
  if (!repoFullName || !runId) {
    return "";
  }
  const serverUrl = process.env.GITHUB_SERVER_URL || (typeof context !== "undefined" ? context.serverUrl : "") || "https://github.com";
  return `${serverUrl}/${repoFullName}/actions/runs/${runId}`;
}

/**
 * Build the body for the blocked compiler notification issue.
 *
 * @param {string} compiledVersion
 * @returns {string}
 */
function buildBlockedVersionIssueBody(compiledVersion) {
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || (typeof context !== "undefined" ? context.workflow : "") || "unknown";
  const runUrl = getRunUrl();
  return renderTemplateFromFile(getPromptPath("blocked_compiler_version_issue.md"), {
    compiled_version: compiledVersion,
    compiled_version_code: markdownCode(compiledVersion),
    workflow_name_code: markdownCode(workflowName),
    run_url_line: runUrl ? `- Run: ${runUrl}` : "",
  });
}

/**
 * Find an existing open blocked-version issue for this compiler version.
 *
 * NOTE: This relies on the GitHub search index, which is eventually consistent.
 * During a repo-wide blocked-version outage, many activation runs can fire close
 * together; two runs can both observe `items: []` before the index catches up and
 * both proceed to create an issue, producing duplicates. This is accepted as a
 * best-effort tradeoff (consistent with other dedup lookups in this codebase, e.g.
 * handle_agent_failure.cjs) rather than a correctness guarantee. The call is wrapped
 * in withRetry so transient failures and secondary rate limits (more likely during a
 * fan-out of many concurrent activation failures) don't silently drop the lookup.
 *
 * @param {string} owner
 * @param {string} repo
 * @param {string} compiledVersion
 * @returns {Promise<{number: number, html_url: string} | null>}
 */
async function findExistingBlockedVersionIssue(owner, repo, compiledVersion) {
  const title = buildBlockedVersionIssueTitle(compiledVersion);
  const result = await withRetry(
    () =>
      github.rest.search.issuesAndPullRequests({
        q: `repo:${owner}/${repo} is:issue is:open in:title "${title}"`,
        per_page: 10,
      }),
    {},
    "search for existing blocked compiler version issue"
  );
  const existing = result.data.items.find(item => item.title === title && !item.pull_request);
  return existing ? { number: existing.number, html_url: existing.html_url } : null;
}

/**
 * Best-effort issue notification for blocked compiler versions. Failures here must not
 * mask the primary blocked-version error.
 *
 * @param {string} compiledVersion
 * @returns {Promise<void>}
 */
async function reportBlockedVersionIssue(compiledVersion) {
  if (process.env.GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE === "false") {
    core.info("Blocked compiler version issue reporting is disabled");
    return;
  }
  if (typeof github === "undefined" || typeof context === "undefined" || !github.rest?.issues || !github.rest?.search || !context.repo) {
    core.info("GitHub issue APIs are unavailable; skipping blocked compiler version issue notification");
    return;
  }

  const { owner, repo } = context.repo;
  const title = buildBlockedVersionIssueTitle(compiledVersion);
  const body = buildBlockedVersionIssueBody(compiledVersion);

  try {
    const existing = await findExistingBlockedVersionIssue(owner, repo, compiledVersion);
    if (existing) {
      const updatedIssue = await withRetry(
        () =>
          github.rest.issues.update({
            owner,
            repo,
            issue_number: existing.number,
            title,
            body,
            headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
          }),
        {},
        "update blocked compiler version issue"
      );
      core.info(`Updated blocked compiler version issue #${updatedIssue.data.number}: ${updatedIssue.data.html_url}`);
      return;
    }

    const newIssue = await withRetry(
      () =>
        github.rest.issues.create({
          owner,
          repo,
          title,
          body,
          headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
        }),
      {},
      "create blocked compiler version issue"
    );
    core.info(`Created blocked compiler version issue #${newIssue.data.number}: ${newIssue.data.html_url}`);
  } catch (err) {
    core.warning(`Could not create or update blocked compiler version issue: ${getErrorMessage(err)}`);
  }
}

/**
 * @typedef {object} UpdateConfig
 * @property {string[]} [blockedVersions]
 * @property {string} [minimumVersion]
 * @property {string} [minRecommendedVersion]
 */

/**
 * Main entry point.
 */
async function main() {
  const compiledVersion = process.env.GH_AW_COMPILED_VERSION || "";

  if (!compiledVersion || compiledVersion === "dev") {
    core.info(`Skipping version update check: version is '${compiledVersion || "(empty)"}' (dev build)`);
    return;
  }

  // Only check official releases in vMAJOR.MINOR.PATCH format; ignore unknown syntax
  if (!parseVersion(compiledVersion)) {
    core.info(`Skipping version update check: '${compiledVersion}' is not an official release version (expected vMAJOR.MINOR.PATCH format)`);
    return;
  }

  core.info(`Checking compile-agentic version: ${compiledVersion}`);
  core.info(`Fetching update configuration from: ${CONFIG_URL}`);

  /** @type {UpdateConfig} */
  let config;
  try {
    config = await withRetry(
      async () => {
        const res = await fetch(CONFIG_URL, { signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
        if (!res.ok) {
          const err = new Error(`HTTP ${res.status} fetching ${CONFIG_URL}`);
          // @ts-ignore - Attach status so the retry predicate can inspect it
          err.status = res.status;
          throw err;
        }
        const parsed = JSON.parse(await res.text());
        // Guard: JSON.parse("null") returns null; treat non-object/null/array as empty config
        return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : {};
      },
      {
        shouldRetry: err =>
          isTransientError(err) ||
          // Retry on any HTTP 5xx response (server errors)
          (err !== null && typeof err === "object" && "status" in err && Number(err.status) >= 500),
      },
      "fetch update configuration"
    );
  } catch (err) {
    const message = getErrorMessage(err);
    core.info(`Could not fetch update configuration (${message}). Skipping version check.`);
    return;
  }

  const blockedVersions = Array.isArray(config.blockedVersions) ? config.blockedVersions : [];
  const minimumVersion = typeof config.minimumVersion === "string" ? config.minimumVersion : "";
  const minRecommendedVersion = typeof config.minRecommendedVersion === "string" ? config.minRecommendedVersion : "";

  // Check blocked versions — only consider entries in vMAJOR.MINOR.PATCH format; ignore unknown syntax
  const isBlocked = blockedVersions.some(v => parseVersion(v) !== null && compareVersions(compiledVersion, v) === 0);
  if (isBlocked) {
    core.summary
      .addRaw("### ❌ Blocked compile-agentic version\n\n")
      .addRaw(`The compile-agentic version \`${compiledVersion}\` is **blocked** and cannot be used to run workflows.\n\n`)
      .addRaw("This version has been revoked, typically due to a security issue.\n\n")
      .addRaw("**Action required:** Update `gh-aw` to the latest version and recompile your workflow with `gh aw compile`.\n");
    await core.summary.write();
    await reportBlockedVersionIssue(compiledVersion);
    core.setFailed(`Blocked compile-agentic version: ${compiledVersion} is in the blocked versions list. Update gh-aw to the latest version and recompile your workflow.`);
    return;
  }

  // Check minimum version — skip if minimumVersion is absent, empty, or has unknown syntax
  if (minimumVersion && parseVersion(minimumVersion) !== null) {
    if (compareVersions(compiledVersion, minimumVersion) < 0) {
      core.summary
        .addRaw("### ❌ Outdated compile-agentic version\n\n")
        .addRaw(`The compile-agentic version \`${compiledVersion}\` is below the minimum supported version \`${minimumVersion}\`.\n\n`)
        .addRaw("**Action required:** Update `gh-aw` to the latest version and recompile your workflow with `gh aw compile`.\n");
      await core.summary.write();
      core.setFailed(`Outdated compile-agentic version: ${compiledVersion} is below the minimum supported version ${minimumVersion}. Update gh-aw to the latest version and recompile your workflow.`);
      return;
    }
  }

  // Check recommended version — skip if minRecommendedVersion is absent, empty, or has unknown syntax
  if (minRecommendedVersion && parseVersion(minRecommendedVersion) !== null) {
    if (compareVersions(compiledVersion, minRecommendedVersion) < 0) {
      core.warning(
        `Recommended upgrade: compile-agentic version ${compiledVersion} is below the recommended version ${minRecommendedVersion}. Consider updating gh-aw to the latest version and recompiling your workflow with \`gh aw compile\`.`
      );
    }
  }

  core.info(`✅ Version check passed: ${compiledVersion}`);
}

module.exports = {
  buildBlockedVersionIssueBody,
  buildBlockedVersionIssueTitle,
  compareVersions,
  findExistingBlockedVersionIssue,
  main,
  parseVersion,
  reportBlockedVersionIssue,
};
