import { AST_NODE_TYPES, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const HTTP_MODULE_SPECIFIERS = new Set(["http", "https", "node:http", "node:https"]);
const REQUEST_METHODS = new Set(["request", "get"]);

/** Resolves `name` to the variable it denotes at `scopeNode`, or null when it is not declared in any enclosing scope. */
function resolveVariable(name: string, scopeNode: TSESTree.Node, sourceCode: TSESLint.SourceCode): TSESLint.Scope.Variable | null {
  let scope: ReturnType<typeof sourceCode.getScope> | null = sourceCode.getScope(scopeNode);
  while (scope) {
    const variable = scope.set.get(name);
    if (variable) return variable;
    scope = scope.upper;
  }
  return null;
}

/** Returns true when `require` at this location is not shadowed by a local declaration (parameter, variable, function, ...). */
function isModuleRequireIdentifier(callee: TSESTree.Identifier, sourceCode: TSESLint.SourceCode): boolean {
  const variable = resolveVariable(callee.name, callee, sourceCode);
  // Undeclared, or the implicit CommonJS/global `require` (declared by the environment, so it has no definitions).
  return variable === null || variable.defs.length === 0;
}

/** Returns true when the node is `require("http")` / `require("https")` / `require("node:http")` / `require("node:https")`. */
function isRequireHttpModule(node: TSESTree.Node | null | undefined, sourceCode: TSESLint.SourceCode): boolean {
  if (!node) return false;
  if (node.type !== AST_NODE_TYPES.CallExpression) return false;
  if (node.callee.type !== AST_NODE_TYPES.Identifier || node.callee.name !== "require") return false;
  if (!isModuleRequireIdentifier(node.callee, sourceCode)) return false;
  const firstArg = node.arguments[0];
  if (!firstArg || firstArg.type !== AST_NODE_TYPES.Literal) return false;
  return typeof firstArg.value === "string" && HTTP_MODULE_SPECIFIERS.has(firstArg.value);
}

/** Returns true when `node` resolves to Node's `http`/`https` module. */
function isHttpModuleExpression(node: TSESTree.Node | null | undefined, sourceCode: TSESLint.SourceCode, visited: Set<TSESLint.Scope.Variable>): boolean {
  if (!node) return false;
  if (isRequireHttpModule(node, sourceCode)) return true;
  if (node.type === AST_NODE_TYPES.Identifier) return isHttpModuleBinding(node.name, node, sourceCode, visited);
  if (node.type !== AST_NODE_TYPES.ConditionalExpression) return false;
  return isHttpModuleExpression(node.consequent, sourceCode, visited) && isHttpModuleExpression(node.alternate, sourceCode, visited);
}

/** Returns true when `name` resolves to a variable bound to Node's `http`/`https` module and never reassigned. */
function isHttpModuleBinding(name: string, scopeNode: TSESTree.Node, sourceCode: TSESLint.SourceCode, visited = new Set<TSESLint.Scope.Variable>()): boolean {
  const variable = resolveVariable(name, scopeNode, sourceCode);
  if (!variable || variable.defs.length === 0 || visited.has(variable)) return false;
  visited.add(variable);
  try {
    // Any write other than an HTTP module expression means the binding may no longer denote the module.
    for (const reference of variable.references) {
      if (!reference.isWrite()) continue;
      if (!isHttpModuleExpression(reference.writeExpr, sourceCode, visited)) return false;
    }
    let bound = false;
    for (const def of variable.defs) {
      if (def.type !== "Variable") return false;
      const declarator = def.node as TSESTree.VariableDeclarator;
      if (declarator.id.type !== AST_NODE_TYPES.Identifier) return false;
      if (!isHttpModuleExpression(declarator.init, sourceCode, visited)) return false;
      bound = true;
    }
    return bound;
  } finally {
    visited.delete(variable);
  }
}

/** Returns true when `call` is `<http>.request(...)` / `<http>.get(...)` on a resolved http/https module binding. */
function isHttpRequestCall(call: TSESTree.CallExpression, sourceCode: TSESLint.SourceCode): boolean {
  const callee = call.callee;
  if (callee.type !== AST_NODE_TYPES.MemberExpression || callee.computed) return false;
  if (callee.object.type !== AST_NODE_TYPES.Identifier) return false;
  if (callee.property.type !== AST_NODE_TYPES.Identifier || !REQUEST_METHODS.has(callee.property.name)) return false;
  return isHttpModuleBinding(callee.object.name, callee.object, sourceCode);
}

type ResponseCallback = TSESTree.FunctionExpression | TSESTree.ArrowFunctionExpression;

// Node's EventEmitter treats "on" and "addListener" as synonyms, and "once" for a one-shot variant.
const LISTENER_METHODS = new Set(["on", "once", "addListener"]);

/** Returns the response callback argument of an http request call, when it has a single named response parameter. */
function getResponseCallback(call: TSESTree.CallExpression): ResponseCallback | null {
  for (const arg of call.arguments) {
    if (arg.type !== AST_NODE_TYPES.FunctionExpression && arg.type !== AST_NODE_TYPES.ArrowFunctionExpression) continue;
    const firstParam = arg.params[0];
    if (!firstParam || firstParam.type !== AST_NODE_TYPES.Identifier) return null;
    return arg;
  }
  return null;
}

/** Returns true when `call` is `<name>.on("error", ...)` / `<name>.once("error", ...)` / `<name>.addListener("error", ...)`. */
function isErrorListenerCall(call: TSESTree.CallExpression, name: string): boolean {
  const callee = call.callee;
  if (callee.type !== AST_NODE_TYPES.MemberExpression || callee.computed) return false;
  if (callee.object.type !== AST_NODE_TYPES.Identifier || callee.object.name !== name) return false;
  if (callee.property.type !== AST_NODE_TYPES.Identifier || !LISTENER_METHODS.has(callee.property.name)) return false;
  const firstArg = call.arguments[0];
  return firstArg !== undefined && firstArg.type === AST_NODE_TYPES.Literal && firstArg.value === "error";
}

/** Returns true when `node` is a direct `<http>.request(...)` / `<http>.get(...)` call expression. */
function isHttpRequestResultExpression(node: TSESTree.Node | null | undefined, sourceCode: TSESLint.SourceCode): boolean {
  if (!node) return false;
  return node.type === AST_NODE_TYPES.CallExpression && isHttpRequestCall(node, sourceCode);
}

/**
 * Returns true when `name` (declared at `scopeNode`) is bound, via a single variable declarator,
 * to the direct result of an http/https `request()`/`get()` call — e.g. `const req = http.request(...)` —
 * and every write to the binding keeps it holding such a request, so a reassigned variable pointing at an
 * unrelated object is never treated as a Node request.
 */
function isHttpRequestResultBinding(name: string, scopeNode: TSESTree.Node, sourceCode: TSESLint.SourceCode): boolean {
  const variable = resolveVariable(name, scopeNode, sourceCode);
  if (!variable || variable.defs.length !== 1) return false;
  const def = variable.defs[0];
  if (def.type !== "Variable") return false;
  const declarator = def.node as TSESTree.VariableDeclarator;
  if (declarator.id.type !== AST_NODE_TYPES.Identifier) return false;
  if (!isHttpRequestResultExpression(declarator.init, sourceCode)) return false;
  // Any write other than another http request call means the binding may no longer denote a request.
  for (const reference of variable.references) {
    if (!reference.isWrite()) continue;
    if (!isHttpRequestResultExpression(reference.writeExpr, sourceCode)) return false;
  }
  return true;
}

/**
 * Returns the callback attached via the `req.on("response", cb)` idiom (or `.once`/`.addListener`
 * variant, including the chained `http.request(...).on("response", cb)` form), when `req` resolves
 * to the direct result of an http/https `request()`/`get()` call and `cb` has a single named
 * response parameter. Returns null otherwise.
 */
function getResponseEventCallback(call: TSESTree.CallExpression, sourceCode: TSESLint.SourceCode): ResponseCallback | null {
  const callee = call.callee;
  if (callee.type !== AST_NODE_TYPES.MemberExpression || callee.computed) return null;
  if (callee.property.type !== AST_NODE_TYPES.Identifier || !LISTENER_METHODS.has(callee.property.name)) return null;
  const firstArg = call.arguments[0];
  if (!firstArg || firstArg.type !== AST_NODE_TYPES.Literal || firstArg.value !== "response") return null;

  const object = callee.object;
  let isFromHttpRequest = false;
  if (object.type === AST_NODE_TYPES.Identifier) {
    isFromHttpRequest = isHttpRequestResultBinding(object.name, object, sourceCode);
  } else if (object.type === AST_NODE_TYPES.CallExpression) {
    isFromHttpRequest = isHttpRequestCall(object, sourceCode);
  }
  if (!isFromHttpRequest) return null;

  const cb = call.arguments[1];
  if (!cb || (cb.type !== AST_NODE_TYPES.FunctionExpression && cb.type !== AST_NODE_TYPES.ArrowFunctionExpression)) return null;
  const firstParam = cb.params[0];
  if (!firstParam || firstParam.type !== AST_NODE_TYPES.Identifier) return null;
  return cb;
}

export const requireHttpResponseErrorListenerRule = createRule({
  name: "require-http-response-error-listener",
  meta: {
    type: "problem",
    docs: {
      description:
        "Require an 'error' event listener on the response object passed to http.request()/http.get()/https.request()/https.get() callbacks. " +
        "Node emits 'error' on the IncomingMessage itself for socket-level failures that occur while the body is streamed " +
        "(reset connections, decompression failures, aborted sockets); a listener on the request does not catch these, " +
        "so an unhandled response 'error' event crashes the action. Also recognizes the `req.on(\"response\", cb)` idiom " +
        "(listening for the response event on the request object returned by request()/get(), rather than an inline callback) " +
        'and treats `.addListener("error", ...)` as equivalent to `.on("error", ...)` since it is a documented EventEmitter alias. ' +
        'Scope: only fires when the http/https module identifier is statically resolved through a `require("http")`-style binding.',
    },
    schema: [],
    messages: {
      missingResponseErrorListener:
        "The response passed to this http request callback must have an 'error' event listener attached (e.g. `res.on(\"error\", reject)`). " +
        "Node emits 'error' on the response for socket-level failures while the body streams; `req.on(\"error\", ...)` does not catch those and the unhandled event crashes the action.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;

    /** Reports `callback` when its declared response parameter never gets an 'error' listener attached. */
    function checkResponseCallback(callback: ResponseCallback) {
      const param = callback.params[0];
      if (!param || param.type !== AST_NODE_TYPES.Identifier) return;
      const responseName = param.name;

      const variable = sourceCode.getDeclaredVariables(callback).find(candidate => candidate.name === responseName);
      if (!variable) return;

      const hasErrorListener = variable.references.some(ref => {
        const id = ref.identifier;
        const parent = id.parent;
        if (!parent || parent.type !== AST_NODE_TYPES.MemberExpression || parent.object !== id) return false;
        const grandparent = parent.parent;
        return grandparent !== undefined && grandparent.type === AST_NODE_TYPES.CallExpression && grandparent.callee === parent && isErrorListenerCall(grandparent, responseName);
      });

      if (!hasErrorListener) {
        context.report({ node: param, messageId: "missingResponseErrorListener" });
      }
    }

    return {
      CallExpression(node: TSESTree.CallExpression) {
        // Inline callback form: http.request(options, res => { ... })
        if (isHttpRequestCall(node, sourceCode)) {
          const callback = getResponseCallback(node);
          if (callback) checkResponseCallback(callback);
        }

        // Separate-listener idiom: req.on("response", res => { ... }) / .once(...) / .addListener(...),
        // including the chained http.request(...).on("response", cb) form.
        const responseCallback = getResponseEventCallback(node, sourceCode);
        if (responseCallback) checkResponseCallback(responseCallback);
      },
    };
  },
});
