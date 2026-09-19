import { AST_NODE_TYPES, ESLintUtils, TSESTree } from "@typescript-eslint/utils";
import { buildTryCatchSuggestion, findEnclosingStatement, isInsideTryBlock } from "./try-catch-rule-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

export const requireDynamicRegexpConstructorTryCatchRule = createRule({
  name: "require-dynamic-regexp-constructor-try-catch",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description:
        "Require `new RegExp(pattern)` calls in actions/setup/js scripts to be wrapped in try/catch when `pattern` is passed through " +
        "wholesale as a bare identifier or property access (e.g. `validation.pattern`), rather than built locally from a template " +
        "literal or string literal. The RegExp constructor throws a SyntaxError when given a malformed pattern (unbalanced brackets, " +
        "invalid escape sequences, or invalid backreferences); when the entire pattern originates from external configuration " +
        "(a workflow-provided validation rule, an environment variable, or user-controlled data) with no local escaping or " +
        "construction, an unguarded `new RegExp(...)` call turns a single bad input into an unhandled crash. Without a call-site " +
        "try/catch, the entrypoint-level catch produces a generic engine-level stack instead of a specific message that preserves " +
        "the error as `{ cause }`. Patterns built locally from template/string literals (even with interpolated variables) are out " +
        "of scope — those are covered by require-escaped-regexp-interpolation instead.",
    },
    schema: [],
    messages: {
      requireTryCatch:
        "Wrap new RegExp({{arg}}) in try/catch — a dynamic pattern can be malformed and throws SyntaxError at construction time; " +
        "without a call-site try/catch, you lose the original error context and get a generic engine-level stack instead of a " +
        "specific message with `{ cause }`.",
      wrapInTryCatch: "Wrap in try { ... } catch { ... } and re-throw with { cause: err } to preserve context.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    type SourceCodeScope = ReturnType<typeof sourceCode.getScope>;

    /** Returns true when `name` is bound by a local definition, meaning it shadows the global RegExp constructor. */
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

    /** Returns true when an expression coerces to a compile-time constant string (never throws in RegExp position). */
    function isStaticStringExpression(arg: TSESTree.Node): boolean {
      if (arg.type === AST_NODE_TYPES.Literal && typeof arg.value === "string") return true;
      if (arg.type === AST_NODE_TYPES.TemplateLiteral && arg.expressions.length === 0) return true;
      if (arg.type === AST_NODE_TYPES.BinaryExpression && arg.operator === "+") {
        return isStaticStringExpression(arg.left) && isStaticStringExpression(arg.right);
      }
      return false;
    }

    /**
     * Returns true when `expr` is a bare identifier or a non-computed property-access chain rooted in an
     * identifier (e.g. `pattern`, `validation.pattern`, `config.rules.pattern`), excluding chains that end
     * in `.source` (e.g. `FOO_RE.source`) — a `.source` access reads the already-validated pattern string of
     * an existing regular expression, so it can never introduce a new malformed pattern.
     * These are the only shapes flagged: passing an entire external value through wholesale, with no local
     * template/string construction, is the pattern most likely to carry unsanitized config or user data
     * straight into the constructor.
     */
    function isWholesaleExternalPattern(expr: TSESTree.Node): boolean {
      if (expr.type === AST_NODE_TYPES.Identifier) return true;

      if (expr.type === AST_NODE_TYPES.MemberExpression && !expr.computed && expr.property.type === AST_NODE_TYPES.Identifier) {
        if (expr.property.name === "source") return false;
        return isWholesaleExternalPattern(expr.object) || expr.object.type === AST_NODE_TYPES.ThisExpression;
      }

      return false;
    }

    /** Returns true when an argument is a runtime-dynamic pattern expression that is not already regex-validated. */
    function isUnvalidatedDynamicArg(arg: TSESTree.CallExpressionArgument): boolean {
      if (arg.type === "SpreadElement") return false;
      if (isStaticStringExpression(arg)) return false;
      return isWholesaleExternalPattern(arg);
    }

    return {
      NewExpression(node) {
        if (node.callee.type !== AST_NODE_TYPES.Identifier || node.callee.name !== "RegExp") return;
        if (hasLocalBinding(node, "RegExp")) return;

        const firstArg = node.arguments[0];
        if (!firstArg || !isUnvalidatedDynamicArg(firstArg)) return;

        if (isInsideTryBlock(sourceCode, node)) return;

        const argText = sourceCode.getText(firstArg as TSESTree.Node);
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
