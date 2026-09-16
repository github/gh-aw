import { AST_NODE_TYPES, ESLintUtils, TSESTree } from "@typescript-eslint/utils";
import { buildTryCatchSuggestion, findEnclosingStatement, isInsideTryBlock } from "./try-catch-rule-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

/**
 * Returns true when `arg` is a compile-time-fixed regex source: a string
 * literal, a template literal with no interpolated expressions, or a `.source`
 * property read off a RegExp literal/identifier (a common "clone this fixed
 * regex" idiom, e.g. `new RegExp(FOO_RE.source, "g")`).
 *
 * Any of these forms can only throw when the *literal* text itself is an
 * invalid pattern — a bug that is caught immediately by tests, not a runtime
 * risk from untrusted input. Fully dynamic sources built by joining an
 * arbitrary array (e.g. `patterns.map(p => p.source).join("|")`) are still
 * considered dynamic on purpose: the fixed-ness of each element does not make
 * the combined pattern statically safe against all runtime inputs.
 */
function isStaticPatternSource(arg: TSESTree.CallExpressionArgument): boolean {
  if (arg.type === AST_NODE_TYPES.Literal && (typeof arg.value === "string" || "regex" in arg)) return true;
  if (arg.type === AST_NODE_TYPES.TemplateLiteral && arg.expressions.length === 0) return true;
  if (arg.type === AST_NODE_TYPES.MemberExpression && !arg.computed && arg.property.type === AST_NODE_TYPES.Identifier && arg.property.name === "source") {
    return true;
  }
  return false;
}

/**
 * Returns true when `arg` is a bare identifier or a (non-`.source`) property
 * access — i.e. an opaque runtime value whose textual content is not visible
 * at the call site and therefore cannot be reviewed for regex validity ahead
 * of time. This is the shape the rule targets: `new RegExp(validation.pattern)`,
 * `new RegExp(userPattern)`.
 */
function isOpaqueDynamicSource(arg: TSESTree.CallExpressionArgument): boolean {
  if (arg.type === AST_NODE_TYPES.Identifier) return true;
  if (arg.type === AST_NODE_TYPES.MemberExpression) return !isStaticPatternSource(arg);
  return false;
}

export const requireNewRegExpTryCatchRule = createRule({
  name: "require-new-regexp-try-catch",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description:
        "Require new RegExp(variable) calls in actions/setup/js scripts to be wrapped in try/catch when the pattern source is an opaque " +
        "identifier or property access (e.g. config-driven or user-supplied). The RegExp constructor throws a SyntaxError for an invalid " +
        "pattern; without a call-site try/catch, an attacker- or config-controlled pattern can crash the script with a generic, hard-to-diagnose stack trace.",
    },
    schema: [],
    messages: {
      requireTryCatch:
        "Wrap new RegExp({{arg}}) in try/catch — the RegExp constructor throws a SyntaxError for an invalid pattern, and {{arg}} is an opaque " +
        "runtime value whose content cannot be validated ahead of time. Without a call-site try/catch, you lose the original error context " +
        "and get a generic engine-level stack instead of a specific message with `{ cause }`.",
      wrapInTryCatch: "Wrap in try { ... } catch { ... } and re-throw with { cause: err } to preserve context.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    type SourceCodeScope = ReturnType<typeof sourceCode.getScope>;

    /** Returns true when name is bound by a local definition, meaning it shadows the global. */
    function hasLocalBinding(node: TSESTree.Node, name: string): boolean {
      let scope: SourceCodeScope | null = sourceCode.getScope(node);
      while (scope) {
        const variable = scope.set.get(name);
        if (variable?.defs.length) {
          return true;
        }
        scope = scope.upper;
      }
      return false;
    }

    return {
      NewExpression(node) {
        // Only flag `new RegExp(...)` — the global RegExp constructor.
        if (node.callee.type !== AST_NODE_TYPES.Identifier || node.callee.name !== "RegExp") return;
        // Skip when RegExp is shadowed by a local binding (e.g. a parameter or import named RegExp).
        if (hasLocalBinding(node, "RegExp")) return;

        const firstArg = node.arguments[0];
        if (firstArg === undefined || firstArg.type === AST_NODE_TYPES.SpreadElement) return;
        if (!isOpaqueDynamicSource(firstArg)) return;

        if (isInsideTryBlock(sourceCode, node)) return;

        const argText = sourceCode.getText(firstArg);
        const stmt = findEnclosingStatement(sourceCode, node);

        context.report({
          node,
          messageId: "requireTryCatch",
          data: { arg: argText },
          suggest: stmt
            ? [
                {
                  messageId: "wrapInTryCatch",
                  fix(fixer) {
                    const stmtText = sourceCode.getText(stmt);
                    const startLine = stmt.loc?.start.line;
                    const stmtLine = startLine !== undefined ? (sourceCode.lines[startLine - 1] ?? "") : "";
                    const indent = stmtLine.match(/^(\s*)/)?.[1] ?? "";
                    return fixer.replaceText(
                      stmt,
                      buildTryCatchSuggestion(stmtText, {
                        indent,
                        todoComment: "TODO: handle invalid pattern for this new RegExp(...) call.",
                        errorPrefix: "RegExp constructor call failed: ",
                      })
                    );
                  },
                },
              ]
            : [],
        });
      },
    };
  },
});
