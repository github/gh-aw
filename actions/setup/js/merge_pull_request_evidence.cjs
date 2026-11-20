// @ts-check

const { createHash } = require("node:crypto");
const { resolveTargetRepoConfig } = require("./repo_helpers.cjs");
const { parseStrictJSON } = require("./work_queue_codec.cjs");
const { SAFE_OUTPUT_E007 } = require("./error_codes.cjs");

/**
 * Read only the selected, independently produced check through the trusted API
 * client. Agent messages and evidence references are never proof sources.
 * @param {any} client
 * @param {string} owner
 * @param {string} repo
 * @param {number} pullNumber
 * @param {string} headSha
 * @param {any} config
 * @param {any[]} passingChecks
 */
async function collectMergeProof(client, owner, repo, pullNumber, headSha, config, passingChecks) {
  const proof = config.proof;
  const selected = passingChecks.find(check => check.name === proof.check);
  const appId = config.required_check_apps?.[proof.check];
  if (!selected || !Number.isSafeInteger(selected.check_run_id) || selected.check_run_id <= 0 || !Number.isSafeInteger(appId) || appId <= 0 || selected.app_id !== appId) {
    throw new Error(`${SAFE_OUTPUT_E007}: Merge proof requires a passing required check from the pinned verifier App`);
  }
  const { data: run } = await client.rest.checks.get({ owner, repo, check_run_id: selected.check_run_id });
  if (run?.id !== selected.check_run_id || run.name !== proof.check || run.app?.id !== appId || run.head_sha !== headSha || run.status !== "completed" || run.conclusion !== "success") {
    throw new Error(`${SAFE_OUTPUT_E007}: Merge proof check identity or result changed`);
  }
  if (typeof run.output?.text !== "string" || Buffer.byteLength(run.output.text) > 8192) {
    throw new Error(`${SAFE_OUTPUT_E007}: Missing or oversized verifier proof output`);
  }
  let binding;
  try {
    binding = parseStrictJSON(run.output.text);
  } catch (error) {
    throw new Error(`${SAFE_OUTPUT_E007}: Invalid verifier proof JSON`, { cause: error });
  }
  const expected = {
    version: "gh-aw.merge-proof.v1",
    repository: `${owner}/${repo}`,
    pull_request_number: pullNumber,
    head_sha: headSha,
    verifier: proof.verifier,
    verifier_version: proof.verifier_version,
    configuration_digest: proof.configuration_digest,
    result: "passed",
  };
  if (!binding || typeof binding !== "object" || Array.isArray(binding) || Object.keys(binding).length !== Object.keys(expected).length || Object.entries(expected).some(([key, value]) => binding[key] !== value)) {
    throw new Error(`${SAFE_OUTPUT_E007}: Missing, failed, Unknown, unsupported or mismatched merge proof; require a fresh passing verifier result`);
  }
  return { ...expected, check_run_id: run.id, app_id: appId };
}

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
    ...(config.proof ? { proof: config.proof } : {}),
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

module.exports = { collectMergeProof, mergeAuthorizationEvidence, reconcileMerge };
