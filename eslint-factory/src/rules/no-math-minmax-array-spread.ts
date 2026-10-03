import { AST_NODE_TYPES, ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const MATH_MIN_MAX_METHODS = new Set(["min", "max"]);

// The identity value for each method: the value returned by `Math.min()` / `Math.max()`
// when called with no arguments (and thus what an empty spread, e.g. `Math.max(...[])`,
// evaluates to). Using it as the `reduce()` initializer keeps empty-input behavior
// identical to the spread form instead of throwing on an empty array.
const IDENTITY_VALUE: Record<string, string> = { min: "Infinity", max: "-Infinity" };

export const noMathMinMaxArraySpreadRule = createRule({
  name: "no-math-minmax-array-spread",
  meta: {
    type: "problem",
    docs: {
      description:
        "Disallow spreading an array of unknown size into Math.min(...) / Math.max(...). Spreading a large array into call arguments can throw `RangeError: Maximum call stack size exceeded` once the array exceeds the engine argument limit, so arrays whose size depends on runtime data must be reduced instead.",
    },
    schema: [],
    messages: {
      noMathMinMaxArraySpread:
        "Avoid {{invocation}} — spreading an array of unknown size can throw `RangeError: Maximum call stack size exceeded`. Use `{{arg}}.reduce((a, b) => Math.{{method}}(a, b), {{identity}})` instead — the `{{identity}}` initializer preserves the same result as `{{invocation}}` on an empty array.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    type SourceCodeScope = ReturnType<typeof sourceCode.getScope>;
    const BOUNDED_ARRAY_METHODS = new Set(["filter", "slice"]);

    /**
     * Checks whether a given identifier name is locally bound in the current scope chain,
     * which means the global `Math` object is shadowed at that location.
     */
    function hasLocalBinding(node: TSESTree.Node, name: string): boolean {
      let scope: SourceCodeScope | null = sourceCode.getScope(node);

      while (scope) {
        const variable = scope.set.get(name);
        if (variable && variable.defs.length > 0) return true;
        scope = scope.upper;
      }

      return false;
    }

    /**
     * Returns "min" / "max" when the member expression accesses `Math.min` or `Math.max`
     * (direct or string-literal computed access), and undefined otherwise.
     */
    function getMathMinMaxMethod(node: TSESTree.MemberExpression): string | undefined {
      if (node.object.type !== AST_NODE_TYPES.Identifier || node.object.name !== "Math") return undefined;

      const property = node.property;
      if (!node.computed && property.type === AST_NODE_TYPES.Identifier && MATH_MIN_MAX_METHODS.has(property.name)) return property.name;
      if (node.computed && property.type === AST_NODE_TYPES.Literal && typeof property.value === "string" && MATH_MIN_MAX_METHODS.has(property.value)) return property.value;

      return undefined;
    }

    /**
     * Returns true unless a const binding starts with a fixed-size array literal
     * and only passes through size-non-increasing array methods.
     */
    function isBoundedArrayExpression(node: TSESTree.Node): boolean {
      if (node.type === AST_NODE_TYPES.ArrayExpression) {
        return node.elements.every(element => element?.type !== AST_NODE_TYPES.SpreadElement);
      }
      if (node.type === AST_NODE_TYPES.CallExpression && !node.optional && node.callee.type === AST_NODE_TYPES.MemberExpression && !node.callee.optional && !node.callee.computed) {
        const { object, property } = node.callee;
        return property.type === AST_NODE_TYPES.Identifier && BOUNDED_ARRAY_METHODS.has(property.name) && isBoundedArrayExpression(object);
      }
      return false;
    }

    function isUnboundedSpreadArgument(node: TSESTree.Node): boolean {
      if (node.type === AST_NODE_TYPES.Identifier) {
        let scope: SourceCodeScope | null = sourceCode.getScope(node);
        while (scope) {
          const variable = scope.set.get(node.name);
          if (variable) {
            if (variable.defs.length !== 1 || variable.references.some(ref => ref.isWrite() && !ref.init)) return true;
            const def = variable.defs[0];
            if (def.type !== "Variable" || def.parent.kind !== "const" || def.node.id.type !== AST_NODE_TYPES.Identifier) return true;
            // Const fixes the binding, not the array's length. Reject prior uses
            // that could mutate or pass the array to code that mutates it.
            if (
              variable.references.some(ref => {
                const identifier = ref.identifier;
                if (ref.init || identifier === node || identifier.range[0] >= node.range[0]) return false;
                const parent = identifier.parent;
                if (parent.type !== AST_NODE_TYPES.MemberExpression || parent.object !== identifier || parent.computed || parent.property.type !== AST_NODE_TYPES.Identifier || parent.property.name !== "length") return true;
                const use = parent.parent;
                return (use.type === AST_NODE_TYPES.AssignmentExpression && use.left === parent) || (use.type === AST_NODE_TYPES.UpdateExpression && use.argument === parent);
              })
            )
              return true;
            return !def.node.init || !isBoundedArrayExpression(def.node.init);
          }
          scope = scope.upper;
        }
        return true;
      }
      return node.type === AST_NODE_TYPES.MemberExpression || node.type === AST_NODE_TYPES.CallExpression;
    }

    return {
      CallExpression(node) {
        if (node.callee.type !== AST_NODE_TYPES.MemberExpression) return;

        const method = getMathMinMaxMethod(node.callee);
        if (!method) return;
        if (hasLocalBinding(node, "Math")) return;

        // Fixed extra arguments (e.g. `Math.max(0, ...arr)`) are clamp/floor values, not a
        // size bound on the spread array — they don't change the crash risk, so every
        // spread argument is checked regardless of how many fixed arguments accompany it.
        const fixedArgsText = node.arguments.filter(argument => argument.type !== AST_NODE_TYPES.SpreadElement).map(argument => sourceCode.getText(argument));

        // The reduce initializer must preserve the result of calling Math.min/max with the
        // fixed arguments (and no spread elements at all), so fold them into the seed:
        // no fixed args keeps the identity value, one fixed arg is used as-is, and multiple
        // fixed args are folded together with the same method first.
        const seed = fixedArgsText.length === 0 ? IDENTITY_VALUE[method] : fixedArgsText.length === 1 ? fixedArgsText[0] : `Math.${method}(${fixedArgsText.join(", ")})`;

        for (const argument of node.arguments) {
          if (argument.type !== AST_NODE_TYPES.SpreadElement) continue;
          if (!isUnboundedSpreadArgument(argument.argument)) continue;

          const argText = sourceCode.getText(argument.argument);
          const invocation = fixedArgsText.length === 0 ? `Math.${method}(...${argText})` : `Math.${method}(${fixedArgsText.join(", ")}, ...${argText})`;

          context.report({
            node,
            messageId: "noMathMinMaxArraySpread",
            data: { method, arg: argText, identity: seed, invocation },
          });
        }
      },
    };
  },
});
