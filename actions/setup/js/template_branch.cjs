// @ts-check

// template_branch.cjs
// Shared branch-selection logic for {{#if / #elseif* / #else}} template conditionals.

const { isTruthy } = require("./is_truthy.cjs");

const ELSEIF_BRANCH_REGEX = /[ \t]*\{\{#?else[-_]?if\s+([^}]*)\}\}[ \t]*\n?/;

/**
 * Selects the appropriate branch from a conditional block that may contain
 * {{#elseif}}, {{#else-if}}, {{#else_if}}, {{elseif}}, {{else-if}}, {{else_if}}
 * branches in addition to the optional {{#else}} fallback.
 *
 * Algorithm:
 *   1. Split the body on elseif markers (all syntax variants) — capturing groups
 *      in the split pattern yield alternating [content, condition, content, ...].
 *   2. Pair each content piece with the condition that guards it:
 *        - First piece is guarded by the {{#if}} condition (ifCondition).
 *        - Subsequent pieces are guarded by the preceding elseif condition.
 *   3. Check the last piece for a {{#else}} tail and, if found, add an
 *      unconditional else branch.
 *   4. Return the content of the first truthy branch, or null if none matched.
 *
 * @param {string} ifCondition - The condition from the opening {{#if ...}} tag
 * @param {string} body        - Everything between the opening tag and {{/if}}
 * @param {(condition: string) => string} [resolveCondition] - Resolve opaque values after parsing author-owned branches
 * @returns {string|null}      - Content of the first truthy branch, or null
 */
function selectBranch(ifCondition, body, resolveCondition = condition => condition) {
  // Split on all elseif variants.  The capturing group ensures that the condition
  // text appears between the content pieces in the resulting array.
  // Supported: {{#elseif}}, {{#else-if}}, {{#else_if}}, {{elseif}}, {{else-if}}, {{else_if}}
  const parts = body.split(ELSEIF_BRANCH_REGEX);

  // parts alternates: [content0, cond1, content1, cond2, content2, ...]
  /** @type {Array<{condition: string|null, content: string}>} */
  const branches = [{ condition: ifCondition, content: parts[0] }];
  for (let i = 1; i < parts.length; i += 2) {
    branches.push({ condition: parts[i].trim(), content: parts[i + 1] || "" });
  }

  // Check whether the last branch's content contains a {{#else}} tail
  const lastBranch = branches[branches.length - 1];
  const elseParts = lastBranch.content.split(/[ \t]*\{\{#else\}\}[ \t]*\n?/);
  if (elseParts.length > 1) {
    lastBranch.content = elseParts[0];
    // condition: null = unconditional else branch
    branches.push({ condition: null, content: elseParts.slice(1).join("{{#else}}") });
  }

  // Return content of the first truthy branch
  for (const branch of branches) {
    if (branch.condition === null || isTruthy(resolveCondition(branch.condition))) {
      return branch.content;
    }
  }
  return null;
}

module.exports = { selectBranch };
