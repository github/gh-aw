// @ts-check
/**
 * Fuzz harness for {{#if / #elseif / #else}} template branch selection and rendering.
 *
 * Reads a JSON test case from stdin:
 *   { ifCondition: string, body: string }   — for selectBranch
 *   { markdown: string }                     — for renderMarkdownTemplate
 *
 * Writes the result as JSON to stdout:
 *   { result: string|null, error: string|null }
 *
 * Used by the Go fuzz driver in template_conditional_js_fuzz_test.go.
 */
// @safe-outputs-exempt SEC-004 -- test infrastructure fuzz harness; parsed.body is never written to a GitHub API

const { selectBranch } = require("./template_branch.cjs");
const { isTruthy } = require("./is_truthy.cjs");

// Minimal shim so renderMarkdownTemplate can call core.info
if (!global.core) {
  require("./shim.cjs");
  Object.assign(global.core, {
    info: () => {},
    warning: () => {},
    setFailed: () => {},
  });
}

const { renderMarkdownTemplate } = require("./render_template.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

if (require.main === module) {
  let input = "";
  process.stdin.on("data", chunk => {
    input += chunk;
  });
  process.stdin.on("end", () => {
    try {
      const parsed = JSON.parse(input);

      let result;
      if (Object.prototype.hasOwnProperty.call(parsed, "ifCondition")) {
        // selectBranch test
        result = { result: selectBranch(parsed.ifCondition, parsed.body || ""), error: null };
      } else if (Object.prototype.hasOwnProperty.call(parsed, "markdown")) {
        // renderMarkdownTemplate test
        result = { result: renderMarkdownTemplate(parsed.markdown || ""), error: null };
      } else {
        result = { result: null, error: "Unknown test type: expected 'ifCondition' or 'markdown' key" };
      }

      process.stdout.write(JSON.stringify(result));
      process.exit(0);
    } catch (err) {
      process.stdout.write(JSON.stringify({ result: null, error: getErrorMessage(err) }));
      process.exit(1);
    }
  });
}

module.exports = { selectBranch, renderMarkdownTemplate };
