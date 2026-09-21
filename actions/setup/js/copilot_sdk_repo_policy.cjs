// @ts-check

"use strict";

const { checkFileProtectionPostApply } = require("./manifest_file_helpers.cjs");
const { parseAllowedBranchPatterns, isAllowedBranch } = require("./branch_pattern_helpers.cjs");
const { resolveEnvPlaceholders } = require("./safe_outputs_config.cjs");

/** @typedef {import("./types/handler-factory").HandlerConfig} RepositoryPolicy */
/** @typedef {{id: "go-repository", repositoryDefaultBranch: string, policy: RepositoryPolicy}} GoRepositoryProfile */

const STRING_FIELDS = new Set(["target-repo", "patch_workspace_path", "current_checkout_repo", "base_branch", "branch_prefix", "protected_files_policy"]);
const ARRAY_FIELDS = new Set(["allowed_base_branches", "allowed_branches", "allowed_files", "excluded_files", "protected_files", "protected_path_prefixes", "protected_dot_folder_excludes"]);
const BOOLEAN_FIELDS = new Set(["preserve_branch_name", "recreate_ref", "protect_top_level_dot_folders"]);
const LIMIT_FIELDS = new Set(["max_patch_size", "max_patch_files"]);
const PROFILE_BINDINGS = new Set([
  "GH_AW_GITHUB_EVENT_REPOSITORY_DEFAULT_BRANCH",
  "GITHUB_REPOSITORY",
  "GITHUB_REPOSITORY_OWNER",
  "GITHUB_SHA",
  "GITHUB_REF",
  "GITHUB_REF_NAME",
  "GITHUB_REF_TYPE",
  "GITHUB_HEAD_REF",
  "GITHUB_BASE_REF",
  "GITHUB_ACTOR",
  "GITHUB_TRIGGERING_ACTOR",
  "GITHUB_RUN_ID",
  "GITHUB_RUN_NUMBER",
  "GITHUB_RUN_ATTEMPT",
  "GITHUB_EVENT_NAME",
]);

/** @param {unknown} value */
function validateProfileBindings(value) {
  if (Array.isArray(value)) {
    for (const item of value) validateProfileBindings(item);
  } else if (isRecord(value)) {
    for (const item of Object.values(value)) validateProfileBindings(item);
  } else if (typeof value === "string") {
    for (const match of value.matchAll(/\$\{([A-Z_][A-Z0-9_]*)\}/g)) {
      const name = match[1];
      if (!PROFILE_BINDINGS.has(name) && !/^GH_AW_INPUT_[A-Z0-9_]+$/.test(name)) throw new Error("SDK repository profile contains an unsupported runtime binding");
      if (process.env[name] === undefined) throw new Error("SDK repository profile contains an unresolved runtime binding");
    }
  }
}

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {unknown} value @returns {GoRepositoryProfile} */
function parseGoRepositoryProfile(value) {
  validateProfileBindings(value);
  value = resolveEnvPlaceholders(value);
  if (!isRecord(value) || value.id !== "go-repository") throw new Error("SDK v2 tool contract requires profile.id go-repository");
  if (Object.keys(value).some(key => !["id", "repositoryDefaultBranch", "policy"].includes(key))) throw new Error("SDK repository profile contains an unknown field");
  if (typeof value.repositoryDefaultBranch !== "string" || !value.repositoryDefaultBranch.trim() || /[\x00-\x1f\x7f]|\$\{/.test(value.repositoryDefaultBranch)) {
    throw new Error("SDK repository profile requires a resolved repositoryDefaultBranch");
  }
  if (!isRecord(value.policy)) throw new Error("SDK repository profile requires a publication policy object");
  /** @type {RepositoryPolicy} */
  const policy = {};
  for (const [key, entry] of Object.entries(value.policy)) {
    if (STRING_FIELDS.has(key)) {
      if (typeof entry !== "string" || /[\x00-\x1f\x7f]|\$\{/.test(entry)) throw new Error(`SDK repository policy ${key} must be a resolved string`);
      policy[key] = entry;
    } else if (ARRAY_FIELDS.has(key)) {
      if (!Array.isArray(entry) || entry.some(item => typeof item !== "string" || !item.trim() || /[\x00-\x1f\x7f]|\$\{/.test(item))) {
        throw new Error(`SDK repository policy ${key} must be an array of resolved non-empty strings`);
      }
      policy[key] = [...entry];
    } else if (BOOLEAN_FIELDS.has(key)) {
      if (typeof entry !== "boolean") throw new Error(`SDK repository policy ${key} must be a boolean`);
      policy[key] = entry;
    } else if (LIMIT_FIELDS.has(key)) {
      const number = typeof entry === "string" && /^[0-9]+$/.test(entry) ? Number(entry) : entry;
      if (typeof number !== "number" || !Number.isSafeInteger(number) || number <= 0) throw new Error(`SDK repository policy ${key} must be a positive integer`);
      policy[key] = number;
    } else {
      throw new Error("SDK repository policy contains an unsupported or potentially sensitive field");
    }
  }
  if (policy.protected_files_policy !== undefined && !["allowed", "blocked", "fallback-to-issue", "request-review", "request_review"].includes(policy.protected_files_policy)) {
    throw new Error("SDK repository policy contains an unknown protected-files policy");
  }
  return { id: "go-repository", repositoryDefaultBranch: value.repositoryDefaultBranch, policy };
}

/**
 * Check Git's already-excluded file list with the existing publication rules.
 * Review and fallback policies are not unconditional edit denials.
 * @param {string[]} files
 * @param {RepositoryPolicy} policy
 */
function checkRepositoryPublicationFiles(files, policy) {
  const protection = checkFileProtectionPostApply(files, policy);
  if (protection.action === "deny") throw new Error(`Repository changes violate the create-pull-request file policy: ${protection.files.join(", ")}`);
  return protection;
}

/** @param {string} branch @param {GoRepositoryProfile} profile */
function validateRepositoryBranch(branch, profile) {
  if (
    typeof branch !== "string" ||
    branch.length > 200 ||
    !/^[A-Za-z0-9_][A-Za-z0-9_./-]*$/.test(branch) ||
    branch.includes("..") ||
    branch.endsWith("/") ||
    branch.endsWith(".") ||
    branch.split("/").some(part => !part || part.startsWith(".") || part.endsWith(".lock"))
  ) {
    throw new Error("prepare_branch requires a safe, literal Git branch name");
  }
  if (branch === "HEAD" || branch === profile.repositoryDefaultBranch || branch === profile.policy.base_branch) throw new Error("prepare_branch cannot select a repository default or pull-request base branch");
  const allowed = parseAllowedBranchPatterns(profile.policy.allowed_branches);
  if (allowed.length && !isAllowedBranch(branch, allowed)) throw new Error("prepare_branch does not match create-pull-request allowed-branches");
}

module.exports = { parseGoRepositoryProfile, checkRepositoryPublicationFiles, validateRepositoryBranch };
