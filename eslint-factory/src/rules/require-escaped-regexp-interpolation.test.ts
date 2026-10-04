import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { requireEscapedRegexpInterpolationRule } from "./require-escaped-regexp-interpolation";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("require-escaped-regexp-interpolation", () => {
  it("uses the correct docs URL", () => {
    expect(requireEscapedRegexpInterpolationRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#require-escaped-regexp-interpolation");
  });

  it("valid: non-interpolated RegExp patterns are accepted", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: ['new RegExp("^[a-z]+$");', "new RegExp(`^[a-z]+$`);", "new RegExp(somePattern);", "new RegExp(somePattern, 'g');"],
      invalid: [],
    });
  });

  it("valid: interpolated value already passed through an escape helper is accepted", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [
        "new RegExp(`^${escapeRegExp(varName)}$`);",
        "new RegExp(`(^|[-_\\\\s])${escapeRegex(qualifier)}($|[-_\\\\s])`);",
        "new RegExp(`\\\\$\\\\{${utils.escapeRegExp(varName)}\\\\}`, 'g');",
        "new RegExp(`^${ESCAPED_NAME}$`);",
        "new RegExp(`^${escapedValue}$`);",
      ],
      invalid: [],
    });
  });

  it("valid: const variable assigned from escape-helper call (name not starting with 'escaped')", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [
        "const regexPattern = escapeRegExpChars(pattern); new RegExp(`^${regexPattern}$`);",
        "const safeStr = utils.escapeRegex(input); new RegExp(`prefix-${safeStr}-suffix`);",
        'const normalized = value.replace(/[.*+?^${}()|[\\]\\\\]/g, "\\\\$&"); new RegExp(`^${normalized}$`);',
      ],
      invalid: [],
    });
  });

  it("valid: const string literal with no metacharacters is accepted when interpolated", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: ['const TAG = "gh-aw-comment-memory"; new RegExp(`${TAG}:([^\\\\n]+)`);', 'const PREFIX = "v"; new RegExp(`^${PREFIX}\\\\d+`);'],
      invalid: [],
    });
  });

  it("valid: const strings with explicit regex-fragment name suffixes are accepted", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [
        'const PATH_CHAR = "[^/]"; new RegExp(`^${PATH_CHAR}$`);',
        'const ALTERNATIVES_RE = "(?:a|b)"; new RegExp(`${ALTERNATIVES_RE}`);',
        'const HOST_RE_SOURCE = "(?:localhost|example\\\\.com)"; new RegExp(`^${HOST_RE_SOURCE}$`);',
        'const URL_DELIMITERS = "(?:^|[\\\\s,])"; new RegExp(`${URL_DELIMITERS}`);',
        'const TOKEN_PATTERN = "(?:a|b)"; new RegExp(`${TOKEN_PATTERN}`);',
      ],
      invalid: [],
    });
  });

  it("valid: const numeric literal is accepted when interpolated", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: ["const MAX_LENGTH = 128; new RegExp(`([^\\\\n]{1,${MAX_LENGTH}})`);", "const COUNT = 3; new RegExp(`a{${COUNT}}`);"],
      invalid: [],
    });
  });

  it('valid: standard inline .replace(…, "\\\\$&") escape form is accepted', () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: ['new RegExp(`^${varName.replace(/[.*+?^${}()|[\\]\\\\]/g, "\\\\$&")}$`);', 'new RegExp(`^${qualifier.replace(/[.*+?^${}()|[\\]\\\\]/g, "\\\\$&")}($|[-_\\\\s])`);'],
      invalid: [],
    });
  });

  it("valid: targeted literal .replace() escape forms are accepted", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: ['new RegExp(`^${varName.replace(".", "\\\\.")}$`);', 'new RegExp(`^${varName.replace(/\\./g, "\\\\.")}$`);'],
      invalid: [],
    });
  });

  it("valid: unrelated `new` calls to other constructors are not flagged", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: ["new Foo(`^${bar}$`);", "new Date(`${year}-01-01`);"],
      invalid: [],
    });
  });

  it("invalid: interpolated loop variable in RegExp pattern without escaping", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "const pattern = new RegExp(`\\\\$\\\\{${varName}\\\\}`, 'g');",
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: interpolated function parameter in RegExp pattern without escaping", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "function hasQualifier(name, qualifier) { return new RegExp(`(^|[-_\\\\s])${qualifier}($|[-_\\\\s])`).test(name); }",
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: raw user-controlled strings with regex metacharacters are flagged", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "function search(userInput) { return new RegExp(`^${userInput}$`); }",
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: multiple unescaped interpolations are each reported", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "new RegExp(`${prefix}-${suffix}`);",
          errors: [{ messageId: "unescapedInterpolation" }, { messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: mixed escaped and unescaped interpolations only report the unescaped one", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "new RegExp(`${escapeRegExp(prefix)}-${suffix}`);",
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: identifier named unescapedValue is not treated as safe", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "new RegExp(`^${unescapedValue}$`);",
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: escapeHtml call is not treated as a regex-escape helper", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "new RegExp(`^${escapeHtml(userInput)}$`);",
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: arbitrary .replace() calls are not treated as regex escaping", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: 'new RegExp(`^${varName.replace(".", ".")}$`);',
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it('invalid: .replace() with "\\\\$&" but non-canonical search pattern is not treated as an escape', () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: 'new RegExp(`^${varName.replace(/./, "\\\\$&")}$`);',
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it('invalid: .replace() with "\\\\$&" and sticky-flag regex is not treated as an escape', () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: 'new RegExp(`^${varName.replace(/[.*+?^${}()|[\\]\\\\]/y, "\\\\$&")}$`);',
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: .replace() with a $' replacement token is not treated as an escape", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: 'new RegExp(`^${varName.replace("$\'", "\\\\$\'")}$`);',
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: let variable (even with escape-helper initializer) is flagged", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "let regexPattern = escapeRegExpChars(pattern); new RegExp(`^${regexPattern}$`);",
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: reassigned const variable is flagged even when initializer is an escape-helper call", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "const regexPattern = escapeRegExpChars(pattern); regexPattern = 'overwritten'; new RegExp(`^${regexPattern}$`);",
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: const string literal containing regex metacharacters is flagged", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: 'const PATTERN = "file.*txt"; new RegExp(`^${PATTERN}$`);',
          errors: [{ messageId: "unescapedInterpolation" }],
        },
        {
          code: 'const SUFFIX = ".md"; new RegExp(`${SUFFIX}$`);',
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });

  it("invalid: const variable assigned from a non-escape function is flagged", () => {
    cjsRuleTester.run("require-escaped-regexp-interpolation", requireEscapedRegexpInterpolationRule, {
      valid: [],
      invalid: [
        {
          code: "const processed = someOtherFunction(pattern); new RegExp(`^${processed}$`);",
          errors: [{ messageId: "unescapedInterpolation" }],
        },
      ],
    });
  });
});
