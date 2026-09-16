import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireNewRegExpTryCatchRule } from "./require-new-regexp-try-catch";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

const esmRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "module",
  },
});

describe("require-new-regexp-try-catch", () => {
  it("valid: static pattern sources are always safe (CommonJS)", () => {
    cjsRuleTester.run("require-new-regexp-try-catch", requireNewRegExpTryCatchRule, {
      valid: [
        `const re = new RegExp("^foo$");`,
        `const re = new RegExp(\`^foo$\`);`,
        `const re = new RegExp(/^foo$/);`,
        `const re = new RegExp(\`^\${escapedName}$\`, "i");`,
        `const re = new RegExp(FOO_RE.source, "g");`,
        `const re = new RegExp(patternSources.join("|"), "i");`,
      ],
      invalid: [],
    });
  });

  it("valid: new RegExp(identifier) inside try block passes (CommonJS)", () => {
    cjsRuleTester.run("require-new-regexp-try-catch", requireNewRegExpTryCatchRule, {
      valid: [
        `try { const re = new RegExp(pattern); } catch (e) {}`,
        `try { return new RegExp(pattern); } catch (e) {}`,
        `function f() { try { new RegExp(validation.pattern); } catch (e) {} }`,
      ],
      invalid: [],
    });
  });

  it("valid: RegExp shadowed by a local binding is not the global constructor (CommonJS)", () => {
    cjsRuleTester.run("require-new-regexp-try-catch", requireNewRegExpTryCatchRule, {
      valid: [
        `function parse(RegExp, value) { return new RegExp(value); }`,
        `const RegExp = require("./my-regexp"); const re = new RegExp(pattern);`,
      ],
      invalid: [],
    });
  });

  it("invalid: new RegExp(identifier) with no try/catch is flagged (CommonJS)", () => {
    cjsRuleTester.run("require-new-regexp-try-catch", requireNewRegExpTryCatchRule, {
      valid: [],
      invalid: [
        {
          // const declarations are not auto-wrappable (the binding would escape the try block),
          // so the rule reports without a suggestion here.
          code: `const regex = new RegExp(validation.pattern);`,
          errors: [{ messageId: "requireTryCatch", suggestions: [] }],
        },
      ],
    });
  });

  it("invalid: new RegExp(identifier) with no try/catch is flagged (ES module)", () => {
    esmRuleTester.run("require-new-regexp-try-catch", requireNewRegExpTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const regex = new RegExp(userPattern);`,
          errors: [{ messageId: "requireTryCatch", suggestions: [] }],
        },
      ],
    });
  });

  it("invalid: new RegExp inside setTimeout callback is not protected by outer try (CommonJS)", () => {
    cjsRuleTester.run("require-new-regexp-try-catch", requireNewRegExpTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `try { setTimeout(() => { new RegExp(pattern); }, 0); } catch(e) {}`,
          errors: [
            {
              messageId: "requireTryCatch",
              suggestions: [
                {
                  messageId: "wrapInTryCatch",
                  output: `try { setTimeout(() => { try {\n  new RegExp(pattern);\n} catch (err) {\n  // TODO: handle invalid pattern for this new RegExp(...) call.\n  throw new Error(\n    "RegExp constructor call failed: " + (err instanceof Error ? err.message : String(err)),\n    { cause: err },\n  );\n} }, 0); } catch(e) {}`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: new RegExp(member expression) that is not .source is flagged (CommonJS)", () => {
    cjsRuleTester.run("require-new-regexp-try-catch", requireNewRegExpTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const regex = new RegExp(options.pattern, options.flags);`,
          errors: [{ messageId: "requireTryCatch", suggestions: [] }],
        },
      ],
    });
  });

  it("invalid: new RegExp(identifier) in expression statement produces a try/catch suggestion (CommonJS)", () => {
    cjsRuleTester.run("require-new-regexp-try-catch", requireNewRegExpTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `new RegExp(validation.pattern);`,
          errors: [
            {
              messageId: "requireTryCatch",
              suggestions: [
                {
                  messageId: "wrapInTryCatch",
                  output: `try {\n  new RegExp(validation.pattern);\n} catch (err) {\n  // TODO: handle invalid pattern for this new RegExp(...) call.\n  throw new Error(\n    "RegExp constructor call failed: " + (err instanceof Error ? err.message : String(err)),\n    { cause: err },\n  );\n}`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: new RegExp(identifier) in arrow-expression body has no wrappable ancestor — no suggestion emitted (CommonJS)", () => {
    cjsRuleTester.run("require-new-regexp-try-catch", requireNewRegExpTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const f = () => new RegExp(pattern);`,
          errors: [{ messageId: "requireTryCatch", suggestions: [] }],
        },
      ],
    });
  });
});
