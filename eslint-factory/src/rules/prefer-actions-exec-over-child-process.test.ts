import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { preferActionsExecOverChildProcessRule } from "./prefer-actions-exec-over-child-process";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: "latest",
    sourceType: "commonjs",
  },
});

const esmRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: "latest",
    sourceType: "module",
  },
});

/** Marks a module as running under actions/github-script, where the `exec` toolkit global exists. */
const GH_SCRIPT = `/// <reference types="@actions/github-script" />\n`;

/** Prefixes each snippet with the github-script marker the rule requires to activate. */
const ghScript = (code: string) => `${GH_SCRIPT}${code}`;

describe("prefer-actions-exec-over-child-process", () => {
  it("flags child_process output-capturing calls (CommonJS, destructured)", () => {
    cjsRuleTester.run("prefer-actions-exec-over-child-process", preferActionsExecOverChildProcessRule, {
      valid: [
        // @actions/exec is already used
        { code: ghScript(`async function f() { await exec.getExecOutput("git", ["status"]); }`) },
        { code: ghScript(`async function f() { await exec.exec("git", ["status"]); }`) },
        // spawn / spawnSync are out of scope (no @actions/exec equivalent)
        { code: ghScript(`const { spawn } = require("child_process"); spawn("node", ["server.js"]);`) },
        { code: ghScript(`const { spawnSync } = require("child_process"); spawnSync("git", ["status"]);`) },
        { code: ghScript(`const cp = require("child_process"); cp.spawn("node", ["server.js"]);`) },
        { code: ghScript(`const cp = require("child_process"); cp.spawnSync("git", ["status"]);`) },
        // Same method name from an unrelated module — should not be flagged
        { code: ghScript(`const { execSync } = require("some-other-lib"); execSync("git status");`) },
        { code: ghScript(`const { exec } = require("./local-exec-helper.cjs"); exec("git status");`) },
        // Bare identifier without any require — should not be flagged
        { code: ghScript(`execSync("git status");`) },
        // Modules without the github-script marker have no `exec` global available
        { code: `const { execSync } = require("child_process"); execSync("git status");` },
        { code: `const { execFileSync } = require("child_process"); execFileSync("git", ["status"]);` },
        // exec() / execFile() calls that retain the ChildProcess handle for streaming or lifecycle control
        { code: ghScript(`const { execFile } = require("child_process"); const child = execFile("git", ["status"], cb); child.stdin.end();`) },
        { code: ghScript(`const { exec } = require("child_process"); const child = exec("git status"); child.kill();`) },
        { code: ghScript(`const { execFile } = require("child_process"); execFile("git", ["status"]).stdout.pipe(process.stdout);`) },
        { code: ghScript(`const cp = require("child_process"); function f() { return cp.exec("git status"); }`) },
        // Retained handles nested inside value-preserving wrappers/containers
        { code: ghScript(`const { exec } = require("child_process"); async function f() { const child = await exec("git status"); child.kill(); }`) },
        { code: ghScript(`const { exec } = require("child_process"); const children = [exec("git status")]; children[0].kill();`) },
        { code: ghScript(`const { exec } = require("child_process"); const holder = { child: exec("git status") }; holder.child.kill();`) },
      ],
      invalid: [
        {
          code: ghScript(`const { execSync } = require("child_process"); execSync("git status");`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
        {
          code: ghScript(`const { exec } = require("child_process"); exec("git status", cb);`),
          errors: [{ messageId: "preferActionsExec", data: { method: "exec" } }],
        },
        {
          code: ghScript(`const { execFile } = require("child_process"); execFile("git", ["status"], cb);`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execFile" } }],
        },
        {
          code: ghScript(`const { exec } = require("child_process"); async function f() { await exec("git", args, cb); }`),
          errors: [{ messageId: "preferActionsExec", data: { method: "exec" } }],
        },
        {
          code: ghScript(`const { exec } = require("child_process"); void exec("git", args, cb);`),
          errors: [{ messageId: "preferActionsExec", data: { method: "exec" } }],
        },
        {
          code: ghScript(`const { exec } = require("child_process"); exec("git", args, cb) || onError();`),
          errors: [{ messageId: "preferActionsExec", data: { method: "exec" } }],
        },
        {
          code: ghScript(`const { execFileSync } = require("child_process"); execFileSync("git", ["status"]);`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execFileSync" } }],
        },
        // execSync / execFileSync return output, so retaining the result is still flagged
        {
          code: ghScript(`const { execSync } = require("child_process"); const out = execSync("git status");`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
        {
          code: ghScript(`const cp = require("child_process"); cp.execSync("git status");`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
        {
          code: ghScript(`const cp = require("node:child_process"); cp.execFileSync("git", ["status"]);`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execFileSync" } }],
        },
        {
          code: ghScript(`require("child_process").execSync("git status");`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
        {
          code: ghScript(`const run = require("child_process").execSync; run("git status");`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
        {
          code: ghScript(`function f() { const { execSync } = require("child_process"); execSync("git status"); }`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
      ],
    });
  });

  it("flags a distinct message variant when migration requires converting a sync function (and its callers) to async", () => {
    cjsRuleTester.run("prefer-actions-exec-over-child-process", preferActionsExecOverChildProcessRule, {
      valid: [],
      invalid: [
        // Bare statement for side effects only — no return-value dependency, so plain message unchanged
        {
          code: ghScript(`const { execSync } = require("child_process"); function f() { execSync("git status"); }`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
        // Already-async enclosing function — even though the result is returned, the enclosing
        // function is already async, so no caller-chain conversion is needed; message unchanged
        {
          code: ghScript(`const { execSync } = require("child_process"); async function f() { return execSync("git status"); }`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
        // Non-async function whose return value is directly the call's result
        {
          code: ghScript(`const { execSync } = require("child_process"); function f() { return execSync("git status"); }`),
          errors: [{ messageId: "preferActionsExecSyncContext", data: { method: "execSync" } }],
        },
        // Non-async function where the call's result is assigned and then consumed
        {
          code: ghScript(`const { execSync } = require("child_process"); function f() { const out = execSync("git status"); return out.trim(); }`),
          errors: [{ messageId: "preferActionsExecSyncContext", data: { method: "execSync" } }],
        },
        // Also applies to arrow functions
        {
          code: ghScript(`const { execSync } = require("child_process"); const f = () => execSync("git status");`),
          errors: [{ messageId: "preferActionsExecSyncContext", data: { method: "execSync" } }],
        },
      ],
    });
  });

  it("flags child_process output-capturing calls (ES module)", () => {
    esmRuleTester.run("prefer-actions-exec-over-child-process", preferActionsExecOverChildProcessRule, {
      valid: [
        { code: ghScript(`import { spawn } from "child_process"; spawn("node", ["server.js"]);`) },
        { code: ghScript(`import { spawnSync } from "node:child_process"; spawnSync("git", ["status"]);`) },
        { code: ghScript(`import * as cp from "child_process"; cp.spawn("node", ["server.js"]);`) },
        { code: ghScript(`import childProcess from "child_process"; childProcess.spawn("node", ["server.js"]);`) },
        // No github-script marker — the `exec` global is not available in standalone Node modules
        { code: `import { execSync } from "child_process"; execSync("git status");` },
        // Retained ChildProcess handle
        { code: ghScript(`import { execFile } from "child_process"; const child = execFile("git", ["status"], cb); child.stdin.end();`) },
      ],
      invalid: [
        {
          code: ghScript(`import { execSync } from "child_process"; execSync("git status");`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
        {
          code: ghScript(`import { exec } from "child_process"; exec("git status", cb);`),
          errors: [{ messageId: "preferActionsExec", data: { method: "exec" } }],
        },
        {
          code: ghScript(`import { execFile } from "node:child_process"; execFile("git", ["status"], cb);`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execFile" } }],
        },
        {
          code: ghScript(`import { execFileSync } from "node:child_process"; execFileSync("git", ["status"]);`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execFileSync" } }],
        },
        {
          code: ghScript(`import * as cp from "child_process"; cp.execSync("git status");`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
        {
          code: ghScript(`import childProcess from "child_process"; childProcess.execFileSync("git", ["status"]);`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execFileSync" } }],
        },
        {
          code: ghScript(`import * as cp from "node:child_process"; const run = cp.execSync; run("git status");`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execSync" } }],
        },
      ],
    });
  });

  it("flags promisify()-wrapped child_process bindings", () => {
    cjsRuleTester.run("prefer-actions-exec-over-child-process", preferActionsExecOverChildProcessRule, {
      valid: [
        // spawn is out of scope, even when promisified
        {
          code: ghScript(`const { promisify } = require("util"); const { spawn } = require("child_process"); const spawnAsync = promisify(spawn); spawnAsync("node", ["server.js"]);`),
        },
        // promisify() of an unrelated module's method
        { code: ghScript(`const { promisify } = require("util"); const { exec } = require("some-other-lib"); const execAsync = promisify(exec); execAsync("git status");`) },
        // promisify() of a non-child_process function
        { code: ghScript(`const { promisify } = require("util"); const fs = require("fs"); const readFile = promisify(fs.readFile); readFile("f.txt");`) },
        // No github-script marker
        { code: `const { promisify } = require("util"); const { exec } = require("child_process"); const execAsync = promisify(exec); execAsync("git status");` },
        // Self-referential binding must not loop or resolve
        { code: ghScript(`const { promisify } = require("util"); let execAsync = promisify(execAsync); execAsync("git status");`) },
      ],
      invalid: [
        {
          code: ghScript(`const { promisify } = require("util"); const { exec } = require("child_process"); const execAsync = promisify(exec); async function f() { const { stdout } = await execAsync("git status"); }`),
          errors: [{ messageId: "preferActionsExec", data: { method: "exec" } }],
        },
        {
          code: ghScript(`const { promisify } = require("util"); const execAsync = promisify(require("child_process").exec); async function f() { await execAsync("git status"); }`),
          errors: [{ messageId: "preferActionsExec", data: { method: "exec" } }],
        },
        {
          code: ghScript(`const util = require("util"); const cp = require("child_process"); const execFileAsync = util.promisify(cp.execFile); async function f() { await execFileAsync("git", ["status"]); }`),
          errors: [{ messageId: "preferActionsExec", data: { method: "execFile" } }],
        },
      ],
    });

    esmRuleTester.run("prefer-actions-exec-over-child-process", preferActionsExecOverChildProcessRule, {
      valid: [{ code: ghScript(`import { promisify } from "util"; import { spawn } from "child_process"; const spawnAsync = promisify(spawn); spawnAsync("node", ["server.js"]);`) }],
      invalid: [
        {
          code: ghScript(`import { promisify } from "util"; import { exec } from "child_process"; const execAsync = promisify(exec); async function f() { await execAsync("git status"); }`),
          errors: [{ messageId: "preferActionsExec", data: { method: "exec" } }],
        },
      ],
    });
  });
});
