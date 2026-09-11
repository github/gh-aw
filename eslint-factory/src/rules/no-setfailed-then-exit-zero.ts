import { AST_NODE_TYPES, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";
import { CORE_ALIASES } from "./core-aliases";
import { isCoreAliasIdentifier, isDestructuredCoreMethodIdentifier } from "./core-method-resolve";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

type SourceCode = Parameters<typeof isCoreAliasIdentifier>[1];

function isCoreLikeIdentifier(name: string): boolean {
  return CORE_ALIASES.has(name);
}

function isProcessMemberExpression(member: TSESTree.MemberExpression, propertyName: string): boolean {
  if (member.object.type !== AST_NODE_TYPES.Identifier || member.object.name !== "process") {
    return false;
  }
  if (!member.computed) {
    return member.property.type === AST_NODE_TYPES.Identifier && member.property.name === propertyName;
  }
  return member.property.type === AST_NODE_TYPES.Literal && member.property.value === propertyName;
}

/**
 * Returns true when `node` is an expression statement containing a call to
 * `core.setFailed(...)` in any recognized form:
 *  - Direct non-computed: core.setFailed(...)
 *  - Computed string literal: core["setFailed"](...)
 *  - Aliased object: const c = core; c.setFailed(...)
 *  - Destructured binding: const { setFailed } = core; setFailed(...)
 */
function isCoreSetFailedStatement(node: TSESTree.Statement, sourceCode: SourceCode): boolean {
  if (node.type !== AST_NODE_TYPES.ExpressionStatement) return false;
  const expr = node.expression;
  if (expr.type !== AST_NODE_TYPES.CallExpression) return false;
  const callee = expr.callee;

  if (callee.type === AST_NODE_TYPES.MemberExpression) {
    const obj = callee.object;
    const prop = callee.property;
    const isSetFailedNonComputed = !callee.computed && prop.type === AST_NODE_TYPES.Identifier && prop.name === "setFailed";
    const isSetFailedComputed = callee.computed && prop.type === AST_NODE_TYPES.Literal && prop.value === "setFailed";
    if (!isSetFailedNonComputed && !isSetFailedComputed) return false;
    if (obj.type !== AST_NODE_TYPES.Identifier) return false;
    return isCoreLikeIdentifier(obj.name) || isCoreAliasIdentifier(obj, sourceCode);
  }

  if (callee.type === AST_NODE_TYPES.Identifier) {
    return isDestructuredCoreMethodIdentifier(callee, "setFailed", sourceCode);
  }

  return false;
}

/**
 * Returns true when `node` is `process.exit(0)` or `process.exit()` (no args).
 * Both cause the process to exit with code 0, silently overriding any failure
 * previously declared via `core.setFailed()`.
 */
function isProcessExitZero(node: TSESTree.Statement): node is TSESTree.ExpressionStatement {
  if (node.type !== AST_NODE_TYPES.ExpressionStatement) return false;
  const expr = node.expression;
  if (expr.type !== AST_NODE_TYPES.CallExpression) return false;
  const callee = expr.callee;
  if (callee.type !== AST_NODE_TYPES.MemberExpression || !isProcessMemberExpression(callee, "exit")) {
    return false;
  }
  // process.exit() with no arguments defaults to exit code 0
  if (expr.arguments.length === 0) return true;
  // process.exit(0) — explicit zero literal
  if (expr.arguments.length === 1) {
    const arg = expr.arguments[0];
    return arg.type === AST_NODE_TYPES.Literal && arg.value === 0;
  }
  return false;
}

/**
 * Returns true when `node` is `process.exitCode = 0` (literal zero assignment).
 * Unlike `process.exit(0)`, this does not halt execution, but it still silently
 * resets the exit code to success, hiding a failure declared via `core.setFailed()`.
 */
function isProcessExitCodeZero(node: TSESTree.Statement): node is TSESTree.ExpressionStatement {
  if (node.type !== AST_NODE_TYPES.ExpressionStatement) return false;
  const expr = node.expression;
  if (expr.type !== AST_NODE_TYPES.AssignmentExpression || expr.operator !== "=") return false;
  const left = expr.left;
  if (left.type !== AST_NODE_TYPES.MemberExpression || !isProcessMemberExpression(left, "exitCode")) {
    return false;
  }
  const right = expr.right;
  return right.type === AST_NODE_TYPES.Literal && right.value === 0;
}

/**
 * Returns true when `node` is a control-transfer statement that definitively
 * exits the current block: return, throw, break, continue, or process.exit(...).
 */
function isControlTransferStatement(node: TSESTree.Statement): boolean {
  // prettier-ignore
  if (
    node.type === AST_NODE_TYPES.ReturnStatement ||
    node.type === AST_NODE_TYPES.ThrowStatement ||
    node.type === AST_NODE_TYPES.BreakStatement ||
    node.type === AST_NODE_TYPES.ContinueStatement
  ) {
    return true;
  }
  // process.exit(...) — any call, regardless of exit code
  if (node.type === AST_NODE_TYPES.ExpressionStatement && node.expression.type === AST_NODE_TYPES.CallExpression) {
    const callee = node.expression.callee;
    if (callee.type === AST_NODE_TYPES.MemberExpression && isProcessMemberExpression(callee, "exit")) {
      return true;
    }
  }
  return false;
}

function canContinueNormally(stmt: TSESTree.Statement): boolean {
  if (isControlTransferStatement(stmt)) return false;
  if (stmt.type === AST_NODE_TYPES.BlockStatement) {
    return canStatementsContinueNormally(stmt.body);
  }
  if (stmt.type === AST_NODE_TYPES.IfStatement) {
    if (!stmt.alternate) return true;
    return canContinueNormally(stmt.consequent) || canContinueNormally(stmt.alternate);
  }
  if (stmt.type === AST_NODE_TYPES.TryStatement) {
    // If a finally block never continues normally (e.g. it always returns/throws/exits),
    // the whole try statement can't continue normally either, regardless of try/catch.
    if (stmt.finalizer && !canStatementsContinueNormally(stmt.finalizer.body)) return false;
    const blockContinues = canStatementsContinueNormally(stmt.block.body);
    const handlerContinues = stmt.handler ? canStatementsContinueNormally(stmt.handler.body.body) : true;
    return blockContinues || handlerContinues;
  }
  return true;
}

function canStatementsContinueNormally(stmts: readonly TSESTree.Statement[]): boolean {
  for (const stmt of stmts) {
    if (!canContinueNormally(stmt)) return false;
  }
  return true;
}

function statementCanSetFailedAndContinue(stmt: TSESTree.Statement, sourceCode: SourceCode): boolean {
  if (isCoreSetFailedStatement(stmt, sourceCode)) return true;
  if (stmt.type === AST_NODE_TYPES.BlockStatement) {
    return statementsCanSetFailedAndContinue(stmt.body, sourceCode);
  }
  if (stmt.type === AST_NODE_TYPES.IfStatement) {
    return statementCanSetFailedAndContinue(stmt.consequent, sourceCode) || (stmt.alternate ? statementCanSetFailedAndContinue(stmt.alternate, sourceCode) : false);
  }
  if (stmt.type === AST_NODE_TYPES.TryStatement) {
    // A finalizer that itself sets failed-and-continues re-activates the path
    // regardless of what the try/catch arms did.
    if (stmt.finalizer && statementsCanSetFailedAndContinue(stmt.finalizer.body, sourceCode)) return true;
    // Otherwise the path is active if either the try block or the catch handler
    // sets failed and continues, AND the finalizer (if present) doesn't itself
    // definitively transfer control away (return/throw/exit) — a finalizer that
    // always exits/returns/throws overrides the try/catch outcome entirely.
    if (stmt.finalizer && !canStatementsContinueNormally(stmt.finalizer.body)) return false;
    const blockActivates = statementsCanSetFailedAndContinue(stmt.block.body, sourceCode);
    const handlerActivates = stmt.handler ? statementsCanSetFailedAndContinue(stmt.handler.body.body, sourceCode) : false;
    return blockActivates || handlerActivates;
  }
  return false;
}

function statementsCanSetFailedAndContinue(stmts: readonly TSESTree.Statement[], sourceCode: SourceCode): boolean {
  let activeSetFailedPath: boolean = false;
  for (const stmt of stmts) {
    const activeContinues: boolean = activeSetFailedPath && canContinueNormally(stmt);
    const activatesSetFailed: boolean = statementCanSetFailedAndContinue(stmt, sourceCode);
    activeSetFailedPath = activeContinues || activatesSetFailed;
  }
  return activeSetFailedPath;
}

export const noSetFailedThenExitZeroRule = createRule({
  name: "no-setfailed-then-exit-zero",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description:
        "Disallow `process.exit(0)` (or `process.exit()`) or `process.exitCode = 0` after `core.setFailed()` in GitHub Actions scripts. " +
        "`core.setFailed()` marks the step as failed by scheduling a non-zero exit code at process end. " +
        "Resetting the exit code to 0 afterward, whether by exiting immediately or by assigning `process.exitCode = 0`, " +
        "overrides that exit code to success, silently hiding the failure and causing the GitHub Actions step to appear " +
        "successful despite the declared error.",
    },
    schema: [],
    messages: {
      noSetFailedThenExitZero: "`process.exit(0)` after `core.setFailed()` silently resets the exit code to success, hiding the failure. " + "Replace `process.exit(0)` with `return;` to preserve the failure signal.",
      replaceWithReturn: "Replace `process.exit(0)` with `return;` to preserve the failure exit code.",
      noSetFailedThenExitCodeZero: "`process.exitCode = 0` after `core.setFailed()` silently resets the exit code to success, hiding the failure. " + "Remove the assignment (or set a non-zero value) to preserve the failure signal.",
      removeExitCodeZero: "Remove `process.exitCode = 0;` to preserve the failure exit code.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;

    function checkStatements(stmts: readonly TSESTree.Statement[]): void {
      for (let i = 0; i < stmts.length - 1; i++) {
        const current = stmts[i];
        const directSetFailed = isCoreSetFailedStatement(current, sourceCode);
        if (!directSetFailed && !statementCanSetFailedAndContinue(current, sourceCode)) continue;

        // Scan forward for process.exit(0), stopping at any other control-transfer statement.
        for (let j = i + 1; j < stmts.length; j++) {
          const candidate = stmts[j];

          if (isProcessExitZero(candidate)) {
            const isAdjacent = directSetFailed && j === i + 1;
            context.report({
              node: candidate,
              messageId: "noSetFailedThenExitZero",
              suggest: isAdjacent
                ? [
                    {
                      messageId: "replaceWithReturn",
                      fix(fixer: TSESLint.RuleFixer) {
                        return fixer.replaceText(candidate, "return;");
                      },
                    },
                  ]
                : [],
            });
            break;
          }

          if (isProcessExitCodeZero(candidate)) {
            context.report({
              node: candidate,
              messageId: "noSetFailedThenExitCodeZero",
              suggest: [
                {
                  messageId: "removeExitCodeZero",
                  fix(fixer: TSESLint.RuleFixer) {
                    return fixer.remove(candidate);
                  },
                },
              ],
            });
            // process.exitCode = 0 does not halt execution, so keep scanning for
            // further statements (e.g. a later process.exit(0) at the same level).
            continue;
          }

          // Stop scanning at any control-transfer (return, throw, break, process.exit(nonzero), etc.)
          if (isControlTransferStatement(candidate)) {
            break;
          }
        }
      }
    }

    // Handles `try { core.setFailed(...) } finally { process.exit(0); }` (and the
    // catch-block equivalent): the `setFailed` call and the exit-zero statement live
    // in different BlockStatement bodies (try/catch vs finally), so the sibling scan
    // in checkStatements never sees them side by side. Check the finalizer directly.
    function checkTryFinallyExitZero(node: TSESTree.TryStatement): void {
      if (!node.finalizer) return;
      const setFailedInTryOrCatch = statementsCanSetFailedAndContinue(node.block.body, sourceCode) || (node.handler ? statementsCanSetFailedAndContinue(node.handler.body.body, sourceCode) : false);
      if (!setFailedInTryOrCatch) return;

      for (const candidate of node.finalizer.body) {
        if (isProcessExitZero(candidate)) {
          context.report({ node: candidate, messageId: "noSetFailedThenExitZero", suggest: [] });
          break;
        }
        if (isProcessExitCodeZero(candidate)) {
          context.report({
            node: candidate,
            messageId: "noSetFailedThenExitCodeZero",
            suggest: [
              {
                messageId: "removeExitCodeZero",
                fix(fixer: TSESLint.RuleFixer) {
                  return fixer.remove(candidate);
                },
              },
            ],
          });
          continue;
        }
        if (isControlTransferStatement(candidate)) break;
      }
    }

    return {
      BlockStatement(node: TSESTree.BlockStatement) {
        checkStatements(node.body);
      },
      SwitchCase(node: TSESTree.SwitchCase) {
        checkStatements(node.consequent);
      },
      Program(node: TSESTree.Program) {
        checkStatements(node.body.filter((s): s is TSESTree.Statement => s.type !== AST_NODE_TYPES.ImportDeclaration && s.type !== AST_NODE_TYPES.ExportAllDeclaration));
      },
      TryStatement(node: TSESTree.TryStatement) {
        checkTryFinallyExitZero(node);
      },
    };
  },
});
