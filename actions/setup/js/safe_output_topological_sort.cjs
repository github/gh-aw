// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Topological Sort for Safe Output Tool Calls
 *
 * This module provides topological sorting of safe output messages based on
 * temporary ID dependencies. Messages that create entities without referencing
 * temporary IDs are processed first, followed by messages that depend on them.
 *
 * This enables resolution of all temporary IDs in a single pass for acyclic
 * dependency graphs (graphs without loops).
 */

const { isTemporaryId, normalizeTemporaryId } = require("./temporary_id.cjs");

/**
 * Regex pattern for matching temporary ID references in text
 * Format: #aw_XXXXXXXXXXXX (aw_ prefix + 12 hex characters)
 * This pattern is also defined in temporary_id.cjs for consistency
 */
const TEMPORARY_ID_PATTERN = /#(aw_[0-9a-f]{12})/gi;

/**
 * Extract all temporary ID references from a message
 * Checks fields that commonly contain temporary IDs:
 * - body (for create_issue, create_discussion, add_comment)
 * - parent_issue_number, sub_issue_number (for link_sub_issue)
 * - issue_number (for add_comment, update_issue, etc.)
 * - discussion_number (for create_discussion, update_discussion)
 *
 * @param {any} message - The safe output message
 * @returns {Set<string>} Set of normalized temporary IDs referenced by this message
 */
function extractTemporaryIdReferences(message) {
  const tempIds = new Set();

  if (!message || typeof message !== "object") {
    return tempIds;
  }

  // Check text fields for #aw_XXXXXXXXXXXX references
  const textFields = ["body", "title", "description"];
  for (const field of textFields) {
    if (typeof message[field] === "string") {
      let match;
      while ((match = TEMPORARY_ID_PATTERN.exec(message[field])) !== null) {
        tempIds.add(normalizeTemporaryId(match[1]));
      }
    }
  }

  // Check direct ID reference fields
  const idFields = ["parent_issue_number", "sub_issue_number", "issue_number", "discussion_number", "pull_request_number"];

  for (const field of idFields) {
    const value = message[field];
    if (value !== undefined && value !== null) {
      // Strip # prefix if present
      const valueStr = String(value).trim();
      const valueWithoutHash = valueStr.startsWith("#") ? valueStr.substring(1) : valueStr;

      if (isTemporaryId(valueWithoutHash)) {
        tempIds.add(normalizeTemporaryId(valueWithoutHash));
      }
    }
  }

  // Check items array for bulk operations (e.g., add_comment with multiple targets)
  if (Array.isArray(message.items)) {
    for (const item of message.items) {
      if (item && typeof item === "object") {
        const itemTempIds = extractTemporaryIdReferences(item);
        for (const tempId of itemTempIds) {
          tempIds.add(tempId);
        }
      }
    }
  }

  return tempIds;
}

/**
 * Get the temporary ID that a message will create (if any)
 * Only messages with a temporary_id field will create a new entity
 *
 * @param {any} message - The safe output message
 * @returns {string|null} Normalized temporary ID that will be created, or null
 */
function getCreatedTemporaryId(message) {
  if (!message || typeof message !== "object") {
    return null;
  }

  const tempId = message.temporary_id;
  if (tempId && isTemporaryId(String(tempId))) {
    return normalizeTemporaryId(String(tempId));
  }

  return null;
}

/**
 * Build a dependency graph for safe output messages
 * Returns:
 * - dependencies: Map of message index -> Set of message indices it depends on
 * - providers: Map of temporary ID -> message index that creates it
 *
 * @param {Array<any>} messages - Array of safe output messages
 * @returns {{dependencies: Map<number, Set<number>>, providers: Map<string, number>}}
 */
function buildDependencyGraph(messages) {
  /** @type {Map<number, Set<number>>} */
  const dependencies = new Map();

  /** @type {Map<string, number>} */
  const providers = new Map();

  // First pass: identify which messages create which temporary IDs
  for (let i = 0; i < messages.length; i++) {
    const message = messages[i];
    const createdId = getCreatedTemporaryId(message);

    if (createdId !== null) {
      if (providers.has(createdId)) {
        // Duplicate temporary ID - this is a problem
        // We'll let the handler deal with this, but note it
        if (typeof core !== "undefined") {
          core.warning(`Duplicate temporary_id '${createdId}' at message indices ${providers.get(createdId)} and ${i}. ` + `Only the first occurrence will be used.`);
        }
      } else {
        providers.set(createdId, i);
      }
    }

    // Initialize dependencies set for this message
    dependencies.set(i, new Set());
  }

  // Second pass: identify dependencies
  for (let i = 0; i < messages.length; i++) {
    const message = messages[i];
    const referencedIds = extractTemporaryIdReferences(message);

    // For each temporary ID this message references, find the provider
    for (const tempId of referencedIds) {
      const providerIndex = providers.get(tempId);

      if (providerIndex !== undefined) {
        // This message depends on the provider message
        const deps = dependencies.get(i);
        if (deps) {
          deps.add(providerIndex);
        }
      }
      // If no provider, the temp ID might be from a previous step or be unresolved
      // We don't add a dependency in this case
    }
  }

  return { dependencies, providers };
}

/**
 * Detect cycles in the dependency graph
 * Returns an array of message indices that form a cycle, or empty array if no cycle
 *
 * @param {Map<number, Set<number>>} dependencies - Dependency graph
 * @returns {Array<number>} Indices of messages forming a cycle, or empty array
 */
