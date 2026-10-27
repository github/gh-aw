// @ts-check

const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");

const WORKFLOW_LOCK_PATTERN = ".github/workflows/**.lock.yml";
const WORKFLOW_COMPILATION_NOTICE =
  "> [!WARNING]\n> Changes to workflow YAML files were excluded from this changeset to avoid GitHub token workflow-write restrictions. Run `gh aw compile` again on this branch and commit the regenerated `.lock.yml` files using credentials permitted to update GitHub Actions workflows before merging.";

/**
 * @param {string} file
 * @returns {boolean}
 */
function isWorkflowYamlFile(file) {
  return /^\.github\/workflows\/.+\.ya?ml$/.test(file);
}

/**
 * @param {Record<string, any>} config
 * @returns {string[]}
 */
function getExcludedFiles(config) {
  const patterns = Array.isArray(config.excluded_files) ? [...config.excluded_files] : [];
  if (config.exclude_lock_yml === true && !patterns.includes(WORKFLOW_LOCK_PATTERN)) {
    patterns.push(WORKFLOW_LOCK_PATTERN);
  }
  return patterns;
}

/**
 * @param {unknown} files
 * @param {string[]} patterns
 * @returns {string[]}
 */
function getExcludedWorkflowFiles(files, patterns) {
  const matchers = patterns.map(pattern => globPatternToRegex(pattern));
  return Array.isArray(files) ? files.filter(file => typeof file === "string" && isWorkflowYamlFile(file) && matchers.some(matcher => matcher.test(file))) : [];
}

/**
 * @param {Record<string, any>} item
 * @param {Record<string, any>} config
 * @returns {string}
 */
function getWorkflowCompilationNotice(item, config) {
  return getExcludedFiles(config).length > 0 && typeof item.message === "string" && item.message.includes(WORKFLOW_COMPILATION_NOTICE) ? WORKFLOW_COMPILATION_NOTICE : "";
}

module.exports = { getExcludedFiles, getExcludedWorkflowFiles, getWorkflowCompilationNotice, isWorkflowYamlFile, WORKFLOW_COMPILATION_NOTICE };
