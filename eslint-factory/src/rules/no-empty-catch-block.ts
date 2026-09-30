import { AST_NODE_TYPES, ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

export const noEmptyCatchBlockRule = createRule({
  name: "no-empty-catch-block",
  meta: {
    type: "problem",
    docs: {
      description:
        "Disallow empty catch blocks in actions/setup/js scripts. Swallowing an error with no logging, fallback assignment, or explicit intentional-ignore comment hides real failures (corrupted state files, cleanup errors) that are hard to diagnose from CI logs.",
    },
    schema: [],
    messages: {
      noEmptyCatch: "Empty catch block silently swallows the error. Log it (e.g. core.debug/core.warning), assign a fallback value, or add an explicit comment explaining why the error is intentionally ignored.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    const intentionalIgnoreCommentRes = [/\bintentional\b/i, /\bbest[- ]effort\b/i, /\bnon[- ]fatal\b/i, /(?<![-\w])(?:safe to )?ignore(?:d|s)?\b/i, /\bfall[- ]through\b/i, /\bno[- ]?op\b/i];
    const negatedIntentionalIgnoreCommentRe = /\b(?:can't|cannot|do not|don't|must not|never|not|should not)\s+(?:an?\s+)?(?:(?:safe to\s+)?ignore|(?:silently\s+)?swallow|fall[- ]through|no[- ]?op|best[- ]effort|non[- ]fatal)\b/i;
    const swallowIntentionalIgnoreCommentRe = /\bsilently swallow(?:ed|s|ing)?\b|\bswallow(?:ed|s|ing)?\b(?=[^.!?]*(?:\bbecause\b|\bsince\b))/i;
    const consequenceNotAllowedCommentRe = /\b(?:must|should)\s+not\s+(?:fail|reject|surface|turn|cause|result|convert)\b/i;

    function commentSignalsIntentionalIgnore(comment: TSESTree.Comment): boolean {
      if (negatedIntentionalIgnoreCommentRe.test(comment.value)) return false;
      if (intentionalIgnoreCommentRes.some(re => re.test(comment.value))) return true;
      return swallowIntentionalIgnoreCommentRe.test(comment.value) || consequenceNotAllowedCommentRe.test(comment.value);
    }

    function hasAdjacentIntentionalIgnoreComment(node: TSESTree.Node): boolean {
      if (!node.loc) return false;
      return sourceCode.getCommentsBefore(node).some(comment => {
        if (!comment.loc || !commentSignalsIntentionalIgnore(comment)) return false;
        return node.loc.start.line - comment.loc.end.line <= 1;
      });
    }

    function hasIntentionalIgnoreComment(block: TSESTree.BlockStatement, node: TSESTree.Node): boolean {
      if (sourceCode.getCommentsInside(block).some(commentSignalsIntentionalIgnore)) return true;
      if (hasAdjacentIntentionalIgnoreComment(block)) return true;
      if (hasAdjacentIntentionalIgnoreComment(node)) return true;

      const ancestors = sourceCode.getAncestors(node);
      for (let i = ancestors.length - 1; i >= 0; i -= 1) {
        const ancestor = ancestors[i];
        if (ancestor.type === AST_NODE_TYPES.Program) break;
        if (ancestor.type.endsWith("Statement") && hasAdjacentIntentionalIgnoreComment(ancestor)) {
          return true;
        }
      }
      return false;
    }

    return {
      CatchClause(node: TSESTree.CatchClause) {
        const body = node.body.body;
        if (body.length !== 0) return;

        // An explicit intentional-ignore comment inside the otherwise empty
        // braces documents intent, e.g.:
        //   } catch { /* best-effort cleanup */ }
        if (hasIntentionalIgnoreComment(node.body, node)) return;

        context.report({
          node: node.body,
          messageId: "noEmptyCatch",
        });
      },
      "CallExpression[callee.type='MemberExpression'][callee.property.type='Identifier'][callee.property.name='catch']"(node: TSESTree.CallExpression) {
        const [handler] = node.arguments;
        if (!handler) return;
        if (handler.type !== AST_NODE_TYPES.ArrowFunctionExpression && handler.type !== AST_NODE_TYPES.FunctionExpression) return;
        if (handler.body.type !== AST_NODE_TYPES.BlockStatement) return;
        if (handler.body.body.length !== 0) return;
        if (hasIntentionalIgnoreComment(handler.body, handler)) return;

        context.report({
          node: handler.body,
          messageId: "noEmptyCatch",
        });
      },
    };
  },
});
