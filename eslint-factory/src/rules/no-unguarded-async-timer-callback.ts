import { ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const TIMER_FUNCTIONS = new Set(["setTimeout", "setInterval", "setImmediate"]);

function isTimerCallee(callee: TSESTree.Node): string | null {
  if (callee.type === "Identifier" && TIMER_FUNCTIONS.has(callee.name)) {
    return callee.name;
  }
  if (
    callee.type === "MemberExpression" &&
    !callee.computed &&
    callee.property.type === "Identifier" &&
    TIMER_FUNCTIONS.has(callee.property.name) &&
    callee.object.type === "Identifier" &&
    (callee.object.name === "globalThis" || callee.object.name === "timers")
  ) {
    return callee.property.name;
  }
  return null;
}

function isFullyGuarded(body: TSESTree.BlockStatement): boolean {
  const statements = body.body.filter(s => s.type !== "EmptyStatement");
  return statements.length === 1 && statements[0].type === "TryStatement" && statements[0].handler !== null;
}

export const noUnguardedAsyncTimerCallbackRule = createRule({
  name: "no-unguarded-async-timer-callback",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow async callbacks passed to setTimeout/setInterval/setImmediate unless the whole body is wrapped in try/catch; a rejection becomes an unhandled rejection that can crash the process.",
    },
    schema: [],
    messages: {
      unguardedAsyncTimer: "The promise returned by this async {{timer}}() callback is discarded, so any thrown error becomes an unhandled rejection. Wrap the entire callback body in try/catch.",
    },
  },
  defaultOptions: [],
  create(context) {
    return {
      CallExpression(node: TSESTree.CallExpression) {
        const timer = isTimerCallee(node.callee);
        if (!timer) {
          return;
        }
        const callback = node.arguments[0];
        if (!callback || (callback.type !== "ArrowFunctionExpression" && callback.type !== "FunctionExpression") || !callback.async) {
          return;
        }
        if (callback.body.type === "BlockStatement" && isFullyGuarded(callback.body)) {
          return;
        }
        context.report({ node: callback, messageId: "unguardedAsyncTimer", data: { timer } });
      },
    };
  },
});
