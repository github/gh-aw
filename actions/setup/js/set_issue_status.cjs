// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API, ERR_PERMISSION, ERR_VALIDATION } = require("./error_codes.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode, checkRequiredFilter, resolveTarget } = require("./safe_output_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { resolveSafeOutputIssueTarget, resolveRepoIssueTarget, loadTemporaryIdMapFromResolved } = require("./temporary_id.cjs");
const { PLANNING_STATUS_HEADERS, resolvePlanningStatus, planningStatusState, verifyPlanningStatus, planningStatusSnapshot, fetchIssueDuplicate } = require("./planning_status.cjs");

const HANDLER_TYPE = "set_issue_status";

/** @type {import('./types/handler-factory').HandlerFactoryFunction} */
async function main(config = {}) {
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const client = await createAuthenticatedGitHubClient(config);
  const targetConfig = config.target || "triggering";
  const maxCount = config.max || 5;
  const staged = isStagedMode(config);
  let processedCount = 0;

  const handleSetIssueStatus = async function (message, resolvedTemporaryIds, _temporaryIdMap, forcePreview = false) {
    if (processedCount >= maxCount) return { success: false, error: `${ERR_VALIDATION}: Max count of ${maxCount} reached` };
    processedCount++;
    const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "issue");
    if (!repoResult.success) return { success: false, error: repoResult.error };
    const { repo: itemRepo, repoParts } = repoResult;
    let item = message;
    if (targetConfig === "*") {
      const bound = resolveRepoIssueTarget(message.issue_number, loadTemporaryIdMapFromResolved(resolvedTemporaryIds), repoParts.owner, repoParts.repo);
      if (bound.wasTemporaryId && bound.resolved && `${bound.resolved.owner}/${bound.resolved.repo}`.toLowerCase() !== itemRepo.toLowerCase()) return { success: false, error: "Temporary issue target belongs to another repository" };
      const explicit = resolveSafeOutputIssueTarget({ message, resolvedTemporaryIds, repoParts, handlerType: HANDLER_TYPE, aliases: ["issue_number"] });
      if (!explicit.success) {
        if (explicit.deferred) processedCount--;
        return explicit;
      }
      if (explicit.number !== null) item = { ...message, issue_number: explicit.number };
    }
    const target = resolveTarget({ targetConfig, item, context, itemType: HANDLER_TYPE, supportsIssue: true });
    if (!target.success) return { success: false, ...(target.shouldFail === false ? { skipped: true, reason: target.error } : {}), error: target.error };
    const issueNumber = target.number;
    try {
      if (config.issue_intent === true || ["suggest", "rationale", "confidence"].some(key => message[key] !== undefined)) throw new Error(`${ERR_VALIDATION}: Native planning statuses do not support issue intent or suggestions`);
      const filtered = await checkRequiredFilter(client, repoParts, issueNumber, Array.isArray(config.required_labels) ? config.required_labels : [], config.required_title_prefix || "", HANDLER_TYPE);
      if (filtered) return filtered;
      const { owner, repo } = repoParts;
      const repositoryUrl = `${(process.env.GITHUB_API_URL || context.apiUrl || "https://api.github.com").replace(/\/$/, "")}/repos/${owner}/${repo}`;
      const issueUrlPrefix = `${(process.env.GITHUB_SERVER_URL || context.serverUrl || "https://github.com").replace(/\/$/, "")}/${owner}/${repo}/issues/`;
      const isBoundIssue = (issue, number) =>
        issue?.number === number &&
        typeof issue.repository_url === "string" &&
        issue.repository_url.toLowerCase() === repositoryUrl.toLowerCase() &&
        typeof issue.html_url === "string" &&
        issue.html_url.toLowerCase() === `${issueUrlPrefix}${number}`.toLowerCase();
      const status = await resolvePlanningStatus(client, owner, repo, message.status, config.allowed || []);
      const { data: current } = await client.rest.issues.get({ owner, repo, issue_number: issueNumber, headers: PLANNING_STATUS_HEADERS, request: { redirect: "error" } });
      if (!isBoundIssue(current, issueNumber)) throw new Error(`${ERR_VALIDATION}: Current issue does not match the authorized repository and issue number`);
      if (current.pull_request) throw new Error(`${ERR_VALIDATION}: Native planning statuses only apply to issues, not pull requests`);
      if (!["open", "closed"].includes(current.state)) throw new Error(`${ERR_API}: Cannot determine the current issue state`);
      if (!Object.hasOwn(current, "status")) throw new Error(`${ERR_API}: Cannot verify the current planning status because the issue response omitted status`);
      if (current.status && !["backlog", "planned", "started", "completed", "not_planned", "duplicate"].includes(current.status.category)) throw new Error(`${ERR_API}: Unsupported current planning status category`);
      const expected = planningStatusState(status);
      if (expected.state === "closed" && (current.state === "open" || current.state_reason !== expected.state_reason) && config.allow_close !== true)
        throw new Error(`${ERR_PERMISSION}: Closing an issue or changing its closed reason requires allow-close: true`);
      if (current.state === "closed" && (expected.state === "open" || current.state_reason !== expected.state_reason) && config.allow_reopen !== true)
        throw new Error(`${ERR_PERMISSION}: Reopening an issue or changing its closed reason requires allow-reopen: true`);
      let duplicateId;
      if (status.category === "duplicate") {
        if (!Number.isSafeInteger(message.duplicate_of) || message.duplicate_of <= 0 || message.duplicate_of === issueNumber)
          throw new Error(`${ERR_VALIDATION}: Duplicate status requires duplicate_of to reference another positive issue number`);
        const { data: duplicate } = await client.rest.issues.get({ owner, repo, issue_number: message.duplicate_of, headers: PLANNING_STATUS_HEADERS, request: { redirect: "error" } });
        if (duplicate.pull_request || !Number.isSafeInteger(duplicate.id) || duplicate.id <= 0) throw new Error(`${ERR_VALIDATION}: duplicate_of must reference an accessible issue, not a pull request`);
        if (!isBoundIssue(duplicate, message.duplicate_of)) throw new Error(`${ERR_VALIDATION}: duplicate_of does not match an issue in the authorized target repository`);
        duplicateId = duplicate.id;
      } else if (message.duplicate_of !== undefined) {
        throw new Error(`${ERR_VALIDATION}: duplicate_of is only supported for Duplicate status`);
      }
      const currentDuplicate = status.category === "duplicate" || current.status?.category === "duplicate" ? await fetchIssueDuplicate(client, owner, repo, issueNumber) : null;
      if (current.status?.category === "duplicate" && !currentDuplicate) throw new Error(`${ERR_API}: Cannot verify the current native duplicate issue relationship`);
      const sameDuplicate = duplicateId === undefined || (currentDuplicate?.id === duplicateId && currentDuplicate?.number === message.duplicate_of && currentDuplicate?.repo.toLowerCase() === itemRepo.toLowerCase());
      const retargetDuplicate = currentDuplicate && !sameDuplicate;
      if (retargetDuplicate && (config.allow_close !== true || config.allow_reopen !== true)) throw new Error(`${ERR_PERMISSION}: Changing the canonical duplicate issue requires both allow-close: true and allow-reopen: true`);
      if (current.status?.id === status.id && current.status?.category === status.category && current.state === expected.state && (expected.state === "open" || current.state_reason === expected.state_reason) && sameDuplicate) {
        return { success: true, skipped: true, reason: "Issue already has the requested planning status", issue_number: issueNumber, repo: itemRepo };
      }
      if (staged || forcePreview) {
        logStagedPreviewInfo(`Would set planning status to ${JSON.stringify(status.name)} on ${itemRepo}#${issueNumber}`);
        return { success: true, staged: true, previewInfo: { repo: itemRepo, issue_number: issueNumber, planning_status: status, ...(message.duplicate_of ? { duplicate_of: message.duplicate_of } : {}) } };
      }
      const { data: updated } = await client.request("PATCH /repos/{owner}/{repo}/issues/{issue_number}", {
        owner,
        repo,
        issue_number: issueNumber,
        headers: PLANNING_STATUS_HEADERS,
        request: { redirect: "error" },
        status: status.name,
        ...(duplicateId !== undefined ? { duplicate_issue_id: duplicateId } : {}),
      });
      let updatedDuplicate;
      try {
        verifyPlanningStatus(updated, status);
        updatedDuplicate = duplicateId !== undefined || currentDuplicate ? await fetchIssueDuplicate(client, owner, repo, issueNumber) : null;
        if (duplicateId !== undefined && (!updatedDuplicate || updatedDuplicate.id !== duplicateId || updatedDuplicate.number !== message.duplicate_of || updatedDuplicate.repo.toLowerCase() !== itemRepo.toLowerCase())) {
          throw new Error(`${ERR_API}: GitHub did not apply the requested canonical duplicate issue relationship`);
        }
        if (duplicateId === undefined && updatedDuplicate) throw new Error(`${ERR_API}: GitHub did not clear the previous duplicate issue relationship`);
      } catch (error) {
        return {
          success: false,
          error: `PATCH was sent to ${itemRepo}#${issueNumber} (${issueUrlPrefix}${issueNumber}); the requested status may have been applied, but verification failed: ${getErrorMessage(error)}`,
          issue_number: issueNumber,
          repo: itemRepo,
          url: `${issueUrlPrefix}${issueNumber}`,
        };
      }
      return {
        success: true,
        issue_number: issueNumber,
        repo: itemRepo,
        url: updated.html_url,
        before_state: planningStatusSnapshot(current, currentDuplicate),
        after_state: planningStatusSnapshot(updated, updatedDuplicate),
        metadata: { planning_status: status, ...(message.duplicate_of ? { duplicate_of: message.duplicate_of } : {}) },
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.warning(`Failed to set issue planning status: ${errorMessage}`);
      return { success: false, error: errorMessage };
    }
  };
  handleSetIssueStatus.validatePreview = (message, resolvedTemporaryIds, temporaryIdMap) => handleSetIssueStatus(message, resolvedTemporaryIds, temporaryIdMap, true);
  return handleSetIssueStatus;
}

module.exports = { main };
