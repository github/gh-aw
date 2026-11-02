// @ts-check

/**
 * Bind author expressions only through compiler-selected environment variables.
 * @param {string} content
 * @param {Record<string, string>} bindings
 * @param {NodeJS.ProcessEnv} env
 * @param {(value: string) => string} protectValue
 * @returns {string}
 */
function bindAuthorExpressions(content, bindings, env, protectValue) {
  content = content.replace(/\{\{#?(if|else[-_]?if)\s+([^{}]+)\}\}/g, (match, tag, expression) => {
    const trimmed = expression.trim();
    return Object.prototype.hasOwnProperty.call(bindings, trimmed) ? `{{#${tag === "if" ? "if" : "elseif"} \${{ ${trimmed} }} }}` : match;
  });
  return content.replace(/\$\{\{([\s\S]*?)\}\}/g, (match, expression) => {
    const key = expression.trim();
    if (!Object.prototype.hasOwnProperty.call(bindings, key)) return match;
    const binding = bindings[key];
    if (/^__GH_AW_[A-Z0-9_]+__$/.test(binding)) return binding;
    return Object.prototype.hasOwnProperty.call(env, binding) ? protectValue(env[binding] || "") : `__${binding}__`;
  });
}

module.exports = { bindAuthorExpressions };
