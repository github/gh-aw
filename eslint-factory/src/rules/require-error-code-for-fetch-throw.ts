import { AST_NODE_TYPES, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";
import { resolveWriteOnceInitializerChain } from "./command-initializer-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const ERROR_CODE_PATTERN = /(^|[^A-Za-z0-9])(E[0-9]{3}|ERR_[A-Z0-9_]+|SAFE_OUTPUT_E[0-9]{3})\b/;
const FUNCTION_BOUNDARY_TYPES = new Set([AST_NODE_TYPES.FunctionDeclaration, AST_NODE_TYPES.FunctionExpression, AST_NODE_TYPES.ArrowFunctionExpression]);

function messageReferencesErrorCode(node: TSESTree.Expression, sourceCode: Readonly<TSESLint.SourceCode>): boolean {
  const candidate = resolveWriteOnceInitializerChain(node, sourceCode);
  if (candidate.type === AST_NODE_TYPES.Literal && typeof candidate.value === "string") return ERROR_CODE_PATTERN.test(candidate.value);
  if (candidate.type === AST_NODE_TYPES.Identifier) return ERROR_CODE_PATTERN.test(candidate.name);
  if (candidate.type === AST_NODE_TYPES.TemplateLiteral) {
    if (candidate.quasis.some(quasi => ERROR_CODE_PATTERN.test(quasi.value.raw))) return true;
    return candidate.expressions.some(expression => messageReferencesErrorCode(expression as TSESTree.Expression, sourceCode));
  }
  if (candidate.type === AST_NODE_TYPES.MemberExpression) {
    if (!candidate.computed && candidate.property.type === AST_NODE_TYPES.Identifier) return ERROR_CODE_PATTERN.test(candidate.property.name);
    return false;
  }
  if (candidate.type === AST_NODE_TYPES.BinaryExpression && candidate.operator === "+") {
    return messageReferencesErrorCode(candidate.left, sourceCode) || messageReferencesErrorCode(candidate.right, sourceCode);
  }
  return false;
}

type FunctionNode = TSESTree.FunctionDeclaration | TSESTree.FunctionExpression | TSESTree.ArrowFunctionExpression;

function getImmediateEnclosingFunction(node: TSESTree.Node, sourceCode: Readonly<TSESLint.SourceCode>): FunctionNode | null {
  const ancestors = sourceCode.getAncestors(node);
  for (let i = ancestors.length - 1; i >= 0; i--) {
    const ancestor = ancestors[i];
    if (FUNCTION_BOUNDARY_TYPES.has(ancestor.type)) return ancestor as FunctionNode;
  }
  return null;
}

// Only a bare `fetch(...)` / `await fetch(...)` call counts: this targets
// direct external HTTP calls (Azure DevOps, Google OTLP, artifact upload
// endpoints, etc.), not GitHub's octokit client, which already has its own
// dedicated rule (require-error-code-for-github-api-throw).
function isBareFetchCall(node: TSESTree.CallExpression): boolean {
  return node.callee.type === AST_NODE_TYPES.Identifier && node.callee.name === "fetch";
}

export const requireErrorCodeForFetchThrowRule = createRule({
  name: "require-error-code-for-fetch-throw",
  meta: {
    type: "suggestion",
    docs: {
      description: "Require throw new Error(...) after a bare fetch() call in the same function to include a standardized error code, even in files that have not yet adopted error_codes.cjs.",
    },
    schema: [],
    messages: {
      missingErrorCode: "This throw follows a fetch() call. Prefix the Error message with a standardized code (for example E007, ERR_*, or SAFE_OUTPUT_E007) so failures from this external request can be filtered consistently in logs.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    const fetchCallsByFunction = new Map<FunctionNode, number[]>();

    return {
      CallExpression(node) {
        if (!isBareFetchCall(node)) return;
        const fn = getImmediateEnclosingFunction(node, sourceCode);
        if (!fn) return;
        const calls = fetchCallsByFunction.get(fn);
        if (calls) calls.push(node.range[0]);
        else fetchCallsByFunction.set(fn, [node.range[0]]);
      },
      ThrowStatement(node) {
        if (!node.argument || node.argument.type !== AST_NODE_TYPES.NewExpression) return;
        const thrown = node.argument;
        if (thrown.callee.type !== AST_NODE_TYPES.Identifier || thrown.callee.name !== "Error") return;
        const firstArg = thrown.arguments[0];
        if (!firstArg || firstArg.type === AST_NODE_TYPES.SpreadElement) return;
        if (messageReferencesErrorCode(firstArg as TSESTree.Expression, sourceCode)) return;

        const fn = getImmediateEnclosingFunction(node, sourceCode);
        if (!fn) return;
        const callStarts = fetchCallsByFunction.get(fn);
        if (!callStarts || !callStarts.some(callStart => callStart < node.range[0])) return;

        context.report({
          node: thrown,
          messageId: "missingErrorCode",
        });
      },
    };
  },
});