function detectCycle(dependencies) {
  const visited = new Set();
  const recursionStack = new Set();
  /** @type {Array<number>} */
  const path = [];
  /** @type {Array<number>} */
  let foundCycle = [];

  /**
   * DFS to detect cycle
   * @param {number} node - Current node index
   * @returns {boolean} True if cycle detected
   */
  function dfs(node) {
    visited.add(node);
    recursionStack.add(node);
    path.push(node);

    const deps = dependencies.get(node) || new Set();
    for (const dep of deps) {
      if (!visited.has(dep)) {
        if (dfs(dep)) {
          return true;
        }
      } else if (recursionStack.has(dep)) {
        // Cycle detected - extract cycle from path
        const cycleStart = path.indexOf(dep);
        foundCycle = path.slice(cycleStart);
        return true;
      }
    }

    recursionStack.delete(node);
    path.pop();
    return false;
  }

  // Check each unvisited node
  for (const node of dependencies.keys()) {
    if (!visited.has(node)) {
      if (dfs(node)) {
        return foundCycle;
      }
    }
  }

  return [];
}

/**
 * Perform topological sort on messages using Kahn's algorithm
 * Messages without dependencies come first, followed by their dependents
 *
 * @param {Array<any>} messages - Array of safe output messages
 * @param {Map<number, Set<number>>} dependencies - Dependency graph
 * @returns {Array<number>} Array of message indices in topologically sorted order
 */
function topologicalSort(messages, dependencies) {
  // Calculate in-degree (number of dependencies) for each message
  const inDegree = new Map();
  for (let i = 0; i < messages.length; i++) {
    const deps = dependencies.get(i) || new Set();
    inDegree.set(i, deps.size);
  }

  // Queue of messages with no dependencies
  const queue = [];
  for (let i = 0; i < messages.length; i++) {
    if (inDegree.get(i) === 0) {
      queue.push(i);
    }
  }

  const sorted = [];

  while (queue.length > 0) {
    // Process nodes in order of appearance for stability
    // This preserves the original order when there are no dependencies
    const node = queue.shift();
    if (node !== undefined) {
      sorted.push(node);

      // Find all messages that depend on this one
      for (const [other, deps] of dependencies.entries()) {
        if (deps.has(node)) {
          // Reduce in-degree
          const currentDegree = inDegree.get(other);
          if (currentDegree !== undefined) {
            inDegree.set(other, currentDegree - 1);

            // If all dependencies satisfied, add to queue
            if (inDegree.get(other) === 0) {
              queue.push(other);
            }
          }
        }
      }
    }
  }

  // If sorted.length < messages.length, there's a cycle
  if (sorted.length < messages.length) {
    const unsorted = [];
    for (let i = 0; i < messages.length; i++) {
      if (!sorted.includes(i)) {
        unsorted.push(i);
      }
    }

    if (typeof core !== "undefined") {
      core.warning(`Topological sort incomplete: ${sorted.length}/${messages.length} messages sorted. ` + `Messages ${unsorted.join(", ")} may be part of a dependency cycle.`);
    }
  }

  return sorted;
}

/**
 * Sort safe output messages in topological order based on temporary ID dependencies
 * Messages that don't reference temporary IDs are processed first, followed by
 * messages that depend on them. This enables single-pass resolution of temporary IDs.
 *
 * If a cycle is detected, the original order is preserved and a warning is logged.
 *
 * @param {Array<any>} messages - Array of safe output messages
 * @returns {Array<any>} Messages in topologically sorted order
 */
function sortSafeOutputMessages(messages) {
  if (!Array.isArray(messages) || messages.length === 0) {
    return messages;
  }

  // Build dependency graph
  const { dependencies, providers } = buildDependencyGraph(messages);

  if (typeof core !== "undefined") {
    const messagesWithDeps = Array.from(dependencies.entries()).filter(([_, deps]) => deps.size > 0);
    core.info(`Dependency analysis: ${providers.size} message(s) create temporary IDs, ` + `${messagesWithDeps.length} message(s) have dependencies`);
  }

  // Check for cycles
  const cycle = detectCycle(dependencies);
  if (cycle.length > 0) {
    if (typeof core !== "undefined") {
      const cycleMessages = cycle.map(i => {
        const msg = messages[i];
        const tempId = getCreatedTemporaryId(msg);
        return `${i} (${msg.type}${tempId ? `, creates ${tempId}` : ""})`;
      });
      core.warning(`Dependency cycle detected in safe output messages: ${cycleMessages.join(" -> ")}. ` + `Temporary IDs may not resolve correctly. Messages will be processed in original order.`);
    }
    // Return original order if there's a cycle
    return messages;
  }

  // Perform topological sort
  const sortedIndices = topologicalSort(messages, dependencies);

  // Reorder messages according to sorted indices
  const sortedMessages = sortedIndices.map(i => messages[i]);

  if (typeof core !== "undefined" && sortedIndices.length > 0) {
    // Check if order changed
    const orderChanged = sortedIndices.some((idx, i) => idx !== i);
    if (orderChanged) {
      core.info(`Topological sort reordered ${messages.length} message(s) to resolve temporary ID dependencies. ` + `New order: [${sortedIndices.join(", ")}]`);
    } else {
      core.info(`Topological sort: Messages already in optimal order (no reordering needed)`);
    }
  }

  return sortedMessages;
}

module.exports = {
  extractTemporaryIdReferences,
  getCreatedTemporaryId,
  buildDependencyGraph,
  detectCycle,
  topologicalSort,
  sortSafeOutputMessages,
};
