import { ESLintUtils } from "@typescript-eslint/utils";
import { buildTryCatchSuggestion, createFsSyncMethodResolver, findEnclosingStatement, isInsideTryBlock } from "./try-catch-rule-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

// fs.lstatSync and fs.readlinkSync are the two most commonly hand-rolled symlink-inspection
// primitives in actions/setup/js. Both throw synchronously on missing paths, permission
// errors, or broken symlinks, and both sit on security-sensitive code paths (symlink guards,
// audit-log cleanup, staged-attachment validation) where an unguarded throw skips the
// containment check entirely instead of failing with actionable context.
const FS_SYNC_METHODS = new Set(["lstatSync", "readlinkSync"]);

export const requireLstatSyncTryCatchRule = createRule({
  name: "require-lstatsync-try-catch",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description:
        "Require fs.lstatSync and fs.readlinkSync calls in actions/setup/js scripts to be wrapped in try/catch. " +
        "These methods throw synchronously on missing paths, permission errors, or broken symlinks; " +
        "without a call-site try/catch, symlink-traversal guards and audit-log cleanup abort with a generic engine-level stack instead of a specific message that preserves the error as `{ cause }`.",
    },
    schema: [],
    messages: {
      requireTryCatch:
        "Wrap fs.{{method}}({{arg}}) in try/catch — synchronous fs methods throw on I/O errors " +
        "(missing path, permission denied, broken symlink); without a call-site try/catch, you lose the original error context and get a generic engine-level stack instead of a specific message with `{ cause }`.",
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

        if (!methodName) return;

        if (isInsideTryBlock(sourceCode, node)) return;

        const argText = node.arguments.length > 0 ? sourceCode.getText(node.arguments[0]) : "";
        const method = methodName;
        const stmt = findEnclosingStatement(sourceCode, node);

        context.report({
          node,
          messageId: "requireTryCatch",
          data: { method, arg: argText },
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
                        todoComment: `TODO: handle I/O failure for this fs.${method} call.`,
                        errorPrefix: `fs.${method} failed: `,
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
