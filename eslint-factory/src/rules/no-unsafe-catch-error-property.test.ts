import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { noUnsafeCatchErrorPropertyRule } from "./no-unsafe-catch-error-property";

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

describe("no-unsafe-catch-error-property", () => {
  it("valid: bare catch {} without binding is ignored", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [
        `try { f(); } catch { }`,
        // Destructuring binding is also ignored
        `try { f(); } catch ({ message }) { console.log(message); }`,
      ],
      invalid: [],
    });
  });

  it("valid: getErrorMessage guard suppresses all warnings in the catch block", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [`try { f(); } catch (err) { core.setFailed(getErrorMessage(err)); }`, `try { f(); } catch (err) { const msg = getErrorMessage(err); console.log(err.message); }`],
      invalid: [],
    });
  });

  it("valid: instanceof Error guard suppresses all warnings in the catch block", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [`try { f(); } catch (err) { core.setFailed(err instanceof Error ? err.message : String(err)); }`, `try { f(); } catch (err) { if (err instanceof Error) { console.log(err.stack); } }`],
      invalid: [],
    });
  });

  it("valid: nested guard block that dominates access is allowed", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [`try { f(); } catch (err) { if (flag) { if (err instanceof Error) { console.log(err.stack); } } }`],
      invalid: [],
    });
  });

  it("valid: early-exit instanceof guard (negated) before access is recognized", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [
        // if (!(err instanceof Error)) return;  err.stack;
        `try { f(); } catch (err) { if (!(err instanceof Error)) return; console.log(err.stack); }`,
        // if (!(err instanceof Error)) { return; }  err.stack;
        `try { f(); } catch (err) { if (!(err instanceof Error)) { return; } console.log(err.stack); }`,
        // if (!(err instanceof Error)) throw new TypeError();  err.stack;
        `try { f(); } catch (err) { if (!(err instanceof Error)) throw new TypeError("not an error"); console.log(err.stack); }`,
        // if (err instanceof Error) {} else return;  err.stack;
        `try { f(); } catch (err) { if (err instanceof Error) { doSomething(); } else return; console.log(err.stack); }`,
        // if (err instanceof Error) {} else { return; }  err.stack;
        `try { f(); } catch (err) { if (err instanceof Error) { } else { return; } console.log(err.stack); }`,
        // multiple safe properties after a single early-exit guard
        `try { f(); } catch (err) { if (!(err instanceof Error)) return; console.log(err.message, err.stack); }`,
      ],
      invalid: [],
    });
  });

  it("invalid: early-exit guard without termination does not suppress access", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          // non-matching path does not terminate — access is still unsafe
          code: `try { f(); } catch (err) { if (!(err instanceof Error)) console.log("not an error"); console.log(err.stack); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "stack", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "stack" },
                  output: `try { f(); } catch (err) { if (!(err instanceof Error)) console.log("not an error"); console.log((err instanceof Error ? err.stack : undefined)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("valid: property access on a different variable is not flagged", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [`try { f(); } catch (err) { console.log(otherObj.message); }`, `try { f(); } catch (err) { const e = new Error(); console.log(e.message); }`],
      invalid: [],
    });
  });

  it("valid: catch aliases guarded by instanceof or prior safe extraction are accepted", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [
        `try { f(); } catch (error) { const err = error; if (err instanceof Error) { console.log(err.message); } }`,
        `try { f(); } catch (error) { const err = error; if (!(err instanceof Error)) return; console.log(err.stack); }`,
        `try { f(); } catch (error) { const err = error; if (error instanceof Error) { console.log(err.message); } }`,
        `try { f(); } catch (error) { const err = error; getErrorMessage(err); console.log(err.message); }`,
        `try { f(); } catch (error) { const err = new Error(); console.log(err.message); }`,
      ],
      invalid: [],
    });
  });

  it("invalid: unguarded const aliases of catch parameters are flagged by alias name", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (error) { const err = error; console.log(err.message); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "message", errorVar: "err" },
              suggestions: [
                {
                  messageId: "useGetErrorMessage",
                  data: { errorVar: "err" },
                  output: `try { f(); } catch (error) { const err = error; console.log(getErrorMessage(err)); }`,
                },
              ],
            },
          ],
        },
        {
          code: `try { f(); } catch (err) { const e = /** @type {Error} */ err; console.log(e["stack"]); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "stack", errorVar: "e" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "e", prop: "stack" },
                  output: `try { f(); } catch (err) { const e = /** @type {Error} */ err; console.log((e instanceof Error ? e.stack : undefined)); }`,
                },
              ],
            },
          ],
        },
        {
          code: `try { f(); } catch (error) { const alias = error; error = new Error(); if (error instanceof Error) console.log(alias.message); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "message", errorVar: "alias" },
              suggestions: [
                {
                  messageId: "useGetErrorMessage",
                  data: { errorVar: "alias" },
                  output: `try { f(); } catch (error) { const alias = error; error = new Error(); if (error instanceof Error) console.log(getErrorMessage(alias)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("valid: dynamic computed property access on catch variable is not flagged", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [`try { f(); } catch (err) { const prop = "message"; console.log(err[prop]); }`],
      invalid: [],
    });
  });

  it('invalid: computed string-literal err["message"] is flagged same as err.message', () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { console.log(err["message"]); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "message", errorVar: "err" },
              suggestions: [
                {
                  messageId: "useGetErrorMessage",
                  data: { errorVar: "err" },
                  output: `try { f(); } catch (err) { console.log(getErrorMessage(err)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it('invalid: computed string-literal err["stack"] suggests instanceof guard', () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { console.log(err["stack"]); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "stack", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "stack" },
                  output: `try { f(); } catch (err) { console.log((err instanceof Error ? err.stack : undefined)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it('invalid: computed string-literal err["code"] suggests instanceof guard', () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { if (err["code"] === "ENOENT") { } }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "code", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "code" },
                  output: `try { f(); } catch (err) { if ((err instanceof Error ? err.code : undefined) === "ENOENT") { } }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: err.message without guard is flagged", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { core.setFailed(err.message); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "message", errorVar: "err" },
              suggestions: [
                {
                  messageId: "useGetErrorMessage",
                  data: { errorVar: "err" },
                  output: `try { f(); } catch (err) { core.setFailed(getErrorMessage(err)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: sibling-branch guard does not suppress unguarded access", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { if (flag) { core.info(err.message); } if (err instanceof Error) { core.info(err.stack); } }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "message", errorVar: "err" },
              suggestions: [
                {
                  messageId: "useGetErrorMessage",
                  data: { errorVar: "err" },
                  output: `try { f(); } catch (err) { if (flag) { core.info(getErrorMessage(err)); } if (err instanceof Error) { core.info(err.stack); } }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: guard after unsafe access does not suppress report", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { console.log(err.stack); if (err instanceof Error) { core.info(err.message); } }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "stack", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "stack" },
                  output: `try { f(); } catch (err) { console.log((err instanceof Error ? err.stack : undefined)); if (err instanceof Error) { core.info(err.message); } }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: err.stack without guard is flagged", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { console.log(err.stack); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "stack", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "stack" },
                  output: `try { f(); } catch (err) { console.log((err instanceof Error ? err.stack : undefined)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: err.code without guard is flagged", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { if (err.code === "ENOENT") { } }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "code", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "code" },
                  output: `try { f(); } catch (err) { if ((err instanceof Error ? err.code : undefined) === "ENOENT") { } }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: multiple unsafe property accesses in one catch block are all reported", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { console.log(err.message); console.log(err.stack); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "message", errorVar: "err" },
              suggestions: [
                {
                  messageId: "useGetErrorMessage",
                  data: { errorVar: "err" },
                  output: `try { f(); } catch (err) { console.log(getErrorMessage(err)); console.log(err.stack); }`,
                },
              ],
            },
            {
              messageId: "unsafeProperty",
              data: { prop: "stack", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "stack" },
                  output: `try { f(); } catch (err) { console.log(err.message); console.log((err instanceof Error ? err.stack : undefined)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: .message suggests getErrorMessage replacement", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { core.setFailed(err.message); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "message", errorVar: "err" },
              suggestions: [
                {
                  messageId: "useGetErrorMessage",
                  data: { errorVar: "err" },
                  output: `try { f(); } catch (err) { core.setFailed(getErrorMessage(err)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: .stack suggests instanceof Error guard", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { console.log(err.stack); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "stack", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "stack" },
                  output: `try { f(); } catch (err) { console.log((err instanceof Error ? err.stack : undefined)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: works with ES module syntax", () => {
    esmRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { fetch(url); } catch (e) { console.error(e.message); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "message", errorVar: "e" },
              suggestions: [
                {
                  messageId: "useGetErrorMessage",
                  data: { errorVar: "e" },
                  output: `try { fetch(url); } catch (e) { console.error(getErrorMessage(e)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: nested try/catch — each catch block is checked independently", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          // Inner catch has a guard; outer catch does not — outer should still be flagged
          code: `
try {
  f();
} catch (outer) {
  try {
    g();
  } catch (inner) {
    core.setFailed(getErrorMessage(inner));
  }
  core.setFailed(outer.message);
}`.trim(),
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "message", errorVar: "outer" },
              suggestions: [
                {
                  messageId: "useGetErrorMessage",
                  data: { errorVar: "outer" },
                  output: `try {\n  f();\n} catch (outer) {\n  try {\n    g();\n  } catch (inner) {\n    core.setFailed(getErrorMessage(inner));\n  }\n  core.setFailed(getErrorMessage(outer));\n}`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("valid: typeof err === 'object' with non-null guard suppresses warnings in the catch block", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [
        `try { f(); } catch (err) { if (typeof err === 'object' && err !== null) { console.log(err.status); } }`,
        `try { f(); } catch (err) { if ('object' === typeof err && null !== err) { console.log(err.status); } }`,
        `try { f(); } catch (err) { if (typeof err === 'object' && err != null) { console.log(err.status); } }`,
        `try { f(); } catch (err) { if (err && typeof err === 'object') { console.log(err.status); } }`,
        `try { f(); } catch (err) { if (!err) return; if (typeof err === 'object') { console.log(err.status); } }`,
      ],
      invalid: [],
    });
  });

  it("invalid: bare typeof err === 'object' guard is insufficient", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { if (typeof err === 'object') { console.log(err.status); } }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "status", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "status" },
                  output: `try { f(); } catch (err) { if (typeof err === 'object') { console.log((err instanceof Error ? err.status : undefined)); } }`,
                },
              ],
            },
          ],
        },
        {
          code: `try { f(); } catch (err) { if ('object' === typeof err) { console.log(err.status); } }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "status", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "status" },
                  output: `try { f(); } catch (err) { if ('object' === typeof err) { console.log((err instanceof Error ? err.status : undefined)); } }`,
                },
              ],
            },
          ],
        },
        {
          // Standalone err !== null in a separate if (without return) does not count as companion guard
          code: `try { f(); } catch (err) { if (err !== null) { } if (typeof err === 'object') { console.log(err.status); } }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "status", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "status" },
                  output: `try { f(); } catch (err) { if (err !== null) { } if (typeof err === 'object') { console.log((err instanceof Error ? err.status : undefined)); } }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: err.status without guard is flagged", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { if (err.status === 404) { } }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "status", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "status" },
                  output: `try { f(); } catch (err) { if ((err instanceof Error ? err.status : undefined) === 404) { } }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: err.cause without guard is flagged", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { console.log(err.cause); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "cause", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "cause" },
                  output: `try { f(); } catch (err) { console.log((err instanceof Error ? err.cause : undefined)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("invalid: err.name without guard is flagged", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { console.log(err.name); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "name", errorVar: "err" },
              suggestions: [
                {
                  messageId: "wrapWithInstanceof",
                  data: { errorVar: "err", prop: "name" },
                  output: `try { f(); } catch (err) { console.log((err instanceof Error ? err.name : undefined)); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("valid: typeof err === 'object' with truthy err guard suppresses .status access", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [`try { f(); } catch (err) { if (err && typeof err === 'object' && err.status === 404) { } }`],
      invalid: [],
    });
  });

  it("invalid: chained access on non-message prop is flagged but wrapWithInstanceof suggestion is suppressed", () => {
    cjsRuleTester.run("no-unsafe-catch-error-property", noUnsafeCatchErrorPropertyRule, {
      valid: [],
      invalid: [
        {
          code: `try { f(); } catch (err) { console.log(err.stack.length); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "stack", errorVar: "err" },
              suggestions: [],
            },
          ],
        },
        {
          code: `try { f(); } catch (err) { console.log(err.code.toString()); }`,
          errors: [
            {
              messageId: "unsafeProperty",
              data: { prop: "code", errorVar: "err" },
              suggestions: [],
            },
          ],
        },
      ],
    });
  });
});
