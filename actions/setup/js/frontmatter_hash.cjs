// @ts-check

/**
 * Frontmatter hash computation for agentic workflows
 * This module provides hash computation using the pure JavaScript implementation
 * from frontmatter_hash_pure.cjs for production use.
 */

const { computeFrontmatterHash, extractHashFromLockFile } = require("./frontmatter_hash_pure.cjs");

module.exports = {
  computeFrontmatterHash,
  extractHashFromLockFile,
};
