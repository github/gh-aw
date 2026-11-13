// @ts-check
/// <reference types="@actions/github-script" />
require("./shim.cjs");

const fs = require("fs");
const path = require("path");
const { ERR_VALIDATION, ERR_SYSTEM } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { parseAllowedRepos, validateTargetRepo } = require("./repo_helpers.cjs");
const {
  COMMENT_MEMORY_DIR,
  COMMENT_MEMORY_MAX_SCAN_PAGES,
  COMMENT_MEMORY_MAX_SCAN_EMPTY_PAGES,
  COMMENT_MEMORY_MAX_FILE_BYTES,
  COMMENT_MEMORY_MAX_TOTAL_BYTES,
  COMMENT_MEMORY_PROMPT_START_MARKER,
  COMMENT_MEMORY_PROMPT_END_MARKER,
  extractCommentMemoryEntries,
  resolveCommentMemoryConfig,
} = require("./comment_memory_helpers.cjs");

const PROMPT_PATH = "/tmp/gh-aw/aw-prompts/prompt.txt";

function loadSafeOutputsConfig() {
  const configPath = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH || `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/config.json`;
  if (!fs.existsSync(configPath)) {
    return {};
  }
  try {
    return JSON.parse(fs.readFileSync(configPath, "utf8"));
  } catch (error) {
    core.warning(`comment_memory setup: failed to parse config at ${configPath}: ${getErrorMessage(error)}`);
    return {};
  }
}

function getCommentMemoryConfig(config) {
  return resolveCommentMemoryConfig(config);
}

function resolveTargetNumber(commentMemoryConfig) {
  const target = String(commentMemoryConfig?.target || "triggering").trim();
  if (target === "triggering") {
    return context.payload.issue?.number || context.payload.pull_request?.number || null;
  }

  if (target === "*") {
    return context.payload.issue?.number || context.payload.pull_request?.number || null;
  }

  const parsed = parseInt(target, 10);
  if (Number.isInteger(parsed) && parsed > 0) {
    return parsed;
  }
  return null;
}

function resolveTargetRepo(commentMemoryConfig) {
  const configuredRepo = String(commentMemoryConfig?.["target-repo"] || "").trim();
  const repoSlug = configuredRepo || `${context.repo.owner}/${context.repo.repo}`;
  const [owner, repo] = repoSlug.split("/");
  if (!owner || !repo) {
    return null;
  }
  return { owner, repo, slug: `${owner}/${repo}` };
}

function resolveCommentMemoryTarget(commentMemoryConfig) {
  const targetNumber = resolveTargetNumber(commentMemoryConfig);
  if (!targetNumber) {
    return null;
  }

  const targetRepo = resolveTargetRepo(commentMemoryConfig);
  if (!targetRepo) {
    throw new Error(`${ERR_VALIDATION}: invalid comment-memory target repo configuration`);
  }

  const contextRepoSlug = `${context.repo.owner}/${context.repo.repo}`;
  const normalizedTargetRepoSlug = targetRepo.slug.toLowerCase();
  const normalizedContextRepoSlug = contextRepoSlug.toLowerCase();
  const isCrossRepo = normalizedTargetRepoSlug !== normalizedContextRepoSlug;
  if (isCrossRepo) {
    const allowedRepos = parseAllowedRepos(commentMemoryConfig?.allowed_repos);
    if (allowedRepos.size === 0) {
      throw new Error(`${ERR_VALIDATION}: E004: Cross-repository comment-memory setup to '${targetRepo.slug}' is not permitted. No allowlist is configured. Define 'allowed_repos' to enable cross-repository access.`);
    }

    const repoValidation = validateTargetRepo(targetRepo.slug, contextRepoSlug, allowedRepos);
    if (!repoValidation.valid) {
      throw new Error(`${ERR_VALIDATION}: E004: ${repoValidation.error}`);
    }
    core.info(`comment_memory setup: cross-repo allowlist check passed for ${targetRepo.slug}`);
  }
  return { ...targetRepo, number: targetNumber };
}

