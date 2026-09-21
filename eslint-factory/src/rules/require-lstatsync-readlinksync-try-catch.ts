import { ESLintUtils } from "@typescript-eslint/utils";
import { buildTryCatchSuggestion, createFsSyncMethodResolver, findEnclosingStatement, isInsideTryBlock } from "./try-catch-rule-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

// fs.lstatSync/fs.readlinkSync are used across actions/setup/js to inspect symlinks in
// security-sensitive paths (symlink-traversal guards, staged-attachment validation, memory
// digesting). Both throw synchronously on missing paths, permission errors, or (readlinkSync)
// a non-symlink target, so unguarded calls surface a generic engine-level stack instead of a
// specific message that preserves the error as `{ cause }`.
const FS_SYNC_METHODS = new Set(["lstatSync", "readlinkSync"]);

export const requireLstatSyncReadlinkSyncTryCatchRule = createRule({
  name: "require-lstatsync-readlinksync-try-catch",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description:
        "Require fs.lstatSync and fs.readlinkSync calls in actions/setup/js scripts to be wrapped in try/catch. " +
        "Both methods throw synchronously on missing paths, permission errors, or (readlinkSync) a non-symlink target; " +
        "without a call-site try/catch, the entrypoint-level catch produces a generic engine-level stack instead of a specific message that preserves the error as `{ cause }`.",
    },
    schema: [],
    messages: {
      requireTryCatch:
        "Wrap fs.{{method}}({{arg}}) in try/catch — synchronous fs methods throw on I/O errors " +
        "(missing path, permission denied, non-symlink target); without a call-site try/catch, you lose the original error context and get a generic engine-level stack instead of a specific message with `{ cause }`.",
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
                        todoComment: `TODO: handle filesystem failure for this fs.${method} call.`,
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
