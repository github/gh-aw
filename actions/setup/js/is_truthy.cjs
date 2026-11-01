// @ts-check
/**
 * Determines if a value is truthy according to template logic.
 *
 * Supports:
 *   - Simple falsy string check: "", "false", "no", "0", "null", "undefined"
 *   - GitHub Actions script style equality with single- or double-quoted literals
 *     After experiment substitution the condition looks like: concise == "concise"
 *
 * @param {string} expr - The expression to evaluate
 * @returns {boolean} - Whether the expression is truthy
 */
function isTruthy(expr) {
  const trimmed = expr.trim();

  // Handle GitHub Actions script style equality expressions with either quote style.
  // Used by experiment conditionals after the experiment value has been substituted:
  //   {{#if experiments.prompt_style == "concise"}} becomes {{#if concise == "concise"}}
  // Note: (.*?) allows an empty LHS — if the experiment variable was not set the substituted
  // condition looks like ' == "concise"', which correctly returns false here rather than
  // falling through to the generic truthy check and incorrectly returning true.
  const comparison = trimmed.match(/^(.*?)\s*(===?|!==?)\s*(?:"([^"]*)"|'([^']*)')\s*$/s);
  if (comparison) {
    const equal = comparison[1].trim() === (comparison[3] ?? comparison[4]);
    return comparison[2].startsWith("!") ? !equal : equal;
  }

  const v = trimmed.toLowerCase();
  return !(v === "" || v === "false" || v === "no" || v === "0" || v === "null" || v === "undefined");
}

module.exports = { isTruthy };
