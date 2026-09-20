import { AST_NODE_TYPES, ESLintUtils, TSESTree } from "@typescript-eslint/utils";
import { buildTryCatchSuggestion, findEnclosingStatement, isInsideTryBlock } from "./try-catch-rule-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

export const requireLstatGuardTryCatchRule = createRule({
  name: "require-lstatguard-try-catch",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description:
        "Require lstatGuard(...) calls (from symlink_guard.cjs) in actions/setup/js scripts to be wrapped in try/catch. " +
        "lstatGuard delegates to fs.lstatSync, which throws synchronously when the target path does not exist, permissions are denied, " +
        "or another I/O error occurs — a case explicitly documented in symlink_guard.cjs's own JSDoc as the caller's responsibility. " +
        "Without a call-site try/catch, the entrypoint-level catch produces a generic engine-level stack instead of a specific message that preserves the error as `{ cause }`.",
    },
    schema: [],
    messages: {
      requireTryCatch:
        "Wrap lstatGuard({{arg}}) in try/catch — it delegates to fs.lstatSync, which throws on missing paths, permission errors, or other I/O failures " +
        "(this is documented in symlink_guard.cjs: callers that need to tolerate non-existent paths must wrap the call); without a call-site try/catch, " +
        "you lose the original error context and get a generic engine-level stack instead of a specific message with `{ cause }`.",
      wrapInTryCatch: "Wrap in try { ... } catch { ... } and re-throw with { cause: err } to preserve context.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    type SourceCodeScope = ReturnType<typeof sourceCode.getScope>;

    /** Returns true when `identifierName` resolves to a local binding (not a global). */
    function hasLocalBinding(node: TSESTree.Node, identifierName: string): boolean {
      let scope: SourceCodeScope | null = sourceCode.getScope(node);
      while (scope) {
        const variable = scope.set.get(identifierName);
        if (variable?.defs.length) {
          return true;
        }
        scope = scope.upper;
      }
      return false;
    }

    /**
     * Returns true when `callee` is bound to the `lstatGuard` export of `./symlink_guard.cjs`,
     * via either `const { lstatGuard } = require("./symlink_guard.cjs")` or
     * `import { lstatGuard } from "./symlink_guard.cjs"`.
     */
    function isLstatGuardImport(callee: TSESTree.Identifier): boolean {
      if (callee.name !== "lstatGuard") return false;

      let scope: SourceCodeScope | null = sourceCode.getScope(callee);
      while (scope) {
        const variable = scope.set.get("lstatGuard");
        if (variable && variable.defs.length > 0) {
          for (const def of variable.defs) {
            if (def.type === "ImportBinding") {
              const importDecl = def.parent;
              if (importDecl?.type === AST_NODE_TYPES.ImportDeclaration && importDecl.source.type === AST_NODE_TYPES.Literal && typeof importDecl.source.value === "string" && importDecl.source.value.includes("symlink_guard")) {
                return true;
              }
              continue;
            }
            if (def.type === "Variable") {
              const declarator = def.node as TSESTree.VariableDeclarator;
              if (declarator.id.type !== AST_NODE_TYPES.ObjectPattern) continue;
              const init = declarator.init;
              if (init?.type !== AST_NODE_TYPES.CallExpression) continue;
              if (init.callee.type !== AST_NODE_TYPES.Identifier || init.callee.name !== "require") continue;
              const requireArg = init.arguments[0];
              if (requireArg?.type !== AST_NODE_TYPES.Literal || typeof requireArg.value !== "string" || !requireArg.value.includes("symlink_guard")) continue;
              for (const prop of declarator.id.properties) {
                if (prop.type !== AST_NODE_TYPES.Property) continue;
                if (prop.key.type !== AST_NODE_TYPES.Identifier || prop.key.name !== "lstatGuard") continue;
                if (prop.value.type === AST_NODE_TYPES.Identifier && prop.value.name === callee.name) {
                  return true;
                }
              }
            }
          }
          return false;
        }
        scope = scope.upper;
      }
      return false;
    }

    return {
      CallExpression(node) {
        if (node.callee.type !== AST_NODE_TYPES.Identifier || node.callee.name !== "lstatGuard") return;
        if (!isLstatGuardImport(node.callee)) return;
        if (isInsideTryBlock(sourceCode, node)) return;

        const argText = node.arguments.length > 0 ? sourceCode.getText(node.arguments[0] as TSESTree.Node) : "";
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
                        todoComment: "TODO: handle filesystem failure for this lstatGuard(...) call.",
                        errorPrefix: "lstatGuard failed: ",
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
