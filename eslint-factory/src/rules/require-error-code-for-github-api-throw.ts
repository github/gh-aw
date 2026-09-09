import { AST_NODE_TYPES, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";
import { resolveWriteOnceInitializerChain } from "./command-initializer-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const ERROR_CODE_PATTERN = /(^|[^A-Za-z0-9])(E[0-9]{3}|ERR_[A-Z0-9_]+|SAFE_OUTPUT_E[0-9]{3})\b/;
const FUNCTION_BOUNDARY_TYPES = new Set([AST_NODE_TYPES.FunctionDeclaration, AST_NODE_TYPES.FunctionExpression, AST_NODE_TYPES.ArrowFunctionExpression]);

function getStaticPropertyName(node: TSESTree.MemberExpression): string | null {
  if (!node.computed && node.property.type === AST_NODE_TYPES.Identifier) return node.property.name;
  if (node.computed && node.property.type === AST_NODE_TYPES.Literal && typeof node.property.value === "string") return node.property.value;
  return null;
}

function memberChainHasProperty(node: TSESTree.MemberExpression, name: string): boolean {
  let current: TSESTree.MemberExpression | TSESTree.Expression | TSESTree.Super = node;
  while (current.type === AST_NODE_TYPES.MemberExpression) {
    if (getStaticPropertyName(current) === name) return true;
    current = current.object;
  }
  return false;
}

function objectLooksLikeGitHubClient(node: TSESTree.Expression | TSESTree.Super): boolean {
  if (node.type === AST_NODE_TYPES.Identifier) return /^(github|octokit)/i.test(node.name);
  if (node.type !== AST_NODE_TYPES.MemberExpression) return false;
  const prop = getStaticPropertyName(node);
  if (prop === "github" || prop === "octokit") return true;
  return objectLooksLikeGitHubClient(node.object);
}

function hasErrorCodesRequire(program: TSESTree.Program): boolean {
  return program.body.some(statement => {
    if (statement.type !== AST_NODE_TYPES.VariableDeclaration) return false;
    return statement.declarations.some(declaration => {
      if (!declaration.init || declaration.init.type !== AST_NODE_TYPES.CallExpression) return false;
      if (declaration.init.callee.type !== AST_NODE_TYPES.Identifier || declaration.init.callee.name !== "require") return false;
      const firstArg = declaration.init.arguments[0];
      return firstArg?.type === AST_NODE_TYPES.Literal && firstArg.value === "./error_codes.cjs";
    });
  });
}

function isGitHubApiCall(node: TSESTree.CallExpression): boolean {
  if (node.callee.type !== AST_NODE_TYPES.MemberExpression) return false;
  const propertyName = getStaticPropertyName(node.callee);
  if (!propertyName) return false;
  if (propertyName === "graphql" || propertyName === "paginate") return objectLooksLikeGitHubClient(node.callee.object);
  return memberChainHasProperty(node.callee, "rest");
}

function messageReferencesErrorCode(node: TSESTree.Expression, sourceCode: Readonly<TSESLint.SourceCode>): boolean {
  const candidate = resolveWriteOnceInitializerChain(node, sourceCode);
  if (candidate.type === AST_NODE_TYPES.Literal && typeof candidate.value === "string") return ERROR_CODE_PATTERN.test(candidate.value);
  if (candidate.type === AST_NODE_TYPES.Identifier) return ERROR_CODE_PATTERN.test(candidate.name);
  if (candidate.type === AST_NODE_TYPES.TemplateLiteral) {
    if (candidate.quasis.some(quasi => ERROR_CODE_PATTERN.test(quasi.value.raw))) return true;
    return candidate.expressions.some(expression => messageReferencesErrorCode(expression as TSESTree.Expression, sourceCode));
  }
  if (candidate.type === AST_NODE_TYPES.MemberExpression) {
    const propertyName = getStaticPropertyName(candidate);
    return propertyName !== null && ERROR_CODE_PATTERN.test(propertyName);
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

// Returns the call that `fn` is passed to as an argument, but only when that
// call is itself awaited directly (for example `await withRetry(() => ..., ...)`).
// This excludes fire-and-forget callbacks like `setTimeout(() => ..., 0)`,
// whose bodies run outside the dynamic scope of any enclosing try/catch.
function getAwaitedCallbackWrapperCall(fn: FunctionNode): TSESTree.CallExpression | null {
  const parent = fn.parent;
  if (!parent || parent.type !== AST_NODE_TYPES.CallExpression || !parent.arguments.some(argument => argument === fn)) return null;
  return parent.parent?.type === AST_NODE_TYPES.AwaitExpression ? parent : null;
}

// Checks whether a statement (possibly nested in control-flow constructs)
// contains a throw, without crossing into nested function bodies.
function statementRethrows(statement: TSESTree.Statement): boolean {
  switch (statement.type) {
    case AST_NODE_TYPES.ThrowStatement:
      return true;
    case AST_NODE_TYPES.BlockStatement:
      return statement.body.some(statementRethrows);
    case AST_NODE_TYPES.IfStatement:
      return statementRethrows(statement.consequent) || (statement.alternate !== null && statementRethrows(statement.alternate));
    case AST_NODE_TYPES.TryStatement:
      return statementRethrows(statement.block) || (statement.handler !== null && statementRethrows(statement.handler.body)) || (statement.finalizer !== null && statementRethrows(statement.finalizer));
    case AST_NODE_TYPES.SwitchStatement:
      return statement.cases.some(switchCase => switchCase.consequent.some(statementRethrows));
    case AST_NODE_TYPES.ForStatement:
    case AST_NODE_TYPES.ForInStatement:
    case AST_NODE_TYPES.ForOfStatement:
    case AST_NODE_TYPES.WhileStatement:
    case AST_NODE_TYPES.DoWhileStatement:
    case AST_NODE_TYPES.LabeledStatement:
      return statementRethrows(statement.body);
    default:
      return false;
  }
}

function catchClauseRethrows(handler: TSESTree.CatchClause): boolean {
  return statementRethrows(handler.body);
}

function getFunctionsForGitHubApiCall(node: TSESTree.CallExpression, sourceCode: Readonly<TSESLint.SourceCode>): FunctionNode[] {
  const immediateFunction = getImmediateEnclosingFunction(node, sourceCode);
  if (!immediateFunction) return [];
  const functions = [immediateFunction];

  // Only correlate across the callback boundary for the awaited retry-helper
  // shape; otherwise unrelated deferred callbacks (setTimeout, etc.) would be
  // incorrectly attributed to whatever function happens to enclose a try.
  const wrapperCall = getAwaitedCallbackWrapperCall(immediateFunction);
  if (!wrapperCall) return functions;

  const ancestors = sourceCode.getAncestors(wrapperCall);
  for (let i = ancestors.length - 1; i >= 0; i--) {
    const ancestor = ancestors[i];
    if (ancestor.type !== AST_NODE_TYPES.TryStatement || !ancestor.handler) continue;
    if (!(ancestor.block.range[0] <= wrapperCall.range[0] && wrapperCall.range[1] <= ancestor.block.range[1])) continue;
    // A catch that swallows the error without rethrowing terminates the
    // failure here, so it must not be attributed to an outer function.
    if (!catchClauseRethrows(ancestor.handler)) break;
    const ownerFunction = getImmediateEnclosingFunction(ancestor, sourceCode);
    if (ownerFunction && ownerFunction !== immediateFunction) functions.push(ownerFunction);
    break;
  }
  return functions;
}

export const requireErrorCodeForGithubApiThrowRule = createRule({
  name: "require-error-code-for-github-api-throw",
  meta: {
    type: "suggestion",
    docs: {
      description: "In files that import error_codes.cjs, require throw new Error(...) after GitHub API calls in the same function to include a standardized error code.",
    },
    schema: [],
    messages: {
      missingErrorCode: "This throw follows a GitHub API call in a file that imports error_codes.cjs. Prefix the Error message with a standardized code (for example E007, ERR_*, or SAFE_OUTPUT_E007).",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    const importsErrorCodes = hasErrorCodesRequire(sourceCode.ast);
    if (!importsErrorCodes) return {};

    const githubApiCallsByFunction = new Map<FunctionNode, number[]>();

    return {
      CallExpression(node) {
        if (!isGitHubApiCall(node)) return;
        for (const fn of getFunctionsForGitHubApiCall(node, sourceCode)) {
          const calls = githubApiCallsByFunction.get(fn);
          if (calls) calls.push(node.range[0]);
          else githubApiCallsByFunction.set(fn, [node.range[0]]);
        }
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
        const callStarts = githubApiCallsByFunction.get(fn);
        if (!callStarts || !callStarts.some(callStart => callStart < node.range[0])) return;

        context.report({
          node: thrown,
          messageId: "missingErrorCode",
        });
      },
    };
  },
});
