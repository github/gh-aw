import { ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

export const noSliceToUncheckedIndexOfRule = createRule({
  name: "no-slice-to-unchecked-indexof",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow slicing a string or array up to an unchecked indexOf result on the same value, because -1 silently drops the last element.",
    },
    schema: [],
    messages: {
      uncheckedIndexOf: "`{{object}}.slice(…, {{object}}.{{method}}(…))` drops the last item when nothing is found (index -1). Store the index, check for -1, then slice.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;

    function memberName(callee: TSESTree.Node): string | null {
      if (callee.type !== "MemberExpression" || callee.computed || callee.property.type !== "Identifier") {
        return null;
      }
      return callee.property.name;
    }

    return {
      CallExpression(node) {
        if (memberName(node.callee) !== "slice" || node.arguments.length !== 2) {
          return;
        }
        const callee = node.callee as TSESTree.MemberExpression;
        const end = node.arguments[1];
        if (end.type !== "CallExpression") {
          return;
        }
        const method = memberName(end.callee);
        if (method !== "indexOf" && method !== "lastIndexOf") {
          return;
        }
        const searched = (end.callee as TSESTree.MemberExpression).object;
        // Only same-value comparisons of side-effect-free expressions are flagged.
        const isSimple = (n: TSESTree.Node): boolean => n.type === "Identifier" || (n.type === "MemberExpression" && !n.computed && isSimple(n.object)) || n.type === "ThisExpression";
        if (!isSimple(callee.object) || sourceCode.getText(callee.object) !== sourceCode.getText(searched)) {
          return;
        }
        context.report({ node: end, messageId: "uncheckedIndexOf", data: { object: sourceCode.getText(callee.object), method } });
      },
    };
  },
});
