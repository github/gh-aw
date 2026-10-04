import { AST_NODE_TYPES, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

// Matches function/method names that are specifically regex-escaping helpers,
// e.g. escapeRegExp, escapeRegex, regExpEscape. Requires both "escape" and "reg"
// to be present, preventing false negatives from escapeHtml, unescape, etc.
const ESCAPE_CALL_NAME_PATTERN = /escape.*reg|reg.*escape/i;
const REGEXP_META_CHARS = new Set(["\\", "^", "$", ".", "*", "+", "?", "(", ")", "[", "]", "{", "}", "|"]);

// Matches identifier/property names that signal a value has already been
// regex-escaped, e.g. escapedValue, ESCAPED_NAME. Requires the name to START
// with "escaped", so unescapedValue and escapeHelper are never whitelisted.
const ESCAPED_IDENT_PATTERN = /^escaped/i;
const REGEXP_FRAGMENT_IDENT_PATTERN = /_(?:PATTERN|RE(?:_SOURCE)?|CHAR|DELIMITERS)$/i;

// Raw pattern (between regex delimiters) of the canonical inline metacharacter
// escape regex: /[.*+?^${}()|[\]\\]/g  — the only search form accepted when
// the replacement is the `"\\$&"` back-reference token.
const CANONICAL_METACHAR_REGEX_PATTERN = "[.*+?^${}()|[\\]\\\\]";

/**
 * Returns true when `node` is a call expression whose callee name looks like
 * a regex-escaping helper (e.g. `escapeRegExp(value)`, `utils.escapeRegex(value)`).
 * Both "escape" and "reg" must appear in the name so unrelated helpers such as
 * `escapeHtml` or `unescape` are not treated as regex-safe.
 */
function isEscapeHelperCall(node: TSESTree.Node): boolean {
  if (node.type !== AST_NODE_TYPES.CallExpression) return false;
  const callee = node.callee;
  if (callee.type === AST_NODE_TYPES.Identifier) return ESCAPE_CALL_NAME_PATTERN.test(callee.name);
  if (callee.type === AST_NODE_TYPES.MemberExpression && !callee.computed && callee.property.type === AST_NODE_TYPES.Identifier) {
    return ESCAPE_CALL_NAME_PATTERN.test(callee.property.name);
  }
  return false;
}

/**
 * Returns true when `node` is a regex literal that matches exactly
 * `/[.*+?^${}()|[\]\\]/g` — the canonical form that escapes every regex
 * metacharacter. Requires the global flag and rejects sticky (`y`) or any
 * other flag combination so that narrower patterns are not accepted.
 */
function isCanonicalMetacharEscapeRegex(node: TSESTree.Node): boolean {
  if (node.type !== AST_NODE_TYPES.Literal || !("regex" in node) || !node.regex) return false;
  return node.regex.pattern === CANONICAL_METACHAR_REGEX_PATTERN && node.regex.flags === "g";
}

/**
 * Returns true when `node` is a call of the form
 * `value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")` — the standard inline
 * pattern for escaping all regex metacharacters before interpolation.
 */
function isRegexEscapeReplaceCall(node: TSESTree.Node): boolean {
  if (node.type !== AST_NODE_TYPES.CallExpression) return false;
  const { callee, arguments: args } = node;
  if (callee.type !== AST_NODE_TYPES.MemberExpression || callee.computed) return false;
  if (callee.property.type !== AST_NODE_TYPES.Identifier || callee.property.name !== "replace") return false;
  if (args.length < 2) return false;
  const search = getFixedLiteralSearchText(args[0]);
  const replacement = getStringLiteralValue(args[1]);
  if (replacement === "\\$&") return isCanonicalMetacharEscapeRegex(args[0]);
  return search !== null && replacement !== null && isLiteralRegexEscapeReplacement(search, replacement);
}

function getStringLiteralValue(node: TSESTree.Node): string | null {
  return node.type === AST_NODE_TYPES.Literal && typeof node.value === "string" ? node.value : null;
}

function getFixedLiteralSearchText(node: TSESTree.Node): string | null {
  const stringValue = getStringLiteralValue(node);
  if (stringValue !== null) return stringValue;
  if (node.type !== AST_NODE_TYPES.Literal || !("regex" in node) || !node.regex) return null;
  return decodeFixedLiteralRegexPattern(node.regex.pattern);
}

function decodeFixedLiteralRegexPattern(pattern: string): string | null {
  let decoded = "";

  for (let index = 0; index < pattern.length; index++) {
    const char = pattern[index];
    if (char === "\\") {
      index++;
      const escapedChar = pattern[index];
      if (escapedChar === undefined || !REGEXP_META_CHARS.has(escapedChar)) return null;
      decoded += escapedChar;
      continue;
    }

    if (REGEXP_META_CHARS.has(char)) return null;
    decoded += char;
  }

  return decoded;
}

/**
 * Returns true when `s` contains a replacement-string token (`$&`, `$'`,
 * `` $` ``, `$<`, or `$1`–`$9`) that would expand to something other than
 * the literal text that was matched. Such tokens make it impossible to
 * guarantee that the replacement emits the intended escaped string.
 */
function containsReplacementToken(s: string): boolean {
  for (let i = 0; i < s.length - 1; i++) {
    if (s[i] === "$") {
      const next = s[i + 1];
      if (next === "&" || next === "'" || next === "`" || next === "<" || (next >= "0" && next <= "9")) {
        return true;
      }
    }
  }
  return false;
}

function isLiteralRegexEscapeReplacement(search: string, replacement: string): boolean {
  if (search.length === 0 || replacement !== `\\${search}`) return false;
  if (!REGEXP_META_CHARS.has(search[0])) return false;
  if (containsReplacementToken(replacement)) return false;

  for (const char of search.slice(1)) {
    if (REGEXP_META_CHARS.has(char)) return false;
  }

  return true;
}

/**
 * Returns true when `node` is an identifier or member expression whose name
 * indicates the value has already been escaped, e.g. `ESCAPED_FOO` or
 * `escapedValue`. The name must start with "escaped" so that `unescapedValue`
 * is never treated as safe.
 */
function isEscapedNameReference(node: TSESTree.Node): boolean {
  if (node.type === AST_NODE_TYPES.Identifier) return ESCAPED_IDENT_PATTERN.test(node.name);
  if (node.type === AST_NODE_TYPES.MemberExpression && !node.computed && node.property.type === AST_NODE_TYPES.Identifier) {
    return ESCAPED_IDENT_PATTERN.test(node.property.name);
  }
  return false;
}

/**
 * Returns true when `node` is a literal value that can never introduce a regex
 * metacharacter — specifically a number or boolean literal, or a string literal
 * whose characters are all outside the regex metacharacter set.
 */
function isLiteralSafeForRegexp(node: TSESTree.Node): boolean {
  if (node.type !== AST_NODE_TYPES.Literal) return false;
  const { value } = node;
  if (typeof value === "number" || typeof value === "boolean") return true;
  if (typeof value === "string") {
    for (const char of value) {
      if (REGEXP_META_CHARS.has(char)) return false;
    }
    return true;
  }
  return false;
}

type ScopeType = ReturnType<TSESLint.SourceCode["getScope"]>;

/**
 * Given an `Identifier` reference, walks the scope chain to find the variable.
 * If the variable is bound by a single `const` declarator that has no write
 * references other than the initialization, returns the initializer expression.
 * Returns null for reassigned bindings, `let`/`var` declarations, parameters,
 * destructuring, and any other shape we cannot statically resolve.
 */
function findConstInitializer(sourceCode: Readonly<TSESLint.SourceCode>, node: TSESTree.Identifier): TSESTree.Expression | null {
  let scope: ScopeType | null = sourceCode.getScope(node);
  let variable: ScopeType["variables"][number] | undefined;
  while (scope) {
    variable = scope.set.get(node.name);
    if (variable) break;
    scope = scope.upper;
  }
  if (!variable) return null;

  // Must have exactly one definition
  if (variable.defs.length !== 1) return null;
  const def = variable.defs[0];

  // Must be a variable declarator (not a parameter, catch clause, import, etc.)
  if (def.type !== "Variable") return null;

  // def.node is VariableDeclarator, def.parent is VariableDeclaration for Variable defs
  const declarator = def.node as TSESTree.VariableDeclarator;
  const declaration = def.parent as TSESTree.VariableDeclaration;

  // Only const declarations — let/var can be reassigned elsewhere
  if (declaration.kind !== "const") return null;

  // Must have an initializer
  if (!declarator.init) return null;

  // Reject any write that is not the initialization (guards against const in loops, etc.)
  if (variable.references.some(ref => ref.isWrite() && !ref.init)) return null;

  return declarator.init;
}

/**
 * Returns true when the interpolated expression is recognized as already
 * escaped — via a named regex-escape helper call, the standard inline
 * `.replace()` form, a variable name that starts with "escaped", or a
 * `const` identifier whose initializer is itself recognized as escaped or
 * is a literal value that can never contain regex metacharacters. A `const`
 * string with an explicit regex-fragment name suffix is also accepted.
 */
function isRecognizedAsEscaped(node: TSESTree.Node, sourceCode?: Readonly<TSESLint.SourceCode>): boolean {
  if (isEscapeHelperCall(node) || isRegexEscapeReplaceCall(node) || isEscapedNameReference(node)) return true;

  // One level of const-binding resolution: look up the variable's initializer
  // and check whether it is itself safe.
  if (sourceCode && node.type === AST_NODE_TYPES.Identifier) {
    const init = findConstInitializer(sourceCode, node);
    if (init !== null) {
      if (isEscapeHelperCall(init) || isRegexEscapeReplaceCall(init) || isLiteralSafeForRegexp(init)) return true;
      if (typeof getStringLiteralValue(init) === "string" && REGEXP_FRAGMENT_IDENT_PATTERN.test(node.name)) return true;
    }
  }

  return false;
}

export const requireEscapedRegexpInterpolationRule = createRule({
  name: "require-escaped-regexp-interpolation",
  meta: {
    type: "problem",
    docs: {
      description:
        "Require values interpolated into a `new RegExp()` template-literal pattern to be escaped or explicitly named regex fragments. " +
        "Unescaped interpolation of a value containing regex metacharacters (e.g. `.`, `*`, `+`, `(`, `)`) can produce unintended matches " +
        "or, with attacker-controlled input, a ReDoS-prone pattern.",
    },
    schema: [],
    messages: {
      unescapedInterpolation:
        "Interpolated value `{{expr}}` in `new RegExp()` template literal is not passed through a regex-escaping helper. " + 'Escape regex metacharacters before interpolating, e.g. `{{expr}}.replace(/[.*+?^${}()|[\\]\\\\]/g, "\\\\$&")`.',
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;

    return {
      NewExpression(node) {
        if (node.callee.type !== AST_NODE_TYPES.Identifier || node.callee.name !== "RegExp") return;

        const patternArg = node.arguments[0];
        if (!patternArg || patternArg.type !== AST_NODE_TYPES.TemplateLiteral) return;
        if (patternArg.expressions.length === 0) return;

        for (const expr of patternArg.expressions) {
          if (isRecognizedAsEscaped(expr, sourceCode)) continue;

          context.report({
            node: expr,
            messageId: "unescapedInterpolation",
            data: { expr: sourceCode.getText(expr) },
          });
        }
      },
    };
  },
});
