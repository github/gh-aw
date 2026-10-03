import { AST_NODE_TYPES, ESLintUtils, TSESTree } from "@typescript-eslint/utils";
import { resolveWriteOnceInitializerChain } from "./command-initializer-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const OCTOKIT_CLIENT_NAMES = new Set(["github", "octokit", "githubClient", "octokitClient"]);
const GET_OCTOKIT_MEMBER_OBJECT_NAMES = new Set(["github", "actions", "global", "globalState"]);
const HTTP_METHOD_PREFIXES = new Set(["GET ", "POST ", "PUT ", "PATCH ", "DELETE ", "HEAD ", "OPTIONS "]);

/**
 * Returns true when the node is a template literal that contains at least one
 * interpolated expression (i.e. a tagged or untagged TemplateLiteral with
 * one or more `expressions` entries).
 */
function isInterpolatedTemplateLiteral(node: TSESTree.Node): boolean {
  return node.type === "TemplateLiteral" && node.expressions.length > 0;
}

/**
 * Returns true when the node is a compile-time constant route expression built
 * from literals only.
 */
function isStaticRouteExpression(node: TSESTree.Node): boolean {
  if (node.type === "Literal") return true;
  if (node.type === "TemplateLiteral") return node.expressions.length === 0;
  if (node.type === "BinaryExpression" && node.operator === "+") {
    return isStaticRouteExpression(node.left) && isStaticRouteExpression(node.right);
  }
  return false;
}

/**
 * Returns true when the node is a binary `+` expression, which indicates
 * string concatenation. Nested concatenations such as
 * `"GET /repos/" + owner + "/" + repo` parse as left-associative
 * BinaryExpressions, so the outermost node is still a `+` BinaryExpression
 * and is caught by this check. Compile-time constant concatenations built
 * from literals only are intentionally excluded.
 */
function isStringConcatenation(node: TSESTree.Node): boolean {
  return node.type === "BinaryExpression" && node.operator === "+" && !isStaticRouteExpression(node);
}

/**
 * Returns true when `text` is an exact HTTP-method route prefix including the
 * required trailing space, such as `GET ` or `POST `.
 */
function isValidHttpMethodPrefix(text: string | null | undefined): boolean {
  return typeof text === "string" && HTTP_METHOD_PREFIXES.has(text);
}

/**
 * Returns the human-readable route-expression kind string used in diagnostics,
 * or null when `node` is not one of the interpolated route shapes this rule
 * reports on.
 */
function getInterpolatedRouteKind(node: TSESTree.Node): string | null {
  if (isInterpolatedTemplateLiteral(node)) return "template literal with interpolations";
  if (isStringConcatenation(node)) return "string concatenation expression";
  return null;
}

/**
 * Returns true when the route expression is exactly an HTTP method plus a
 * single opaque route expression, such as:
 * - `POST ${endpoint}`
 * - `"POST " + endpoint`
 *
 * This shape differs from value-into-path interpolation because there is no
 * known static path at the call site to rewrite with `{placeholder}` segments.
 */
function isOpaqueWholeRouteInterpolation(node: TSESTree.Node): boolean {
  if (node.type === "TemplateLiteral") {
    const hasTwoQuasis = node.quasis.length === 2;
    const hasSingleExpression = node.expressions.length === 1;
    const hasMethodPrefix = hasTwoQuasis && isValidHttpMethodPrefix(node.quasis[0].value.cooked);
    const hasEmptyTrailingQuasi = hasTwoQuasis && node.quasis[1].value.cooked === "";
    const isDynamicWholeRoute = hasSingleExpression && !isStaticRouteExpression(node.expressions[0]);
    return hasMethodPrefix && hasEmptyTrailingQuasi && isDynamicWholeRoute;
  }

  if (node.type === "BinaryExpression" && node.operator === "+") {
    if (node.left.type !== "Literal" || typeof node.left.value !== "string") return false;
    const hasMethodPrefix = isValidHttpMethodPrefix(node.left.value);
    const isRightDynamic = !isStaticRouteExpression(node.right);
    return hasMethodPrefix && isRightDynamic;
  }

  return false;
}

/**
 * Returns true when `node` is the `context.github` member expression.
 */
function isContextGithubExpression(node: TSESTree.Node): boolean {
  return (
    node.type === AST_NODE_TYPES.MemberExpression && !node.computed && node.object.type === AST_NODE_TYPES.Identifier && node.object.name === "context" && node.property.type === AST_NODE_TYPES.Identifier && node.property.name === "github"
  );
}

/**
 * Returns true when a syntactic expression node directly represents a known
 * Octokit client source without scope resolution. Recognizes:
 * - Direct known names: github, octokit, githubClient, octokitClient
 * - `getOctokit(...)` call results (bare or via known module objects, e.g.
 *   `github.getOctokit(...)` or `actions.getOctokit(...)`)
 * - `context.github` member expression
 * - Fallback (`||` / `??`) and ternary expressions with a known source branch
 */
