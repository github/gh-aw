import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { requireGetExecOutputExitCodeCheckRule } from "./require-getexecoutput-exitcode-check";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("require-getexecoutput-exitcode-check", () => {
  it("uses the correct docs URL", () => {
    expect(requireGetExecOutputExitCodeCheckRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#require-getexecoutput-exitcode-check");
  });

  it("valid", () => {
    cjsRuleTester.run("require-getexecoutput-exitcode-check", requireGetExecOutputExitCodeCheckRule, {
      valid: [
        // destructuring includes exitCode
        `async function f() { const { stdout, exitCode } = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); if (exitCode !== 0) throw new Error("failed"); }`,
        // only exitCode destructured
        `async function f() { const { exitCode } = await execApi.getExecOutput("git", ["status"], { ignoreReturnCode: true }); }`,
        // identifier binding, .exitCode read later
        `async function f() { const result = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); if (result.exitCode !== 0) throw new Error("failed"); }`,
        // no ignoreReturnCode option at all — default throw-on-failure behavior applies
        `async function f() { const { stdout } = await exec.getExecOutput("git", ["status"]); }`,
        // ignoreReturnCode explicitly false — default throw-on-failure still applies
        `async function f() { const { stdout } = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: false }); }`,
        // rest element could capture exitCode; don't flag
        `async function f() { const { ...rest } = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); console.log(rest.exitCode); }`,
        // options composed only from a spread can't be statically resolved — out of scope
        `async function f() { const { stdout } = await exec.getExecOutput("git", ["status"], { ...opts }); }`,
        // trailing spread may override ignoreReturnCode with an unresolvable value — out of scope
        `async function f() { const { stdout } = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true, ...opts }); }`,
        // options passed as an identifier that isn't a resolvable object literal (e.g. a
        // function parameter) can't be statically inspected
        `async function f(options) { const { stdout } = await exec.getExecOutput("git", ["status"], options); }`,
        // options passed as an identifier initialized from another identifier can't be
        // statically inspected
        `async function f(other) { const options = other; const { stdout } = await exec.getExecOutput("git", ["status"], options); }`,
        // options passed as a reassigned identifier can't be trusted to still hold the
        // initializer's shape at the call site
        `async function f() { let opts = { ignoreReturnCode: true }; opts = {}; const { stdout } = await exec.getExecOutput("git", ["status"], opts); }`,
        // options passed as an identifier resolving to a local object literal, but exitCode
        // is read — no violation
        `async function f() { const opts = { ignoreReturnCode: true }; const { exitCode } = await exec.getExecOutput("git", ["status"], opts); }`,
        // ternary of two non-literal option values can't be statically inspected
        `async function f(a, b) { const opts = Boolean(a) ? a : b; const { stdout } = await exec.getExecOutput("git", ["status"], opts); }`,
        // explicit ignoreReturnCode: true after a spread is resolvable; exitCode is directly accessed
        `async function f() { const r = (await exec.getExecOutput("git", ["status"], { ...opts, ignoreReturnCode: true })).exitCode; }`,
        // direct member access on the awaited result
        `async function f() { const code = (await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true })).exitCode; }`,
        // computed static member access on the awaited result
        `async function f() { const code = (await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }))["exitCode"]; }`,
        // identifier binding with computed static member access
        `async function f() { const result = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); if (result["exitCode"] !== 0) throw new Error("failed"); }`,
        // reassignment to an existing identifier binding, checked later
        `async function f() { let result; result = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); if (result.exitCode !== 0) throw new Error("failed"); }`,
        // returned via helper binding; caller checks exitCode
        `async function f() { const run = async () => { const result = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); return result; }; const out = await run(); if (out.exitCode !== 0) throw new Error("failed"); }`,
        // returned via implicit arrow callback passed to a helper; caller checks exitCode
        `async function withToken(cb) { return await cb(); } async function f() { const out = await withToken(async () => exec.getExecOutput("git", ["status"], { ignoreReturnCode: true })); if (out.exitCode !== 0) throw new Error("failed"); }`,
        // destructuring-assignment (not a declaration) includes exitCode
        `let stdout, exitCode; async function f() { ({ stdout, exitCode } = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true })); }`,
        // destructuring-assignment with only exitCode
        `let exitCode; async function f() { ({ exitCode } = await execApi.getExecOutput("git", ["status"], { ignoreReturnCode: true })); }`,
        // destructuring-assignment with a rest element that could capture exitCode
        `let rest; async function f() { ({ ...rest } = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true })); console.log(rest.exitCode); }`,
        // exec() variant: no ignoreReturnCode option — default throw-on-failure applies
        `async function f() { await exec.exec("git", ["status"]); }`,
        // exec() variant: captured and compared against 0 (mirrors run_validate_workflows.cjs)
        `async function f() { const exitCode = await exec.exec("git", ["status"], { ignoreReturnCode: true }); if (exitCode !== 0) throw new Error("failed"); }`,
        // exec() variant: reassigned identifier, checked later
        `async function f() { let exitCode; exitCode = await exec.exec("git", ["status"], { ignoreReturnCode: true }); if (exitCode !== 0) throw new Error("failed"); }`,
        // exec() variant: value consumed directly in a comparison
        `async function f() { if ((await exec.exec("git", ["status"], { ignoreReturnCode: true })) !== 0) throw new Error("failed"); }`,
        // exec() variant: returned directly from the function
        `async function f() { return await exec.exec("git", ["status"], { ignoreReturnCode: true }); }`,
      ],
      invalid: [],
    });
  });

  it("invalid: ignoreReturnCode: true but exitCode never read", () => {
    cjsRuleTester.run("require-getexecoutput-exitcode-check", requireGetExecOutputExitCodeCheckRule, {
      valid: [],
      invalid: [
        {
          code: `async function f() { const { stdout } = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          code: `async function f() { const { stdout, stderr } = await execApi.getExecOutput("git", ["bundle", "verify", "b"], { ignoreReturnCode: true, silent: true }); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          code: `async function f() { const result = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); return result.stdout.trim(); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          code: `async function f() { await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          code: `async function f() { return await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          // explicit ignoreReturnCode: true overrides whatever the preceding spread carried
          code: `async function f() { const { stdout } = await exec.getExecOutput("git", ["status"], { ...baseGitOpts, ignoreReturnCode: true }); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          code: `async function f() { let result; result = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true }); return result.stdout.trim(); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          // destructuring-assignment with exitCode omitted
          code: `let stdout; async function f() { ({ stdout } = await exec.getExecOutput("git", ["status"], { ignoreReturnCode: true })); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          // options passed as an identifier resolving to a local object literal with
          // ignoreReturnCode: true
          code: `async function f() { const opts = { ignoreReturnCode: true }; const { stdout } = await exec.getExecOutput("git", ["status"], opts); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          // same, but the object literal builds on a spread with an explicit override after it
          code: `async function f() { const opts = { ...base, ignoreReturnCode: true }; const { stdout } = await exec.getExecOutput("git", ["status"], opts); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          // mirrors actions/setup/js/git_helpers.cjs: both ternary branches agree on true
          code: `async function f(gitOpts) { const shallowOpts = gitOpts !== undefined ? { ...gitOpts, ignoreReturnCode: true } : { ignoreReturnCode: true }; const { stdout } = await execApi.getExecOutput("git", ["rev-parse", "--is-shallow-repository"], shallowOpts); }`,
          errors: [{ messageId: "missingExitCodeCheck" }],
        },
        {
          // mirrors actions/setup/js/check_workflow_recompile_needed.cjs: bare await, return value discarded
          code: `async function f() { let diffOutput = ""; await exec.exec("git", ["diff", "--exit-code", "."], { ignoreReturnCode: true, listeners: { stdout: d => { diffOutput += d.toString(); } } }); return diffOutput.trim().length > 0; }`,
          errors: [{ messageId: "missingExecExitCodeCheck" }],
        },
        {
          // exec() variant: captured but never read
          code: `async function f() { const exitCode = await exec.exec("git", ["status"], { ignoreReturnCode: true }); }`,
          errors: [{ messageId: "missingExecExitCodeCheck" }],
        },
        {
          // exec() variant: reassigned identifier, never read afterward
          code: `async function f() { let exitCode; exitCode = await exec.exec("git", ["status"], { ignoreReturnCode: true }); }`,
          errors: [{ messageId: "missingExecExitCodeCheck" }],
        },
      ],
    });
  });
});