async function collectCommentMemoryFiles(githubClient, commentMemoryConfig) {
  if (!resolveTargetRepo(commentMemoryConfig)) {
    core.warning("comment_memory setup: invalid comment-memory target repo configuration, skipping restore");
    return [];
  }
  let targetRepo;
  try {
    targetRepo = resolveCommentMemoryTarget(commentMemoryConfig);
  } catch (error) {
    core.warning(`comment_memory setup: ${getErrorMessage(error)}`);
    return [];
  }
  if (!targetRepo) {
    core.info("comment_memory setup: no resolvable target issue/PR number, skipping");
    return [];
  }
  const targetNumber = targetRepo.number;

  core.info(`comment_memory setup: loading managed comment memory from ${targetRepo.slug}#${targetNumber}`);
  const memoryMap = new Map();
  let emptyPageCount = 0;

  for (let page = 1; page <= COMMENT_MEMORY_MAX_SCAN_PAGES; page++) {
    const { data } = await githubClient.rest.issues.listComments({
      owner: targetRepo.owner,
      repo: targetRepo.repo,
      issue_number: targetNumber,
      per_page: 100,
      page,
    });

    if (!Array.isArray(data) || data.length === 0) {
      break;
    }

    let pageAddedEntries = 0;
    for (const comment of data) {
      const entries = extractCommentMemoryEntries(comment.body, warning => core.warning(`comment_memory setup: ${warning}`));
      for (const entry of entries) {
        const existing = memoryMap.get(entry.memoryId);
        if (existing !== entry.content) {
          pageAddedEntries++;
        }
        memoryMap.set(entry.memoryId, entry.content);
      }
    }

    if (pageAddedEntries === 0) {
      emptyPageCount++;
      // Stop early only after at least one managed entry was found; this avoids
      // missing entries that appear only in later pages on large threads.
      if (memoryMap.size > 0 && emptyPageCount >= COMMENT_MEMORY_MAX_SCAN_EMPTY_PAGES) {
        core.info(`comment_memory setup: stopping scan after ${emptyPageCount} pages without new memory entries`);
        break;
      }
    } else {
      emptyPageCount = 0;
    }

    if (data.length < 100) {
      break;
    }
  }

  try {
    fs.mkdirSync(COMMENT_MEMORY_DIR, { recursive: true });
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to create directory ${COMMENT_MEMORY_DIR}: ${getErrorMessage(err)}`, { cause: err });
  }
  let totalBytes = 0;
  for (const [memoryId, content] of memoryMap.entries()) {
    const writtenContent = `${content}\n`;
    const contentBytes = Buffer.byteLength(writtenContent, "utf8");
    if (contentBytes > COMMENT_MEMORY_MAX_FILE_BYTES) {
      throw new Error(`${ERR_VALIDATION}: comment_memory file '${memoryId}.md' is ${contentBytes} bytes, exceeding max ${COMMENT_MEMORY_MAX_FILE_BYTES} bytes`);
    }
    totalBytes += contentBytes;
    if (totalBytes > COMMENT_MEMORY_MAX_TOTAL_BYTES) {
      throw new Error(`${ERR_VALIDATION}: comment_memory total size ${totalBytes} bytes exceeds max ${COMMENT_MEMORY_MAX_TOTAL_BYTES} bytes`);
    }
  }

  const writtenFiles = [];
  for (const [memoryId, content] of memoryMap.entries()) {
    const filePath = path.join(COMMENT_MEMORY_DIR, `${memoryId}.md`);
    try {
      fs.writeFileSync(filePath, `${content}\n`);
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to write file ${filePath}: ${getErrorMessage(err)}`, { cause: err });
    }
    writtenFiles.push(filePath);
  }

  core.info(`comment_memory setup: wrote ${writtenFiles.length} memory file(s) to ${COMMENT_MEMORY_DIR}`);
  return writtenFiles;
}

function injectCommentMemoryPrompt(filePaths) {
  if (!fs.existsSync(PROMPT_PATH)) {
    core.info(`comment_memory setup: prompt file missing at ${PROMPT_PATH}, skipping prompt injection`);
    return;
  }

  const fileList = filePaths.length > 0 ? filePaths.map(file => `- ${file}`).join("\n") : "- (none yet; create new *.md files here when needed)";
  const injectedBlock = `${COMMENT_MEMORY_PROMPT_START_MARKER}
<comment-memory-files>
Comment memory files are editable markdown files under \`${COMMENT_MEMORY_DIR}\`.
Update existing files or create new \`<memory-id>.md\` files as needed.
These files are synced automatically after agent execution (no tool call required).
Available files:
${fileList}
</comment-memory-files>
${COMMENT_MEMORY_PROMPT_END_MARKER}`;

  let promptContent;
  try {
    promptContent = fs.readFileSync(PROMPT_PATH, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to read file ${PROMPT_PATH}: ${getErrorMessage(err)}`, { cause: err });
  }
  const start = promptContent.indexOf(COMMENT_MEMORY_PROMPT_START_MARKER);
  const end = promptContent.indexOf(COMMENT_MEMORY_PROMPT_END_MARKER);
  if (start >= 0 && end > start) {
    const suffixStart = end + COMMENT_MEMORY_PROMPT_END_MARKER.length;
    promptContent = `${promptContent.slice(0, start).trimEnd()}\n\n${injectedBlock}\n${promptContent.slice(suffixStart).trimStart()}`;
  } else {
    promptContent = `${promptContent.trimEnd()}\n\n${injectedBlock}\n`;
  }
  try {
    fs.writeFileSync(PROMPT_PATH, promptContent);
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to write file ${PROMPT_PATH}: ${getErrorMessage(err)}`, { cause: err });
  }
  core.info("comment_memory setup: injected comment-memory prompt guidance");
}

async function main() {
  const safeOutputsConfig = loadSafeOutputsConfig();
  const commentMemoryConfig = getCommentMemoryConfig(safeOutputsConfig);
  if (!commentMemoryConfig) {
    core.debug("comment_memory setup: comment-memory is not configured");
    return;
  }

  try {
    const files = await collectCommentMemoryFiles(github, commentMemoryConfig);
    injectCommentMemoryPrompt(files);
  } catch (error) {
    core.warning(`comment_memory setup: failed to prepare comment-memory files: ${getErrorMessage(error)}`);
  }
}

module.exports = {
  main,
  extractCommentMemoryEntries,
  resolveTargetNumber,
  resolveCommentMemoryTarget,
  loadSafeOutputsConfig,
};
