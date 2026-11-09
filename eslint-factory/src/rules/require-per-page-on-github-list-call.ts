import { ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const nonPaginatedRestListMethods = new Set(["repos.listLanguages"]);

function isRestListCallee(callee: TSESTree.Expression): string | null {
  // Matches <client>.rest.<namespace>.list*(...)
  if (callee.type !== "MemberExpression" || callee.computed || callee.property.type !== "Identifier" || !callee.property.name.startsWith("list")) {
    return null;
  }
  const ns = callee.object;
  if (ns.type !== "MemberExpression" || ns.computed || ns.property.type !== "Identifier") {
    return null;
  }
  const rest = ns.object;
  if (rest.type !== "MemberExpression" || rest.computed || rest.property.type !== "Identifier" || rest.property.name !== "rest") {
    return null;
  }
  return `${ns.property.name}.${callee.property.name}`;
}

export const requirePerPageOnGithubListCallRule = createRule({
  name: "require-per-page-on-github-list-call",
  meta: {
    type: "problem",
    docs: {
      description: "Require an explicit per_page on Octokit REST list* calls, otherwise only the default 30 items are returned.",
    },
    schema: [],
    messages: {
      missingPerPage: "{{method}}() without per_page returns only the default 30 items. Pass per_page: 100 (and paginate if more results may exist).",
    },
  },
  defaultOptions: [],
  create(context) {
    return {
      CallExpression(node: TSESTree.CallExpression) {
        const method = isRestListCallee(node.callee);
        if (!method || nonPaginatedRestListMethods.has(method) || node.arguments.length !== 1) {
          return;
        }
        const arg = node.arguments[0];
        if (arg.type !== "ObjectExpression") {
          return;
        }
        // A spread may supply per_page; stay conservative.
        if (arg.properties.some(p => p.type === "SpreadElement")) {
          return;
        }
        const perPageProperty = arg.properties.find(p => p.type === "Property" && ((!p.computed && p.key.type === "Identifier" && p.key.name === "per_page") || (p.key.type === "Literal" && p.key.value === "per_page")));
        const hasUndefinedPerPage =
          perPageProperty?.type === "Property" &&
          ((perPageProperty.value.type === "Identifier" && perPageProperty.value.name === "undefined") || (perPageProperty.value.type === "UnaryExpression" && perPageProperty.value.operator === "void"));
        if (!perPageProperty || hasUndefinedPerPage) {
          context.report({ node: node.callee, messageId: "missingPerPage", data: { method } });
        }
      },
    };
  },
});
