// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * @param {string} rawSkills
 * @returns {string[]}
 */
function parseSkillSpecs(rawSkills) {
  return (rawSkills || "")
    .split(/\r?\n/)
    .map(skill => skill.trim())
    .filter(Boolean);
}

/**
 * @typedef {{args: string[]; displaySpec: string}} SkillInstallCommand
 */

/**
 * Reports whether a skill spec is a local path reference — one that has no
 * "@" separator and is not a GitHub Actions expression.  Local refs are
 * installed with --from-local instead of fetching from a remote repository.
 *
 * @param {string} skillSpec
 * @returns {boolean}
 */
function isLocalSkillRef(skillSpec) {
  return skillSpec !== "" && !skillSpec.startsWith("${{") && !skillSpec.includes("@");
}

/**
 * @param {string} skillSpec
 * @param {string} skillsDst
 * @param {string} skillInstallAgent
 * @returns {SkillInstallCommand}
 */
function buildSkillInstallCommand(skillSpec, skillsDst, skillInstallAgent = "") {
  const agentArgs = skillInstallAgent ? ["--agent", skillInstallAgent] : [];

  // Local path reference: install from the filesystem using --from-local.
  // The skill name is derived from the last path component.
  if (isLocalSkillRef(skillSpec)) {
    const skillName = skillSpec.replace(/\\/g, "/").split("/").filter(Boolean).pop() || skillSpec;
    return {
      displaySpec: skillSpec,
      args: ["skill", "install", skillSpec, skillName, "--from-local", ...agentArgs, "--dir", skillsDst, "--force"],
    };
  }

  const atIndex = skillSpec.lastIndexOf("@");
  const hasPin = atIndex >= 0;
  const skillBase = hasPin ? skillSpec.slice(0, atIndex) : skillSpec;
  const skillRef = hasPin ? skillSpec.slice(atIndex + 1) : "";
  const parts = skillBase.split("/");
  const pinArgs = skillRef ? ["--pin", skillRef] : [];

  if (parts.length >= 3) {
    return {
      displaySpec: skillSpec,
      args: ["skill", "install", `${parts[0]}/${parts[1]}`, parts.slice(2).join("/"), ...pinArgs, ...agentArgs, "--dir", skillsDst, "--force"],
    };
  }

  if (parts.length === 2) {
    return {
      displaySpec: skillSpec,
      args: ["skill", "install", skillBase, "--all", ...pinArgs, ...agentArgs, "--dir", skillsDst, "--force"],
    };
  }

  return {
    displaySpec: skillSpec,
    args: ["skill", "install", skillSpec, ...agentArgs, "--dir", skillsDst, "--force"],
  };
}

/**
 * @param {string} skillsDst
 * @returns {number}
 */
function countInstalledSkillFiles(skillsDst) {
  if (!fs.existsSync(skillsDst)) {
    return 0;
  }

  let count = 0;
  const stack = [skillsDst];
  while (stack.length > 0) {
    const currentDir = stack.pop();
    if (!currentDir) {
      continue;
    }
    let entries;
    try {
      entries = fs.readdirSync(currentDir, { withFileTypes: true });
    } catch (err) {
      throw new Error(`Failed to read installed skills directory ${currentDir}: ${getErrorMessage(err)}`, { cause: err });
    }
    for (const entry of entries) {
      const entryPath = path.join(currentDir, entry.name);
      if (entry.isDirectory()) {
        stack.push(entryPath);
        continue;
      }
      if (entry.isFile() && entry.name === "SKILL.md") {
        count++;
      }
    }
  }

  return count;
}

/** Path to the shared skill install failures JSON file within the activation job's runner. */
const SKILL_FAILURES_FILE = "/tmp/gh-aw/skill_install_failures.json";

/**
 * Append a skill install failure record to the shared failures file.
 * All install steps within the same activation job share the same runner,
 * so this file persists across steps and is read by the collect step.
 * @param {string} skillSpec
 * @param {string} errorMessage
 */
