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

const { extractTemporaryIdReferences, getCreatedTemporaryId } = require("./temporary_id.cjs");

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
 * Detect cycles in the dependency graph using iterative mark-and-sweep algorithm
 * Returns an array of message indices that form a cycle, or empty array if no cycle
 *
 * @param {Map<number, Set<number>>} dependencies - Dependency graph
 * @returns {Array<number>} Indices of messages forming a cycle, or empty array
 */
function detectCycle(dependencies) {
  const WHITE = 0; // Not visited
  const GRAY = 1; // Visiting (on stack)
  const BLACK = 2; // Visited (completed)

  const colors = new Map();
  const parent = new Map();

  // Initialize all nodes as WHITE
  for (const node of dependencies.keys()) {
    colors.set(node, WHITE);
    parent.set(node, null);
  }

  // Try to find cycle starting from each WHITE node
  for (const startNode of dependencies.keys()) {
    if (colors.get(startNode) !== WHITE) {
      continue;
    }

    // Use a stack for iterative DFS
    // Each stack entry: [node, iterator, isReturning]
    /** @type {Array<[number, any, boolean]>} */
    const stack = [[startNode, null, false]];

    while (stack.length > 0) {
      const entry = stack.pop();
      if (!entry) continue;

      const [node, depsIterator, isReturning] = entry;

      if (isReturning) {
        // Returning from exploring this node - mark as BLACK
        colors.set(node, BLACK);
        continue;
      }

      // Mark node as GRAY (being explored)
      colors.set(node, GRAY);

      // Push node again to mark BLACK when we return
      stack.push([node, null, true]);

      // Get dependencies for this node
      const deps = dependencies.get(node) || new Set();

      // Process each dependency
      for (const dep of deps) {
        const depColor = colors.get(dep);

        if (depColor === WHITE) {
          // Not visited yet - explore it
          parent.set(dep, node);
          stack.push([dep, null, false]);
        } else if (depColor === GRAY) {
          // Found a back edge - cycle detected!
          // Reconstruct the cycle from dep to current node
          const cycle = [dep];
          let current = node;
          while (current !== dep && current !== null) {
            cycle.unshift(current);
            current = parent.get(current);
          }
          return cycle;
        }
        // If BLACK, it's already fully explored - no cycle through this path
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
  buildDependencyGraph,
  detectCycle,
  topologicalSort,
  sortSafeOutputMessages,
};
