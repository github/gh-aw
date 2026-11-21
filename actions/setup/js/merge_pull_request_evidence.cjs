// @ts-check

const { createHash } = require("node:crypto");
const { resolveTargetRepoConfig } = require("./repo_helpers.cjs");
const { parseStrictJSON } = require("./work_queue_codec.cjs");
const { SAFE_OUTPUT_E007 } = require("./error_codes.cjs");

const MERGE_PROOF_EVIDENCE_ERROR = `${SAFE_OUTPUT_E007}: Missing, unsupported, failed or mismatched merge proof evidence; obtain a passed result from the configured deterministic verifier for this exact head and configuration`;

/**
 * Only the selected, producer-pinned GitHub check supplies proof, never the
 * agent message. The verifier owns the semantics of its configuration digest.
 * @param {any} run
 * @param {NonNullable<import('./types/handler-factory').HandlerConfig['proof_evidence']>} policy
 * @param {string} repository
 * @param {number} pullNumber
 * @param {string} headSha
 */
function readMergeProofEvidence(run, policy, repository, pullNumber, headSha) {
  /** @returns {never} */
  const reject = () => {
    throw new Error(MERGE_PROOF_EVIDENCE_ERROR);
  };
  if (!run || !Number.isSafeInteger(run.id) || run.id <= 0 || run.head_sha !== headSha || run.status !== "completed" || run.conclusion !== "success") reject();
  let proof;
  try {
    proof = parseStrictJSON(run.output?.summary, { maxBytes: 8192 });
  } catch {
    reject();
  }
  const expected = {
    version: "gh-aw.merge-proof.v1",
    repository,
    pull_request_number: pullNumber,
    head_sha: headSha,
    verifier_version: policy.verifier_version,
    configuration_sha256: policy.configuration_sha256,
    result: "passed",
  };
  if (
    !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(headSha) ||
    !proof ||
    typeof proof !== "object" ||
    Array.isArray(proof) ||
    Object.keys(proof).length !== Object.keys(expected).length ||
    Object.entries(expected).some(([key, value]) => proof[key] !== value)
  )
    reject();
  return Object.freeze({ ...expected, check_run_id: run.id, app_id: run.app.id });
}

/** @param {ReturnType<typeof readMergeProofEvidence>} proof */
function mergeProofScope(proof) {
  return {
    binding: "resolved_effect",
    refs: [
      `repository:${proof.repository}`,
      `pull-request:${proof.repository}#${proof.pull_request_number}`,
      `head-sha:${proof.head_sha}`,
      `check-run:${proof.repository}/${proof.check_run_id}`,
      `check-app:${proof.app_id}`,
      `verifier-version:${proof.verifier_version}`,
      `configuration-sha256:${proof.configuration_sha256}`,
      `verification-result:${proof.result}`,
      `sha256:${createHash("sha256").update(JSON.stringify(proof)).digest("hex")}`,
    ],
  };
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
  const policyVersion = config.proof_evidence ? 2 : 1;
  const policy = {
    version: policyVersion,
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
    ...(config.proof_evidence ? { proof_evidence: config.proof_evidence } : {}),
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
    policy_version: policyVersion,
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

module.exports = { MERGE_PROOF_EVIDENCE_ERROR, mergeAuthorizationEvidence, reconcileMerge, readMergeProofEvidence, mergeProofScope };
