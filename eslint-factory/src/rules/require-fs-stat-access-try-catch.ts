import { ESLintUtils } from "@typescript-eslint/utils";
import { buildTryCatchSuggestion, createFsSyncMethodResolver, findEnclosingStatement, isInsideTryBlock } from "./try-catch-rule-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

// fs methods used in symlink-guard and path-validation code paths that throw on
// missing paths or permission errors, but aren't covered by require-fs-io-try-catch
// (statSync/readdirSync/copyFileSync/unlinkSync/renameSync) or
// require-realpathsync-try-catch (realpathSync).
const FS_STAT_ACCESS_METHODS = new Set(["lstatSync", "accessSync", "readlinkSync"]);

export const requireFsStatAccessTryCatchRule = createRule({
  name: "require-fs-stat-access-try-catch",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description:
        "Require fs.lstatSync, fs.accessSync, and fs.readlinkSync calls in actions/setup/js scripts to be wrapped in try/catch. " +
        "These methods throw synchronously on missing paths, broken symlinks, and permission errors — common in symlink-guard " +
        "and path-validation helpers — and without a call-site try/catch the entrypoint-level catch produces a generic " +
        "engine-level stack instead of a specific message that preserves the error as `{ cause }`.",
    },
    schema: [],
    messages: {
      requireTryCatch:
        "Wrap fs.{{method}}({{arg}}) in try/catch — synchronous fs methods throw on missing paths, broken symlinks, or " +
        "permission errors; without a call-site try/catch, you lose the original error context and get a generic " +
        "engine-level stack instead of a specific message with `{ cause }`.",
      wrapInTryCatch: "Wrap in try { ... } catch { ... } and re-throw with { cause: err } to preserve context.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    const resolveFsStatAccessMethod = createFsSyncMethodResolver(sourceCode, FS_STAT_ACCESS_METHODS, { allowUnboundFsIdentifier: true });

    return {
      CallExpression(node) {
        const methodName = resolveFsStatAccessMethod(node);

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
