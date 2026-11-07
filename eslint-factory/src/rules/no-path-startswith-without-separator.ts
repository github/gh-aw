import { ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const PATH_METHODS = new Set(["resolve", "join", "normalize"]);

export const noPathStartsWithoutSeparatorRule = createRule({
  name: "no-path-startswith-without-separator",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow containment checks that call startsWith with a bare path.resolve/join/normalize result, because sibling directories sharing the prefix pass the check.",
    },
    schema: [],
    messages: {
      noBarePathPrefix:
        "startsWith() with a bare directory path also matches sibling directories (e.g. /work/repo-evil starts with /work/repo). Compare against `root.endsWith(path.sep) ? root : root + path.sep` (and allow `=== root` separately), or use path.relative() and reject `relative === '..'`, `relative.startsWith('..' + path.sep)`, or `path.isAbsolute(relative)`.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;

    function isPathCall(node: TSESTree.Node): boolean {
      return (
        node.type === "CallExpression" &&
        node.callee.type === "MemberExpression" &&
        !node.callee.computed &&
        node.callee.object.type === "Identifier" &&
        node.callee.object.name === "path" &&
        node.callee.property.type === "Identifier" &&
        PATH_METHODS.has(node.callee.property.name)
      );
    }

    function isPathSeparator(node: TSESTree.Node): boolean {
      if (node.type === "MemberExpression" && !node.computed && node.object.type === "Identifier" && node.object.name === "path" && node.property.type === "Identifier") {
        return node.property.name === "sep";
      }
      if (node.type === "Literal" && typeof node.value === "string") {
        return node.value.endsWith("/");
      }
      return node.type === "TemplateLiteral" && node.expressions.length === 0 && node.quasis[0]?.value.cooked?.endsWith("/") === true;
    }

    function isSeparatorTerminatedPathCall(node: TSESTree.Node): boolean {
      if (!isPathCall(node) || node.type !== "CallExpression" || node.callee.type !== "MemberExpression" || node.callee.property.type !== "Identifier") {
        return false;
      }
      const method = node.callee.property.name;
      if (method === "join") {
        const lastArgument = node.arguments[node.arguments.length - 1];
        return lastArgument != null && isPathSeparator(lastArgument);
      }
      if (method === "normalize") {
        const argument = node.arguments[0];
        return argument != null && isPathSeparator(argument);
      }
      return false;
    }

    function isBarePathValue(node: TSESTree.Node): boolean {
      if (isPathCall(node)) {
        return !isSeparatorTerminatedPathCall(node);
      }
      if (node.type !== "Identifier") {
        return false;
      }
      let scope: ReturnType<typeof sourceCode.getScope> | null = sourceCode.getScope(node);
      while (scope) {
        const variable = scope.set.get(node.name);
        if (variable) {
          if (variable.defs.length !== 1 || variable.references.some(ref => ref.isWrite() && !ref.init)) {
            return false;
          }
          const def = variable.defs[0];
          return def.type === "Variable" && def.node.type === "VariableDeclarator" && def.node.id.type === "Identifier" && def.node.init != null && isPathCall(def.node.init) && !isSeparatorTerminatedPathCall(def.node.init);
        }
        scope = scope.upper;
      }
      return false;
    }

    return {
      CallExpression(node) {
        const callee = node.callee;
        if (callee.type !== "MemberExpression" || callee.computed || callee.property.type !== "Identifier" || callee.property.name !== "startsWith") {
          return;
        }
        const arg = node.arguments[0];
        if (arg && isBarePathValue(arg)) {
          context.report({ node: arg, messageId: "noBarePathPrefix" });
        }
      },
    };
  },
});
