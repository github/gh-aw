// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** Path written by install_frontmatter_skills.cjs during the activation job. */
const SKILL_FAILURES_FILE = "/tmp/gh-aw/skill_install_failures.json";

/**
 * Read the shared failures file produced by install_frontmatter_skills.cjs.
 * Returns an empty array when the file does not exist or cannot be parsed.
 * @returns {Array<{skill: string; error: string}>}
 */
function readSkillInstallFailures() {
  try {
    if (!fs.existsSync(SKILL_FAILURES_FILE)) {
      return [];
    }
    const raw = fs.readFileSync(SKILL_FAILURES_FILE, "utf8");
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed.filter(entry => entry && typeof entry.skill === "string" && typeof entry.error === "string");
  } catch (readErr) {
    // Warn so "no failures" vs "couldn't read failures file" is distinguishable in logs
    core.warning(`Could not read skill install failures file: ${getErrorMessage(readErr)}`);
    return [];
  }
}

async function main() {
  const failures = readSkillInstallFailures();
  const failureCount = failures.length;

  core.info(`Skill install failures detected: ${failureCount}`);

  core.setOutput("failure_count", String(failureCount));
  core.setOutput("errors", failures.map(f => `${f.skill}\t${f.error.replace(/\r?\n/g, " ").replace(/\t/g, " ")}`).join("\n"));

  if (failureCount > 0) {
    core.setFailed(`${failureCount} skill(s) failed to install — see agent failure issue/comment for details`);
  }
}

module.exports = { main, readSkillInstallFailures };
