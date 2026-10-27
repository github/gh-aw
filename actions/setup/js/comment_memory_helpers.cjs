// @ts-check

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");

const COMMENT_MEMORY_TAG = "gh-aw-comment-memory";
const COMMENT_MEMORY_DIR = "/tmp/gh-aw/comment-memory";
const COMMENT_MEMORY_EXTENSION = ".md";
const MAX_MEMORY_ID_LENGTH = 128;
const COMMENT_MEMORY_MAX_SCAN_PAGES = 50;
const COMMENT_MEMORY_MAX_SCAN_EMPTY_PAGES = 5;
const COMMENT_MEMORY_MAX_FILE_BYTES = 16 * 1024;
const COMMENT_MEMORY_MAX_TOTAL_BYTES = 48 * 1024;
const COMMENT_MEMORY_PROMPT_START_MARKER = "<!-- gh-aw-comment-memory-prompt:start -->";
const COMMENT_MEMORY_PROMPT_END_MARKER = "<!-- gh-aw-comment-memory-prompt:end -->";
const COMMENT_MEMORY_CODE_FENCE = "``````";
const ESCAPED_COMMENT_MEMORY_CODE_FENCE = COMMENT_MEMORY_CODE_FENCE.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/**
 * Builds the opening line of a code-fence memory block.
 * Format: ``````gh-aw-comment-memory:<memoryId>
 * @param {string} memoryId
 * @returns {string}
 */
function buildCodeFenceOpener(memoryId) {
  return `${COMMENT_MEMORY_CODE_FENCE}${COMMENT_MEMORY_TAG}:${memoryId}`;
}

function stripCommentMemoryCodeFence(content) {
  const trimmed = typeof content === "string" ? content.trim() : "";
  if (trimmed.length === 0) {
    return "";
  }
  if (!trimmed.startsWith(COMMENT_MEMORY_CODE_FENCE)) {
    return trimmed;
  }
  const match = trimmed.match(new RegExp(`^${ESCAPED_COMMENT_MEMORY_CODE_FENCE}[^\\n]*\\n([\\s\\S]*)\\n${ESCAPED_COMMENT_MEMORY_CODE_FENCE}$`));
  if (!match) {
    return trimmed;
  }
  return match[1].trim();
}

function isSafeMemoryId(memoryId) {
  if (typeof memoryId !== "string" || memoryId.length === 0 || memoryId.length > MAX_MEMORY_ID_LENGTH) {
    return false;
  }
  if (memoryId.includes("..") || memoryId.includes("/") || memoryId.includes("\\")) {
    return false;
  }
  return /^[A-Za-z0-9_-]+$/.test(memoryId);
}

/**
 * @param {string} commentBody
 * @param {(message: string) => void} [warn]
 * @returns {Array<{memoryId: string, content: string}>}
 */
function extractCommentMemoryEntries(commentBody, warn = () => {}) {
  if (!commentBody || typeof commentBody !== "string") {
    return [];
  }

  const entries = [];
  const seenIds = new Set();

  // New format: ``````gh-aw-comment-memory:<id>\ncontent\n``````
  // Use a broad character class and then validate with isSafeMemoryId() to keep validation in one place.
  const codeFenceOpenerPattern = new RegExp(`(?:^|\\n)(\`{6,})${COMMENT_MEMORY_TAG}:([^\\n]{1,${MAX_MEMORY_ID_LENGTH}})\\n([\\s\\S]*?)\\n\\1(?=\\n|$)`, "g");
  let match;
  while ((match = codeFenceOpenerPattern.exec(commentBody)) !== null) {
    const memoryId = match[2];
    const content = match[3].trim();
    if (isSafeMemoryId(memoryId)) {
      entries.push({ memoryId, content });
      seenIds.add(memoryId);
    } else {
      warn(`skipping unsafe memory_id '${memoryId}'`);
    }
  }

  // Legacy format (backward compat): <gh-aw-comment-memory id="...">...</gh-aw-comment-memory>
  const closeTag = `</${COMMENT_MEMORY_TAG}>`;
  let cursor = 0;
  while (cursor < commentBody.length) {
    const openStart = commentBody.indexOf(`<${COMMENT_MEMORY_TAG} id="`, cursor);
    if (openStart < 0) {
      break;
    }

    const idStart = openStart + `<${COMMENT_MEMORY_TAG} id="`.length;
    const idEnd = commentBody.indexOf('">', idStart);
    if (idEnd < 0) {
      break;
    }

    const memoryId = commentBody.slice(idStart, idEnd);
    const contentStart = idEnd + 2;
    const closeStart = commentBody.indexOf(closeTag, contentStart);
    if (closeStart < 0) {
      break;
    }

    if (!isSafeMemoryId(memoryId)) {
      warn(`skipping unsafe memory_id '${memoryId}'`);
    } else if (!seenIds.has(memoryId)) {
      entries.push({
        memoryId,
        content: stripCommentMemoryCodeFence(commentBody.slice(contentStart, closeStart)),
      });
    }

    cursor = closeStart + closeTag.length;
  }
  return entries;
}

function listCommentMemoryFiles(memoryDir = COMMENT_MEMORY_DIR) {
  if (!fs.existsSync(memoryDir)) {
    return [];
  }

  try {
    return fs
      .readdirSync(memoryDir)
      .filter(file => file.endsWith(COMMENT_MEMORY_EXTENSION))
      .sort()
      .map(file => ({
        memoryId: file.slice(0, -COMMENT_MEMORY_EXTENSION.length),
        filePath: path.join(memoryDir, file),
      }))
      .filter(entry => isSafeMemoryId(entry.memoryId));
  } catch (err) {
    throw new Error(`Failed to read comment-memory directory ${memoryDir}: ${getErrorMessage(err)}`, { cause: err });
  }
}

function resolveCommentMemoryConfig(config) {
  if (!config || typeof config !== "object") {
    return null;
  }
  return config["comment-memory"] || config.comment_memory || null;
}

module.exports = {
  COMMENT_MEMORY_TAG,
  COMMENT_MEMORY_DIR,
  COMMENT_MEMORY_EXTENSION,
  COMMENT_MEMORY_MAX_SCAN_PAGES,
  COMMENT_MEMORY_MAX_SCAN_EMPTY_PAGES,
  COMMENT_MEMORY_MAX_FILE_BYTES,
  COMMENT_MEMORY_MAX_TOTAL_BYTES,
  COMMENT_MEMORY_PROMPT_START_MARKER,
  COMMENT_MEMORY_PROMPT_END_MARKER,
  COMMENT_MEMORY_CODE_FENCE,
  isSafeMemoryId,
  buildCodeFenceOpener,
  stripCommentMemoryCodeFence,
  extractCommentMemoryEntries,
  listCommentMemoryFiles,
  resolveCommentMemoryConfig,
};
