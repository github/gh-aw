import { ESLintUtils } from "@typescript-eslint/utils";
import { buildTryCatchSuggestion, createFsSyncMethodResolver, findEnclosingStatement, isInsideTryBlock } from "./try-catch-rule-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const FS_SYNC_METHODS = new Set(["realpathSync"]);

export const requireRealpathSyncTryCatchRule = createRule({
  name: "require-realpathsync-try-catch",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description:
        "Require fs.realpathSync calls in actions/setup/js scripts to be wrapped in try/catch. " +
        "realpathSync throws synchronously when the target path is missing, permissions are denied, or a symlink cycle is encountered; " +
        "without a call-site try/catch, the containment check is skipped and the original error loses useful call-site context.",
    },
    schema: [],
    messages: {
      requireTryCatch:
        "Wrap fs.realpathSync({{arg}}) in try/catch — realpathSync throws on missing paths, permission denied, or symlink cycles; without a call-site try/catch, you lose the original error context and get a generic engine-level stack instead of a specific message with `{ cause }`.",
      wrapInTryCatch: "Wrap in try { ... } catch { ... } and re-throw with { cause: err } to preserve context.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    const resolveFsSyncMethod = createFsSyncMethodResolver(sourceCode, FS_SYNC_METHODS, { allowUnboundFsIdentifier: true });

    return {
      CallExpression(node) {
        const methodName = resolveFsSyncMethod(node);
        if (methodName !== "realpathSync") return;
        if (isInsideTryBlock(sourceCode, node)) return;

        const argText = node.arguments.length > 0 ? sourceCode.getText(node.arguments[0]) : "";
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
                        todoComment: "TODO: handle filesystem failure for this fs.realpathSync call.",
                        errorPrefix: "fs.realpathSync failed: ",
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
