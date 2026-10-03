import { ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const TIMER_FUNCTIONS = new Set(["setTimeout", "setInterval", "setImmediate"]);

function isFunctionNode(node: TSESTree.Node): boolean {
  return node.type === "ArrowFunctionExpression" || node.type === "FunctionExpression" || node.type === "FunctionDeclaration";
}

function isTimerCallee(callee: TSESTree.Expression): boolean {
  if (callee.type === "Identifier") {
    return TIMER_FUNCTIONS.has(callee.name);
  }
  return callee.type === "MemberExpression" && !callee.computed && callee.object.type === "Identifier" && (callee.object.name === "global" || callee.object.name === "globalThis") && callee.property.type === "Identifier" && TIMER_FUNCTIONS.has(callee.property.name);
}

/** Returns true when `node` sits in the `try` block of a try/catch inside `fn`. */
function isGuardedByCatch(node: TSESTree.Node, fn: TSESTree.Node): boolean {
  let child: TSESTree.Node = node;
  let parent = node.parent;
  while (parent && parent !== fn) {
    if (parent.type === "TryStatement" && parent.handler && parent.block === child) {
      return true;
    }
    child = parent;
    parent = parent.parent;
  }
  return false;
}

export const noUnhandledAsyncTimerCallbackRule = createRule({
  name: "no-unhandled-async-timer-callback",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow `await` outside try/catch in async setTimeout/setInterval callbacks; the timer discards the returned promise, so a rejection becomes an unhandled rejection that can crash the process.",
    },
    schema: [],
    messages: {
      unhandledAwait: "This `await` is inside an async {{timer}}() callback without a surrounding try/catch. The timer ignores the returned promise, so a rejection is unhandled. Wrap the callback body in try/catch.",
    },
  },
  defaultOptions: [],
  create(context) {
    const asyncTimerCallbacks: { fn: TSESTree.Node; timer: string }[] = [];

    function enclosingFunction(node: TSESTree.Node): TSESTree.Node | null {
      let p = node.parent;
      while (p) {
        if (isFunctionNode(p)) return p;
        p = p.parent;
      }
      return null;
    }

    return {
      CallExpression(node: TSESTree.CallExpression) {
        if (!isTimerCallee(node.callee)) return;
        const cb = node.arguments[0];
        if (!cb || (cb.type !== "ArrowFunctionExpression" && cb.type !== "FunctionExpression") || !cb.async) return;
        const callee = node.callee;
        const timer = callee.type === "Identifier" ? callee.name : callee.type === "MemberExpression" && callee.property.type === "Identifier" ? callee.property.name : "timer";
        asyncTimerCallbacks.push({ fn: cb, timer });
      },
      AwaitExpression(node: TSESTree.AwaitExpression) {
        const fn = enclosingFunction(node);
        if (!fn) return;
        const entry = asyncTimerCallbacks.find(e => e.fn === fn);
        if (!entry || isGuardedByCatch(node, fn)) return;
        context.report({ node, messageId: "unhandledAwait", data: { timer: entry.timer } });
      },
    };
  },
});