function appendSkillInstallFailure(skillSpec, errorMessage) {
  let failures = [];
  try {
    if (fs.existsSync(SKILL_FAILURES_FILE)) {
      const raw = fs.readFileSync(SKILL_FAILURES_FILE, "utf8");
      const parsed = JSON.parse(raw);
      failures = Array.isArray(parsed) ? parsed.filter(entry => entry && typeof entry.skill === "string" && typeof entry.error === "string") : [];
    }
  } catch (parseErr) {
    // If reading/parsing fails, start fresh and warn so the issue is visible in logs
    core.warning(`Could not read skill install failures file, starting fresh: ${getErrorMessage(parseErr)}`);
    failures = [];
  }
  failures.push({ skill: skillSpec, error: errorMessage });
  try {
    fs.mkdirSync(path.dirname(SKILL_FAILURES_FILE), { recursive: true });
    fs.writeFileSync(SKILL_FAILURES_FILE, JSON.stringify(failures, null, 2), "utf8");
  } catch (writeErr) {
    core.warning(`Could not write skill install failures file: ${getErrorMessage(writeErr)}`);
  }
}

/**
 * @param {string} skillDir
 * @param {string[]} skills
 * @param {number} installedSkillCount
 * @param {Array<{skill: string; error: string}>} failures
 * @returns {Promise<void>}
 */
async function writeSkillSummary(skillDir, skills, installedSkillCount, failures) {
  let body = "";
  body += `- Engine skill directory: \`${skillDir}\`\n`;
  body += `- Requested references: \`${JSON.stringify(skills)}\`\n`;
  body += `- Installed SKILL.md files: ${installedSkillCount}\n`;
  if (failures.length > 0) {
    body += "\n#### Skill install failures\n\n";
    for (const f of failures) {
      body += `- \`${f.skill}\`: ${f.error}\n`;
    }
  }
  const openAttr = failures.length > 0 ? " open" : "";
  core.summary.addRaw(`<details${openAttr}>\n<summary>Skill install details</summary>\n\n${body}\n</details>\n\n`);
  await core.summary.write();
}

async function main() {
  const skillDir = process.env.GH_AW_SKILL_DIR || "";
  const skillInstallAgent = process.env.GH_AW_GH_SKILL_AGENT_NAME || "";
  const skills = parseSkillSpecs(process.env.GH_AW_FRONTMATTER_SKILLS || "");
  const skillsDst = path.join("/tmp/gh-aw", skillDir);

  try {
    fs.mkdirSync(skillsDst, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create directory ${skillsDst}: ${getErrorMessage(err)}`, { cause: err });
  }

  core.info(`Installing frontmatter skills to ${skillsDst}`);
  if (skillInstallAgent) {
    core.info(`Installing frontmatter skills for gh skill agent ${skillInstallAgent}`);
  }
  core.info("Existing skills at destination may be replaced (--force) to ensure pinned refs are up to date");

  /** @type {Array<{skill: string; error: string}>} */
  const failures = [];

  for (const skillSpec of skills) {
    core.info(`Installing skill reference: ${skillSpec}`);
    const command = buildSkillInstallCommand(skillSpec, skillsDst, skillInstallAgent);
    try {
      await exec.exec("gh", command.args);
    } catch (err) {
      const errorMessage = getErrorMessage(err).replace(/\r?\n/g, " ");
      core.warning(`Failed to install skill '${skillSpec}': ${errorMessage}`);
      failures.push({ skill: skillSpec, error: errorMessage });
      appendSkillInstallFailure(skillSpec, errorMessage);
    }
  }

  const installedSkillCount = countInstalledSkillFiles(skillsDst);
  core.info(`Installed ${installedSkillCount} skill file(s)`);
  if (failures.length > 0) {
    core.warning(`${failures.length} skill(s) failed to install — details will be reported in the agent failure issue/comment`);
  }
  await writeSkillSummary(skillDir, skills, installedSkillCount, failures);
}

module.exports = { main, parseSkillSpecs, buildSkillInstallCommand, isLocalSkillRef, countInstalledSkillFiles, appendSkillInstallFailure };