function isOctokitSourceExpression(node: TSESTree.Node): boolean {
  if (node.type === AST_NODE_TYPES.Identifier && OCTOKIT_CLIENT_NAMES.has(node.name)) return true;
  if (node.type === AST_NODE_TYPES.LogicalExpression && (node.operator === "||" || node.operator === "??")) return isOctokitSourceExpression(node.left) || isOctokitSourceExpression(node.right);
  if (node.type === AST_NODE_TYPES.ConditionalExpression) return isOctokitSourceExpression(node.consequent) || isOctokitSourceExpression(node.alternate);

  if (node.type === AST_NODE_TYPES.CallExpression) {
    const callee = node.callee;
    if (callee.type === AST_NODE_TYPES.Identifier && callee.name === "getOctokit") return true;
    if (
      callee.type === AST_NODE_TYPES.MemberExpression &&
      !callee.computed &&
      callee.object.type === AST_NODE_TYPES.Identifier &&
      GET_OCTOKIT_MEMBER_OBJECT_NAMES.has(callee.object.name) &&
      callee.property.type === AST_NODE_TYPES.Identifier &&
      callee.property.name === "getOctokit"
    ) {
      return true;
    }
  }

  if (isContextGithubExpression(node)) return true;

  return false;
}

export const noGithubRequestInterpolatedRouteRule = createRule({
  name: "no-github-request-interpolated-route",
  meta: {
    type: "problem",
    docs: {
      description:
        "Disallow template literals with interpolations or string concatenation as the route argument of Octokit.request() calls. " +
        "Octokit clients are detected by well-known names (github, octokit, githubClient, octokitClient), " +
        "identifiers initialized from getOctokit(...) call results, context.github, and const aliases or fallback expressions containing any of these. " +
        "Use the typed placeholder form for value interpolation, or thread a typed route string from the caller when the entire route is dynamic.",
    },
    schema: [],
    messages: {
      opaqueWholeRoute:
        "Avoid using an opaque whole-route {{kind}} as the route argument of {{client}}.request(). " +
        "When the entire route is dynamic, pass a typed route string from the caller instead of interpolating or concatenating a route variable.",
      interpolatedRoute:
        "Avoid using a {{kind}} as the route argument of {{client}}.request(). " +
        'Use the typed placeholder form instead — e.g. github.request("GET /repos/{owner}/{repo}", { owner, repo }) — ' +
        "to preserve typed dispatch and prevent malformed paths.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    type SourceCodeScope = ReturnType<typeof sourceCode.getScope>;

    /**
     * Resolves an identifier name to check if it is bound to a known Octokit
     * client source in the visible scope chain. Handles simple single-level
     * assignments:
     *   const x = github
     *   const x = getOctokit(token)
     *   const x = context.github
     */
    function isIdentifierBoundToOctokitClient(name: string, scopeNode: TSESTree.Node): boolean {
      if (OCTOKIT_CLIENT_NAMES.has(name)) return true;

      let scope: SourceCodeScope | null = sourceCode.getScope(scopeNode);
      while (scope) {
        const variable = scope.set.get(name);
        if (variable && variable.defs.length > 0) {
          for (const def of variable.defs) {
            if (def.type !== "Variable") continue;
            const declarator = def.node as TSESTree.VariableDeclarator;
            const declaration = declarator.parent;
            if (!declaration || declaration.type !== AST_NODE_TYPES.VariableDeclaration || declaration.kind !== "const") continue;
            if (declarator.init && isOctokitSourceExpression(declarator.init)) return true;
          }
          return false;
        }
        scope = scope.upper;
      }
      return false;
    }

    /**
     * Returns the display name (for error messages) if the callee object
     * resolves to a known Octokit client, or null otherwise.
     *
     * Recognized shapes:
     * - `<name>.request(...)` where name is in OCTOKIT_CLIENT_NAMES or is a
     *   simple alias of an Octokit source (scope-resolved)
     * - `context.github.request(...)`
     */
    function resolveOctokitClientName(calleeObject: TSESTree.Expression | TSESTree.Super, callNode: TSESTree.Node): string | null {
      if (calleeObject.type === AST_NODE_TYPES.Identifier) {
        return isIdentifierBoundToOctokitClient(calleeObject.name, callNode) ? calleeObject.name : null;
      }

      if (isContextGithubExpression(calleeObject)) {
        return "context.github";
      }

      return null;
    }

    return {
      CallExpression(node) {
        const callee = node.callee;

        // Only match <client>.request(...)
        if (callee.type !== AST_NODE_TYPES.MemberExpression) return;
        if (callee.computed) return;
        if (callee.property.type !== AST_NODE_TYPES.Identifier) return;
        if (callee.property.name !== "request") return;

        const clientName = resolveOctokitClientName(callee.object, node);
        if (!clientName) return;

        const firstArg = node.arguments[0];
        if (!firstArg || firstArg.type === AST_NODE_TYPES.SpreadElement) return;

        const routeExpr = resolveWriteOnceInitializerChain(firstArg as TSESTree.Expression, context.sourceCode);

        const routeKind = getInterpolatedRouteKind(routeExpr);
        if (!routeKind) return;

        if (isOpaqueWholeRouteInterpolation(routeExpr)) {
          context.report({
            node: firstArg,
            messageId: "opaqueWholeRoute",
            data: { kind: routeKind, client: clientName },
          });
          return;
        }

        context.report({
          node: firstArg,
          messageId: "interpolatedRoute",
          data: { kind: routeKind, client: clientName },
        });
      },
    };
  },
});
