// @ts-check

const { createHash } = require("node:crypto");
const { resolveTargetRepoConfig } = require("./repo_helpers.cjs");

/**
 * Fingerprint resolved policy, never credentials or agent-supplied merge options.
 * The fixed field order and sorted collections make equivalent policies stable.
 * @param {import('./types/handler-factory').HandlerConfig} config
 * @param {string[]} requiredChecks
 */
function mergeAuthorizationEvidence(config, requiredChecks) {
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const sorted = values => [...(values || [])].sort();
  const policy = {
    version: 1,
    target: config.target || "triggering",
    target_repo: defaultTargetRepo,
    allowed_repos: sorted([...allowedRepos]),
    max: Number(config.max || 1),
    allowed_base_branches: sorted(config.allowed_base_branches),
    allowed_branches: sorted(config.allowed_branches),
    required_labels: sorted(config.required_labels),
    required_title_prefix: config.required_title_prefix || "",
    required_checks: sorted(requiredChecks),
    required_check_apps: Object.entries(config.required_check_apps || {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)),
    required_approvers: sorted((config.required_approvers || []).map(login => login.toLowerCase())),
    protected_files_policy: config.protected_files_policy || "blocked",
    protected_files: sorted(config.protected_files),
    protected_path_prefixes: sorted(config.protected_path_prefixes),
    protected_files_path_exclude: sorted(config.protected_files_path_exclude),
    protect_top_level_dot_folders: Boolean(config.protect_top_level_dot_folders),
    protected_dot_folder_excludes: sorted(config.protected_dot_folder_excludes),
    allowed_files: sorted(config.allowed_files),
    detection_required: String(process.env.GH_AW_RUN_DETECTION || "").trim() === "true",
  };
  return {
    policy_version: 1,
    policy_sha256: createHash("sha256").update(JSON.stringify(policy)).digest("hex"),
    required_checks: "not_evaluated",
    independent_approval: policy.required_approvers.length > 0 ? "not_evaluated" : "not_required",
    protected_files: policy.protected_files_policy === "allowed" ? "not_required" : "not_evaluated",
    final_revalidation: "not_evaluated",
  };
}

/**
 * A negative read cannot disprove a committed write after a lost response.
 * Only a merged PR on the evaluated head with a valid merge revision confirms it.
 * @param {any} client
 * @param {string} owner
 * @param {string} repo
 * @param {number} pullNumber
 * @param {string} headSha
 * @returns {Promise<string|null>}
 */
async function reconcileMerge(client, owner, repo, pullNumber, headSha) {
  try {
    const { data } = await client.rest.pulls.get({ owner, repo, pull_number: pullNumber });
    if (data?.merged === true && data.head?.sha === headSha && typeof data.merge_commit_sha === "string" && /^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$/.test(data.merge_commit_sha)) {
      return data.merge_commit_sha;
    }
    core.warning("Merge outcome remains indeterminate after read-only reconciliation; do not retry the merge automatically");
  } catch {
    core.warning("Read-only merge reconciliation failed; the merge outcome remains indeterminate");
  }
  return null;
}

module.exports = { mergeAuthorizationEvidence, reconcileMerge };
