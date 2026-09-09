# ESLint Factory

This project hosts custom ESLint linters for `/actions/setup/js`.

## Goals

- Mine recurring JavaScript/TypeScript defects in `actions/setup/js`.
- Implement custom ESLint rules in TypeScript.
- Compile rules to `dist/` and run them against `actions/setup/js` scripts.

## Commands

- `npm run build` — compile rule sources.
- `npm run lint:setup-js` — build and lint all `../actions/setup/js/**/*.cjs` files.
- `npm run lint:setup-js:changed` — build and lint `../actions/setup/js/*.cjs` files.

## Rules

| Rule | Description |
|---|---|
| [`no-core-exportvariable-non-string`](#no-core-exportvariable-non-string) | Require explicit string values for `core.exportVariable` calls |
| [`no-core-setoutput-non-string`](#no-core-setoutput-non-string) | Require explicit string values for `core.setOutput` calls |
| [`no-empty-catch-block`](#no-empty-catch-block) | Disallow undocumented empty `catch` blocks |
| [`no-duplicate-constant-values`](#no-duplicate-constant-values) | Report constants with duplicate static primitive values in the same file |
| [`no-child-process-interpolated-command`](#no-child-process-interpolated-command) | Disallow interpolated command strings in shell-evaluated `child_process` calls |
| [`no-github-request-interpolated-route`](#no-github-request-interpolated-route) | Disallow interpolated route arguments in Octokit `.request()` calls |
| [`no-json-stringify-error`](#no-json-stringify-error) | Disallow `JSON.stringify()` on caught error variables |
| [`no-json-stringify-equality`](#no-json-stringify-equality) | Disallow comparing two `JSON.stringify()` results for equality |
| [`no-json-stringify-set-or-map`](#no-json-stringify-set-or-map) | Disallow `JSON.stringify()` directly on `Set` or `Map` instances |
| [`no-math-minmax-array-spread`](#no-math-minmax-array-spread) | Disallow spreading a non-literal array into `Math.min(...)` / `Math.max(...)` |
| [`no-misplaced-error-code-definition`](#no-misplaced-error-code-definition) | Require exported error-code constants to be defined in `error_codes.cjs` |
| [`no-throw-plain-object`](#no-throw-plain-object) | Disallow throwing plain object literals |
| [`no-unsafe-catch-error-property`](#no-unsafe-catch-error-property) | Disallow unsafe property access on `catch` error bindings |
| [`no-unsafe-promise-catch-error-property`](#no-unsafe-promise-catch-error-property) | Disallow unsafe property access in promise rejection handlers |
| [`prefer-get-error-message`](#prefer-get-error-message) | Prefer `getErrorMessage(err)` over the inline ternary pattern |
| [`prefer-core-logging`](#prefer-core-logging) | Prefer `@actions/core` logging over `console.log` / `console.info` / `console.debug` |
| [`prefer-number-isnan`](#prefer-number-isnan) | Prefer `Number.isNaN()` over global `isNaN()` |
| [`prefer-structured-clone`](#prefer-structured-clone) | Prefer `structuredClone(...)` over `JSON.parse(JSON.stringify(...))` for deep-cloning data |
| [`require-async-entrypoint-catch`](#require-async-entrypoint-catch) | Require `.catch(...)` on bare async entrypoint calls |
| [`require-await-core-summary-write`](#require-await-core-summary-write) | Require `await` on `core.summary.write()` calls |
| [`require-decodeuricomponent-try-catch`](#require-decodeuricomponent-try-catch) | Require try/catch around `decodeURIComponent(...)` and `decodeURI(...)` on dynamic input |
| [`require-error-cause-in-rethrow`](#require-error-cause-in-rethrow) | Require `{ cause: err }` when rethrowing inside a `catch` block |
| [`require-error-code-in-thrown-error`](#require-error-code-in-thrown-error) | Require standardized error codes in thrown errors when `error_codes.cjs` is imported |
| [`require-error-code-for-github-api-throw`](#require-error-code-for-github-api-throw) | Require standardized error codes for `throw new Error(...)` after GitHub API calls |
| [`require-fetch-response-body-try-catch`](#require-fetch-response-body-try-catch) | Require try/catch around `.json()` or `.text()` on Responses from `fetch(...)` |
| [`require-fetch-timeout`](#require-fetch-timeout) | Require `fetch(...)` calls to include a non-nullish abort `signal` option |
| [`require-fetch-try-catch`](#require-fetch-try-catch) | Require try/catch around awaited `fetch(...)` calls, including chained promise forms without rejection handlers |
| [`require-fs-chmod-try-catch`](#require-fs-chmod-try-catch) | Require try/catch around `fs.chmodSync` and `fs.fchmodSync` |
| [`require-fs-close-sync`](#require-fs-close-sync) | Require `fs.openSync(...)` file descriptors to be closed with `fs.closeSync(fd)` in the same function |
| [`require-fs-io-try-catch`](#require-fs-io-try-catch) | Require try/catch around `fs.statSync`, `readdirSync`, `copyFileSync`, `unlinkSync`, and `renameSync` |
| [`require-fs-sync-try-catch`](#require-fs-sync-try-catch) | Require try/catch around `fs.readFileSync`, `writeFileSync`, and `appendFileSync` |
| [`require-json-parse-try-catch`](#require-json-parse-try-catch) | Require try/catch around `JSON.parse(...)` calls |
| [`require-mkdirsync-try-catch`](#require-mkdirsync-try-catch) | Require try/catch around `fs.mkdirSync` calls |
| [`require-mkdtempsync-try-catch`](#require-mkdtempsync-try-catch) | Require try/catch around `fs.mkdtempSync` calls |
| [`require-realpathsync-try-catch`](#require-realpathsync-try-catch) | Require try/catch around `fs.realpathSync` calls |
| [`require-new-url-try-catch`](#require-new-url-try-catch) | Require try/catch around `new URL(variable)` calls |
| [`require-parseInt-radix`](#require-parseInt-radix) | Require an explicit radix argument to `parseInt()` |
| [`require-nan-check-after-env-numeric-parse`](#require-nan-check-after-env-numeric-parse) | Require NaN validation after parsing numeric values from `process.env` |
| [`require-nan-check-after-split-index-parse`](#require-nan-check-after-split-index-parse) | Require NaN validation after parsing a `split(...)[index]` value |
| [`require-invalid-date-check-before-compare`](#require-invalid-date-check-before-compare) | Require Invalid Date validation before relational comparisons |
| [`require-return-after-core-setfailed`](#require-return-after-core-setfailed) | Require a control-transfer statement after `core.setFailed()` |
| [`require-execsync-try-catch`](#require-execsync-try-catch) | Require try/catch around `execSync(...)` calls from `child_process` |
| [`require-execfilesync-try-catch`](#require-execfilesync-try-catch) | Require try/catch around `execFileSync(...)` calls from `child_process` |
| [`require-sync-exec-timeout`](#require-sync-exec-timeout) | Require positive timeouts for synchronous `child_process` calls |
| [`require-spawn-error-listener`](#require-spawn-error-listener) | Require an `'error'` event listener on async `spawn(...)` child processes |
| [`require-spawnsync-error-check`](#require-spawnsync-error-check) | Require checking `result.error` after `spawnSync` calls |
| [`prefer-get-error-message-over-string`](#prefer-get-error-message-over-string) | Prefer `getErrorMessage(err)` over `String(err)` when interpolating a caught error |
| [`require-rmsync-try-catch`](#require-rmsync-try-catch) | Require try/catch around `fs.rmSync` calls |
| [`no-core-error-then-process-exit`](#no-core-error-then-process-exit) | Disallow `core.error()` immediately followed by `process.exit(nonzero)` |
| [`no-core-error-then-process-exitcode`](#no-core-error-then-process-exitcode) | Disallow `core.error()` immediately followed by `process.exitCode = nonzero` |
| [`no-exec-interpolated-command`](#no-exec-interpolated-command) | Disallow interpolated command strings passed to `@actions/exec` |
| [`no-setfailed-then-exit-zero`](#no-setfailed-then-exit-zero) | Disallow resetting the exit code to success after `core.setFailed()` |
| [`no-err-stack-then-string-fallback`](#no-err-stack-then-string-fallback) | Disallow the `err.stack \|\| String(err)` fallback pattern |
| [`no-caught-error-interpolation`](#no-caught-error-interpolation) | Disallow directly interpolating a caught error in a template literal |
| [`no-core-error-then-setfailed`](#no-core-error-then-setfailed) | Disallow a redundant `core.error()` call immediately before `core.setFailed()` with the same message |
| [`require-escaped-regexp-interpolation`](#require-escaped-regexp-interpolation) | Require regex-escaping of interpolated values in `new RegExp()` template literals |
| [`require-lastindex-reset-before-global-exec-loop`](#require-lastindex-reset-before-global-exec-loop) | Require resetting stateful regexes before global `exec()` loops |
| [`require-page-counter-increment-in-while-true-loop`](#require-page-counter-increment-in-while-true-loop) | Require page counters to advance in manual `while (true)` pagination loops |
| [`require-getexecoutput-exitcode-check`](#require-getexecoutput-exitcode-check) | Require `exitCode` / returned exit code to be read after `getExecOutput()` or `exec()` with `{ ignoreReturnCode: true }` |
| [`prefer-actions-exec-over-child-process`](#prefer-actions-exec-over-child-process) | Prefer `@actions/exec` over `child_process` to spawn processes that run to completion |

### `no-empty-catch-block`

Disallow empty `catch` blocks, which silently swallow errors that otherwise remain invisible in CI logs.

Empty catch blocks are allowed only when their comment explicitly documents an intentional no-op with `intentional`, `best-effort`, or `best effort` (case-insensitive). Otherwise, log the error, assign a fallback value, or rethrow it.

### `no-duplicate-constant-values`

Inventory module-level `const` declarations with static primitive initializers and report each declaration after the first one that uses the same value in a file. The diagnostic names both constants and shows the duplicated value.

The rule compares string, number, boolean, `null`, bigint, regular-expression, static template-literal, and signed numeric initializers. To avoid collisions in their small value spaces, it reports duplicate numeric, boolean, and `null` values only when at least three module-level constants share them. Dynamic expressions, object and array literals, destructuring declarations, function-local declarations, and `let` or `var` declarations are ignored.

### `no-github-request-interpolated-route`

Disallow template literals with interpolations or string concatenation expressions as the route argument of Octokit `.request()` calls.

Using an interpolated route bypasses Octokit's typed route dispatch, can silently produce malformed paths when values contain special characters, and prevents static analysis of the route string.

**Detected Octokit clients:**
- Well-known names: `github`, `octokit`, `githubClient`, `octokitClient`.
- `context.github` — the GitHub context object's client property.
- Identifiers initialized by calling `getOctokit(...)` directly or via known module objects (`github.getOctokit(...)`, `actions.getOctokit(...)`). (Known module object names currently: `github`, `actions`.)
- Simple `const` aliases of any of the above:
  `const gh = github`, `const client = getOctokit(token)`, `const myClient = context.github`.

**Flagged forms:**
- `` github.request(`GET /repos/${owner}/${repo}`, ...) `` — template literal with interpolations.
- `github.request("GET /repos/" + owner + "/" + repo, ...)` — string concatenation.
- `` github.request(`POST ${endpoint}`, ...) `` — opaque whole-route helper; thread a typed route from the caller instead of interpolating the entire path.
- `` context.github.request(`GET /repos/${owner}/${repo}`, ...) `` — `context.github` client.
- `` const gh = github; gh.request(`GET /repos/${owner}/${repo}`, ...) `` — aliased client.
- `` const client = getOctokit(token); client.request(`GET /repos/${owner}/${repo}`, ...) `` — `getOctokit` result alias.

**Out of scope:**
- `this.github.request(...)` — `this`-based member expressions are not resolved.
- `github.request(route, ...)` — variable indirection for the route argument is not resolved.
- `github.request("GET /repos/".concat(owner), ...)` — `.concat()`-built routes are not inspected.
- `github.request("GET /repos" + "/{owner}/{repo}", ...)` — compile-time constant concatenations are accepted.

**Safe alternative:**
```js
github.request("GET /repos/{owner}/{repo}", { owner, repo });
```

For helpers that receive the entire route as a parameter, there is no mechanical `{owner}` / `{repo}` rewrite. Pass a typed route string from the caller instead of interpolating `POST ${endpoint}` or `"POST " + endpoint` at the helper call site.

### `no-core-exportvariable-non-string`

Require `core.exportVariable(name, value)` calls to pass an explicit string value for the targeted low-false-positive cases: numeric literals, boolean literals, `null`, `undefined`, and `.length` member accesses.

Why: GitHub Actions environment variables exported by `core.exportVariable` are always strings. Relying on implicit coercion can silently emit `"null"`, `"undefined"`, `"true"`, or other unintended values into downstream expressions that read the variable.

**Detected forms:**
- `core.exportVariable("MY_VAR", 42)` — numeric literal value.
- `core.exportVariable("MY_FLAG", true)` / `...false` — boolean literal value.
- `core.exportVariable("MY_VAR", null)` / `...undefined` — null or undefined value.
- `core.exportVariable("MY_COUNT", items.length)` — `.length` member access.
- `core["exportVariable"]("MY_VAR", 42)` — computed string-literal property access.
- `coreObj.exportVariable("MY_VAR", 42)` — the `coreObj` alias for `@actions/core`.

**Out of scope:**
- Variable references (e.g. `core.exportVariable("MY_VAR", someVariable)`) — the rule does not resolve variable types.
- Methods other than `exportVariable` — use `no-core-setoutput-non-string` for `setOutput`.
- Objects whose name is not in the known `@actions/core` alias list (`core`, `coreObj`).

**Safe alternatives:**
- `core.exportVariable("MY_COUNT", String(items.length))` — explicit coercion.
- `core.exportVariable("MY_VAR", "")` — empty-string semantics when `null` / `undefined` is intended to mean "not set".

### `no-json-stringify-error`

Disallow `JSON.stringify()` on caught error variables. `Error` properties (`message`, `stack`, etc.) are non-enumerable, so `JSON.stringify(err)` silently produces `{}`.

**Detected scopes:**
- `try { } catch (err) { }` — catch-clause bindings.
- `p.catch(err => ...)` — inline arrow or function callbacks passed as the first argument to `.catch()`.
- `p.then(onFulfilled, err => ...)` — inline rejection handlers passed as the **second** argument to `.then()`, which are semantically equivalent to `.catch()`.

**Out of scope:** named-reference handlers such as `p.catch(handler)` or `p.then(ok, handler)` — the rule does not follow references across files or scopes.

Flagged forms:
- `JSON.stringify(err)` where `err` is a catch-clause or inline rejection-handler parameter.
- `JSON.stringify(err, null, 2)` (with replacer/space arguments).

Safe alternatives:
- `getErrorMessage(err)` from `error_helpers.cjs` (auto-suggested fix).
- `JSON.stringify({ message: err.message, stack: err.stack })` — explicitly serializing safe string properties.

### `no-math-minmax-array-spread`

Disallow spreading an array of unknown size into `Math.min(...)` / `Math.max(...)`. Spreading an array into call arguments pushes every element onto the call stack, so a large array throws `RangeError: Maximum call stack size exceeded` (the limit is engine and version dependent, commonly in the tens of thousands of elements). Arrays built from workflow runs, API responses, or file scans have no static size bound, so the crash only appears on large inputs.

**Detected forms:**
- `Math.max(...values)` / `Math.min(...values)` — identifier spread.
- `Math.max(...stats.durations)` — member expression spread.
- `Math.min(...runs.map(run => run.duration))` — call expression spread.
- `Math["max"](...values)` — computed access to the same methods.

**Out of scope:**
- `Math.max(0, ...values)` and `Math.min(a, b, ...values)` — fixed arguments alongside the spread suggest an intentional, likely bounded call shape.
- `Math.max(...[1, 2, 3])` — inline array literals are statically bounded by the source.
- Calls where `Math` is shadowed by a local declaration.

**Safe alternative:**
- `values.reduce((a, b) => Math.max(a, b), -Infinity)` / `values.reduce((a, b) => Math.min(a, b), Infinity)` — folds the array without expanding it into arguments, using the same identity value `Math.max()` / `Math.min()` return on an empty array so the empty-input result matches the spread form instead of throwing.

### `no-misplaced-error-code-definition`

Require exported constants whose names end in `_ERROR_CODE` or `_REASON_CODE` to be defined in the centralized `error_codes.cjs` registry. Local-only constants are allowed because they do not establish a shared code outside the registry.

**Flagged form:**
```js
const POLICY_FILE_PROTECTION_DENIED_REASON_CODE = "POLICY_FILE_PROTECTION_DENIED";
module.exports = { POLICY_FILE_PROTECTION_DENIED_REASON_CODE };
```

**Safe alternative:**
Define and export the constant from `error_codes.cjs`, then import it where needed.

### `prefer-number-isnan`

Prefer `Number.isNaN()` over global `isNaN()` to avoid silent coercion of non-numeric inputs.

Global `isNaN()` coerces its argument before testing, so `isNaN("123")` returns `false` because `"123"` coerces to the number `123` — masking that the input was a string. `Number.isNaN()` is strict and does not coerce, making numeric validation reliable when handling raw inputs such as environment variables or API strings.

Flagged forms:
- `isNaN(x)`
- `globalThis.isNaN(x)` / `globalThis["isNaN"](x)`
- `window.isNaN(x)` / `window["isNaN"](x)`
- `global.isNaN(x)` / `global["isNaN"](x)`

Locally shadowed bindings (e.g. `const isNaN = Number.isNaN`) are intentionally excluded.

### `prefer-structured-clone`

Prefer `structuredClone(...)` over `JSON.parse(JSON.stringify(...))` for deep-cloning data. The JSON round trip is slower, drops values such as `undefined` and functions, converts `Date` instances to strings, and throws on circular references.

**Detected forms:**
- `JSON.parse(JSON.stringify(value))`
- `JSON["parse"](JSON["stringify"](value))`

The rule only reports a `JSON.stringify(...)` call with exactly one argument, because replacer or indentation arguments change the round-trip semantics.

The rule suggests replacing the round trip with `structuredClone(value)` unless `value` is an identifier that is assigned a function-valued property, initialized with a function-valued object property, or checked with `typeof value.property === "function"` anywhere in the file. Those identifiers are still reported, but without a suggestion, because `structuredClone` throws on functions while JSON serialization silently drops them.

### `no-throw-plain-object`

Disallow throwing plain object literals (`throw { ... }`). Plain objects lack a `.stack` trace and a meaningful `.message` string, making errors hard to debug and incompatible with catch-clause error utilities such as `getErrorMessage`.

**Detected forms:**
- `throw { message: "not found" }` — object literal with a `message` property.
- `throw { code: 500, message: "internal" }` — object literal with extra fields.
- `throw {}` — empty object literal.
- `throw { ...base, code: 1 }` — spread elements or computed keys (no autofix suggestion, only an error).

**Out of scope:**
- `throw err` — identifier references are not checked.
- `throw new Error(...)` — `Error` constructor calls are always accepted.
- `throw Object.assign(new Error(...), { ... })` — already in the recommended form.

**JSON-RPC exemption:** Objects that match the JSON-RPC error shape `{ code: <negative integer literal>, message: <any>, data?: <any> }` are intentionally exempt. These are deliberately thrown at protocol boundaries where the receiver reads `code`, `message`, and `data` directly rather than a stack trace. Only keys from `{ code, message, data }` are allowed; extra keys, a positive `code`, a fractional `code`, or a missing `message` disqualify the exemption.

**Safe alternatives:**
- `throw new Error(message)` — minimal form.
- `throw Object.assign(new Error(message), { code, ... })` — attaches extra context while preserving the stack trace.

The rule provides an autofix suggestion for plain-key objects: it extracts the `message` property as the `Error` argument and collects remaining properties into `Object.assign(...)`.

### `no-core-setoutput-non-string`

Require `core.setOutput(name, value)` calls to pass an explicit string value for the targeted low-false-positive cases: numeric literals, boolean literals, `null`, `undefined`, and `.length` member accesses.

Why: GitHub Actions step outputs are strings. Relying on implicit coercion can silently emit `"null"`, `"undefined"`, `"true"`, or other unintended values into downstream expressions.

Typical fixes:
- `core.setOutput("count", String(count))`
- `core.setOutput("optional", "")` when empty-string semantics are intended for `null` / `undefined`

### `no-unsafe-catch-error-property`

Disallow direct access to `.message`, `.stack`, `.code`, `.status`, `.cause`, or `.name` on a `catch (err)` binding unless the code first proves the thrown value is safe to inspect.

Accepted guards:
- `getErrorMessage(err)`
- `err instanceof Error`
- `typeof err === "object" && err !== null`

Why: JavaScript can throw non-`Error` values, so `err.message` is not always safe.

### `no-unsafe-promise-catch-error-property`

Disallow the same unsafe error-property accesses inside inline promise rejection handlers such as `.catch(err => ...)`.

This rule mirrors `no-unsafe-catch-error-property`, but for promise rejection values rather than `catch` clauses. Truthiness checks such as `err && err.message` are recognized for the accessed property.

### `prefer-get-error-message`

Prefer `getErrorMessage(err)` over the repeated pattern `err instanceof Error ? err.message : String(err)`.

Why: `getErrorMessage(err)` centralizes safe error extraction and also sanitizes HTML error-page responses in the gh-aw runtime helpers.

### `require-async-entrypoint-catch`

Require bare calls to module-scope async entrypoints such as `main()` to be chained with `.catch(...)` when they are invoked outside an async context.

Flagged form:
- `main();`

Safe alternatives:
- `main().catch(err => { ... });`
- `await main();` when already inside an async function

### `require-await-core-summary-write`

Require `core.summary.write()` (including known aliases and fluent `core.summary.*().write()` chains) to be awaited when used as a bare expression.

Why: `core.summary.write()` returns a promise. Dropping it can truncate or lose the step summary if the process exits first.

Intentional exception:
- `void core.summary.write()` is treated as an explicit deliberate discard marker.

### `require-error-cause-in-rethrow`

Require rethrown `new Error(...)` values inside a `catch` block to preserve the original failure with `{ cause: err }` when the new message already references the caught error or a direct alias of it.

Flagged form:
- `throw new Error(\`failed: ${getErrorMessage(err)}\`);`

Safe alternative:
- `throw new Error(\`failed: ${getErrorMessage(err)}\`, { cause: err });`

### `require-fetch-try-catch`

Require awaited `fetch(...)` calls to be wrapped in `try/catch`, including member-chained promise forms rooted in `fetch(...)`.

Why: `fetch` rejects with `TypeError` on network failures (DNS errors, connection refused, timeouts surfaced as aborts, etc.). Without either an enclosing `try/catch` or an explicit promise rejection handler, the action crashes with an unhelpful uncaught exception.

**Flagged forms:**
- `await fetch(url);`
- `await fetch(url).then(res => res.json());`
- `await fetch(url).then(ok).finally(cleanup);`

**Not flagged:**
- `try { await fetch(url).then(res => res.json()); } catch (err) {}`
- `await fetch(url).catch(handleFetchError);`
- `await fetch(url).then(onFulfilled, onRejected);`

**Out of scope:**
- locally shadowed `fetch` bindings such as `async function f(fetch) { await fetch(url); }`
- named-reference rejection handlers are not inspected for correctness; the rule only checks that `.catch(handler)` or `.then(ok, onErr)` is present on the awaited fetch chain

### `require-fetch-response-body-try-catch`

Require awaited `.json()` or `.text()` calls on a `fetch(...)` response to be wrapped in `try/catch`. Reading a response body can reject when the stream errors, and `.json()` can also reject for invalid JSON; an unhandled rejection crashes the action with an unhelpful exception.

**Flagged forms:**
- `await fetch(url).json();`
- `const response = await fetch(url); await response.text();`

The rule recognizes direct global `fetch(...)` chains and identifiers assigned from a bare `await fetch(...)`. It supports direct or string-literal computed body methods, such as `response["json"]()`.

**Not flagged:**
- `try { await fetch(url).json(); } catch (err) {}`
- `try { await response.text(); } catch (err) {}` when `response` was assigned from `await fetch(...)`

**Out of scope:**
- body methods other than `.json()` and `.text()`
- response values not resolved to a bare `await fetch(...)`
- calls in deferred callbacks nested within a `try` block, because that `try` cannot catch their asynchronous failures

### `require-fetch-timeout`

Require `fetch(...)` calls to include a `signal` option so requests can be aborted instead of hanging indefinitely.

Why: without an abort signal, a stalled network call can block the action until the workflow/job timeout ends it.

**Flagged forms:**
- `fetch(url);`
- `fetch(url, null);`
- `fetch(url, undefined);`
- `fetch(url, { method: "GET" });`
- `fetch(url, { signal: null });`
- `globalThis.fetch(url, { method: "GET" });`

**Not flagged:**
- `fetch(url, { signal: AbortSignal.timeout(10_000) });`
- `fetch(url, { signal: controller.signal });`
- `fetch(url, options);` (options object is not statically resolved)
- `fetch(url, { ...options });` (spread may already include `signal`)
- `obj.fetch(url);` (only global `fetch` calls are in scope)

### `require-fs-io-try-catch`

Require `fs.statSync`, `fs.readdirSync`, `fs.copyFileSync`, `fs.unlinkSync`, and `fs.renameSync` calls to be wrapped in `try/catch`.

Why: these synchronous filesystem methods throw on missing files, permission errors (`EACCES`), busy resources (`EBUSY`), and other I/O failures. An unhandled throw crashes the action without surfacing a useful diagnostic message.

**Detected forms:**
- `fs.statSync(path)` — direct call on a known `require("fs")` result.
- `fs["readdirSync"](dir)` — computed string-literal property access.
- `const { unlinkSync } = require("fs"); unlinkSync(path)` — destructured binding from `require("fs")` or `require("node:fs")`.
- ESM namespace imports: `import * as fs from "fs"; fs.copyFileSync(src, dest)`.
- ESM named imports: `import { renameSync } from "fs"; renameSync(src, dest)`.
- Bare unbound identifiers: `statSync(path)` when `statSync` is not a locally bound variable.

**Out of scope:**
- Objects whose `require` source is not the Node `fs` / `node:fs` module.
- `try { ... } finally { ... }` without a `catch` clause is still flagged.

**Safe alternative:**
```js
try {
  fs.statSync(filePath);
} catch (err) {
  throw new Error("fs.statSync failed: " + (err instanceof Error ? err.message : String(err)), { cause: err });
}
```

### `require-fs-chmod-try-catch`

Require `fs.chmodSync` and `fs.fchmodSync` calls to be wrapped in `try/catch`.

Why: these calls can throw for missing files or descriptors, permission errors, and unsupported filesystems. A call-site catch preserves useful error context.

**Detected forms:**
- `fs.chmodSync(path, mode)` and `fs["chmodSync"](path, mode)`.
- `fs.fchmodSync(fd, mode)`.
- Bindings imported or required from `fs` / `node:fs`, including destructured bindings.

**Out of scope:**
- Objects that are not resolved to the Node `fs` / `node:fs` module.
- `try { ... } finally { ... }` without a `catch` clause is still flagged.

### `require-fs-close-sync`

Require file descriptors returned by `fs.openSync(...)` to be closed with `fs.closeSync(fd)` in the same enclosing function.

Why: unclosed descriptors leak file handles for the lifetime of the process and can eventually surface as unrelated `EMFILE` failures.

**Detected forms:**
- `const fd = fs.openSync(path, "w")` with no matching `fs.closeSync(fd)` in the same enclosing function.
- `let fd; fd = fs.openSync(path, "w")` with no matching `fs.closeSync(fd)` in the same enclosing function.

**Accepted close forms** (in the same enclosing function):
- `fs.closeSync(fd)`, including inside `try`/`finally`.
- Property access and single-level aliases such as `fs.closeSync(handle.fd)` after `const handle = { fd }`, or `fs.closeSync(alias)` after `const alias = fd`.

**Out of scope:**
- Destructured bindings and inline argument forms such as `consume(fs.openSync(...))`.
- Close calls placed in a nested function, including cleanup callbacks such as `const cleanup = () => fs.closeSync(fd)`, and cross-function close pairs (open in one function, close in another). Only closes in the same enclosing function count.
- Strict control-flow proof. A `fs.closeSync(fd)` anywhere in the enclosing function body is accepted.

### `require-fs-sync-try-catch`

Require `fs.readFileSync`, `fs.writeFileSync`, and `fs.appendFileSync` calls to be wrapped in `try/catch`.

Why: these synchronous filesystem calls throw on missing files, permission errors, and disk failures, which otherwise crash the action without useful context.

Current scope:
- direct `fs.readFileSync(...)`
- known `require("fs")` aliases
- destructured aliases such as `const { readFileSync } = require("fs")`

### `require-json-parse-try-catch`

Require `JSON.parse(...)` calls to be wrapped in `try/catch`.

Why: malformed JSON should produce a controlled failure path in runtime scripts rather than an uncaught exception.

Out of scope:
- aliased or destructured `JSON.parse` references such as `const parse = JSON.parse`

### `require-parseInt-radix`

Require `parseInt()` to include an explicit radix argument.

Flagged forms:
- `parseInt(value)`
- `Number.parseInt(value)`
- `globalThis.parseInt(value)`

Why: omitting the radix allows implicit base detection, which can silently accept prefixes such as `0x`.

### `require-nan-check-after-env-numeric-parse`

Require NaN validation after parsing numeric values from `process.env`.

Why: `parseInt`, `parseFloat`, `Number.parseInt`, `Number.parseFloat`, and `Number()` silently return `NaN` for malformed environment input (empty string, typo, unexpected value). An unvalidated `NaN` can propagate silently into comparisons (e.g. rate-limit thresholds, size limits, timeouts), loop bounds, or GitHub API payloads without any error surfacing.

**Detected parse forms (first argument must trace back to `process.env`):**
- `parseInt(process.env.FOO, 10)` — global `parseInt`
- `parseFloat(process.env.FOO)` — global `parseFloat`
- `Number.parseInt(process.env.FOO, 10)` — `Number.parseInt`
- `Number.parseFloat(process.env.FOO)` — `Number.parseFloat`
- `Number(process.env.FOO)` — `Number` conversion function

**Detected env-access patterns in the first argument:**
- Direct: `process.env.FOO`
- Logical fallbacks: `process.env.FOO || "default"`, `process.env.FOO ?? "default"`
- Optional chaining: `process.env.FOO?.trim()`
- Ternary: `process.env.FOO ? process.env.FOO : "default"`

**Considered validated when** the declared variable is passed as the sole argument to `Number.isNaN(...)` or `isNaN(...)` anywhere in the enclosing file scope.

**Safe pattern:**
```js
const maxRuns = parseInt(process.env.MAX_RUNS, 10);
if (Number.isNaN(maxRuns)) throw new Error("MAX_RUNS must be a valid integer");
```

### `require-mkdirsync-try-catch`

Require `fs.mkdirSync` calls to be wrapped in `try/catch`.

Why: `mkdirSync` throws synchronously on permission errors, invalid paths, or unexpected filesystem state. An unhandled throw crashes the action without surfacing a useful diagnostic.

**Detected forms:**
- `fs.mkdirSync(dir)` / `fs.mkdirSync(dir, { recursive: true })` — direct calls on a known `require("fs")` result.
- `fs["mkdirSync"](dir, ...)` — computed string-literal property access.
- `const { mkdirSync } = require("fs"); mkdirSync(dir)` — destructured binding from `require("fs")` or `require("node:fs")`.
- ESM namespace imports: `import * as fs from "fs"; fs.mkdirSync(dir)`.
- ESM named imports: `import { mkdirSync } from "fs"; mkdirSync(dir)`.

**Out of scope:**
- Objects whose `require` source is not the Node `fs` / `node:fs` module (e.g. `mockFs.mkdirSync`, `storage.mkdirSync`, or `const fs = require("mock-fs"); fs.mkdirSync`).
- Other `fs` methods such as `existsSync` — use `require-fs-sync-try-catch` for `readFileSync`, `writeFileSync`, and `appendFileSync`; use `require-fs-io-try-catch` for `statSync`, `readdirSync`, `copyFileSync`, `unlinkSync`, and `renameSync`.
- `try { ... } finally { ... }` without a `catch` clause is still flagged.

**Safe alternative:**
```js
try {
  fs.mkdirSync(dir, { recursive: true });
} catch (err) {
  throw new Error("fs.mkdirSync failed: " + (err instanceof Error ? err.message : String(err)), { cause: err });
}
```

### `require-mkdtempsync-try-catch`

Require `fs.mkdtempSync` calls to be wrapped in `try/catch`.

Why: `mkdtempSync` throws synchronously when the parent directory does not exist, permissions are denied, or disk space is exhausted. An unhandled throw crashes the action without surfacing a useful diagnostic.

**Detected forms:**
- `fs.mkdtempSync(prefix)` — direct call on a known `require("fs")` result.
- `fs["mkdtempSync"](prefix)` — computed string-literal property access.
- `const { mkdtempSync } = require("fs"); mkdtempSync(prefix)` — destructured binding from `require("fs")` or `require("node:fs")`.
- ESM namespace imports: `import * as fs from "fs"; fs.mkdtempSync(prefix)`.
- ESM named imports: `import { mkdtempSync } from "fs"; mkdtempSync(prefix)`.

**Out of scope:**
- Objects whose `require` source is not the Node `fs` / `node:fs` module (e.g. `mockFs.mkdtempSync`, `storage.mkdtempSync`, or `const fs = require("mock-fs"); fs.mkdtempSync`).
- Other `fs` methods such as `mkdirSync` — use `require-mkdirsync-try-catch`; use `require-fs-io-try-catch` for `statSync`, `readdirSync`, `copyFileSync`, `unlinkSync`, and `renameSync`.
- `try { ... } finally { ... }` without a `catch` clause is still flagged.

**Known limitation — no autofix for `VariableDeclaration`:** when the flagged `fs.mkdtempSync(...)` appears as the initializer of a variable declaration (`const tmpDir = fs.mkdtempSync(prefix)`), the rule reports the error but emits no autofix suggestion. Wrapping the declaration in `try { ... } catch { ... }` would move subsequent uses of `tmpDir` outside the `try` block, leaving them referencing an undeclared binding. Only `ExpressionStatement` and `ReturnStatement` positions receive an autofix suggestion.

**Safe alternative:**
```js
try {
  const tmpDir = fs.mkdtempSync(prefix);
  // use tmpDir here
} catch (err) {
  throw new Error("fs.mkdtempSync failed: " + (err instanceof Error ? err.message : String(err)), { cause: err });
}
```

### `require-realpathsync-try-catch`

Require `fs.realpathSync` calls to be wrapped in `try/catch`.

Why: `realpathSync` throws synchronously when the target path is missing, permissions are denied, or a symlink cycle is encountered. Wrapping the call preserves call-site-specific error context and ensures path containment checks are not skipped on failure.

**Detected forms:**
- `fs.realpathSync(path)` — direct call on a known `require("fs")` result.
- `fs["realpathSync"](path)` — computed string-literal property access.
- `const { realpathSync } = require("fs"); realpathSync(path)` — destructured binding from `require("fs")` or `require("node:fs")`.
- ESM namespace imports: `import * as fs from "fs"; fs.realpathSync(path)`.
- ESM named imports: `import { realpathSync } from "fs"; realpathSync(path)`.

**Out of scope:**
- Objects whose `require` source is not the Node `fs` / `node:fs` module.
- Calls already inside a `try` block with a `catch` clause.
- `try { ... } finally { ... }` without a `catch` clause is still flagged.

**Known limitation — no autofix for `VariableDeclaration`:** when the flagged call appears as a variable initializer, the rule reports the error but emits no autofix suggestion. Only `ExpressionStatement` and `ReturnStatement` positions receive an autofix suggestion.

**Safe alternative:**
```js
try {
  const resolved = fs.realpathSync(path);
} catch (err) {
  throw new Error("fs.realpathSync failed: " + (err instanceof Error ? err.message : String(err)), { cause: err });
}
```

### `require-new-url-try-catch`

Require `new URL(variable)` calls to be wrapped in `try/catch`.

### `require-decodeuricomponent-try-catch`

Require `decodeURIComponent(...)` and `decodeURI(...)` on dynamic input to be wrapped in `try/catch`.

Malformed percent-encoded input throws `URIError` and can crash the action if left unhandled.

Why: the `URL` constructor throws a `TypeError` when given an invalid or relative URL string, which crashes the action with an unhelpful uncaught exception.

**Detected forms:**
- `new URL(urlStr)` — first argument is a runtime-dynamic expression.
- `new URL(process.env.GITHUB_SERVER_URL)` — environment variable reference.
- `` new URL(`https://${host}/path`) `` — template literal with expressions.
- `new URL(host + "/x")` — string concatenation containing a variable.
- `new URL("/path", base)` — dynamic second (base) argument.
- `new URL()` — zero arguments (always throws `TypeError` at runtime).

**Out of scope (not flagged):**
- `new URL("https://github.com")` — compile-time constant string literal or static concatenation.
- `` new URL(`https://github.com/static`) `` — template literal with no expressions.
- `new URL("https://github.com" + "/owner/repo")` — concatenation of string literals only.
- `new URL(import.meta.url)` — `import.meta.url` is always a valid absolute URL in ES modules.
- `new URL("./relative/path", import.meta.url)` — `import.meta.url` as the base is safe.
- `function parse(URL, value) { return new URL(value); }` — `URL` shadowed by a local binding is not the global constructor.
- Calls already inside a `try` block with a `catch` clause.

**Known limitation — no autofix for `VariableDeclaration`:** when the flagged `new URL(...)` appears as the initializer of a variable declaration (`const u = new URL(urlStr)`), the rule reports the error but emits no autofix suggestion. Wrapping that statement in `try { ... } catch { ... }` would move subsequent uses of `u` outside the `try` block, leaving them referencing an undeclared binding. Only `ExpressionStatement` and `ReturnStatement` positions receive an autofix suggestion.

**Safe alternative:**
```js
try {
  const u = new URL(urlStr);
  // use u here
} catch (err) {
  throw new Error("URL constructor call failed: " + (err instanceof Error ? err.message : String(err)), { cause: err });
}
```

### `require-return-after-core-setfailed`

Require a `return`, `throw`, `break`, `continue`, or `process.exit()` statement immediately after `core.setFailed()` to prevent execution from continuing after a failure is declared.

Why: `core.setFailed()` only marks the action as failed at the end of the run — it does **not** stop execution. Any code that follows continues to run in a failed state, which can produce misleading output or unexpected side effects.

**Detected forms:**
- `core.setFailed(...)` — direct non-computed call on a known `@actions/core` alias (`core`, `coreObj`).
- `core["setFailed"](...)` — computed string-literal property access.
- `const c = core; c.setFailed(...)` — single-assignment alias for a core-like object.
- `const { setFailed } = core; setFailed(...)` — destructured binding from a core-like object (including renamed destructuring such as `const { setFailed: sf } = core`).

**Accepted control-transfer statements:**
- `return` / `return value`
- `throw new Error(...)`
- `process.exit(...)`
- `break` (inside a loop or switch body)
- `continue` (inside a loop body)

**Out of scope:**
- Calls on unrecognized objects: `other.setFailed("bad")` is not flagged.
- `setFailed("bad")` as a bare identifier call (not destructured from a core alias) is not flagged.

**Known limitation:** `break` and `continue` are accepted as control-transfer statements within loop or switch bodies, but they do not prevent post-loop or post-switch statements from running in a failed state. Detecting that kind of continuation is out of scope.

**Cross-block fall-through:** the rule also flags `core.setFailed(...)` that is the last statement of a nested block when the enclosing block has a subsequent statement that would execute unconditionally after the nested block exits:
```js
// Flagged — doMore() runs after setFailed even though they are in different blocks
function f() {
  if (!ok) {
    core.setFailed("msg");
  }
  doMore();
}
```

**Safe alternative:**
```js
if (!ok) {
  core.setFailed("msg");
  return;
}
doMore(); // only reached when ok is true
```

### `require-spawnsync-error-check`

Require `spawnSync` result variables to check `result.error` in addition to `result.status`.

Why: when `spawnSync` cannot spawn the child process (e.g. `ENOENT`, `ETIMEDOUT`), `result.status` is `null` and `result.error` holds the actual `Error`. Checking only `result.status` silently swallows spawn-level failures or reports a misleading "exit null" diagnostic.

**Detected forms:**
- `const result = spawnSync(cmd, args)` — unqualified `spawnSync` identifier.
- `const result = childProcess.spawnSync(...)` — `childProcess` namespace alias.
- `const result = child_process.spawnSync(...)` — `child_process` namespace alias.
- Object-destructuring: `const { status, error } = spawnSync(...)` — the destructured `error` binding must appear in a guard position.
- Renamed destructuring: `const { error: spawnError } = spawnSync(...)` — the renamed binding must appear in a guard position.
- String-literal key: `const { "error": spawnError } = spawnSync(...)`.

**Accepted guard positions for `.error`:**
- `if (result.error) throw result.error;` — direct truthiness check as an `if` test.
- `if (result.error !== undefined) throw result.error;` — binary comparison as an `if` test.
- `if (result.status !== 0 || result.error) throw result.error;` — `.error` on the right side of `||` where the full expression is the `if` test.
- `throw result.error;` / `return result.error;` — direct throw or return.
- `const e = result.error; if (e) throw e;` — single-assignment immutable alias that is later guarded.

**Out of scope (not recognized as valid guards):**
- `result.error && result.error.message` — right-hand side of `&&` is not an independent guard.
- `result.error || new Error("fallback")` — `||` right-hand operand is not a guard.
- `result.error ?? null` — nullish coalescing is not a guard.
- `core.info(String(result.error))` — logging without a conditional check does not count.
- `AssignmentExpression` forms (`result = spawnSync(...)`) and inline chains (`spawnSync(...).status`) are not analyzed.
- Passing the result object to a helper function that internally checks `.error` is not recognized.
- Mutable aliases (`let e = result.error; e = undefined; if (e) throw e`) are rejected because the original value may have been discarded before the guard.

### `prefer-core-logging`

Prefer `@actions/core` logging methods (`core.info`, `core.debug`) over `console.log`, `console.info`, and `console.debug`.

`core.*` logging methods integrate with the GitHub Actions annotation system (errors and warnings appear as file annotations in the UI) and produce structured log output. `global.core` is always available via `shim.cjs` in the Node.js context and via `github-script` in the Actions context.

**Covered methods and their replacements:**

| `console.*` method | Suggested replacement |
|---|---|
| `console.log` | `core.info` |
| `console.info` | `core.info` |
| `console.debug` | `core.debug` |

**Intentionally excluded: `console.error` and `console.warn`**

`console.error` and `console.warn` write to **`process.stderr`**, while `core.error` and `core.warning` emit GitHub Actions workflow commands to **`process.stdout`**. For processes that own stdout as a data/protocol channel — such as stdio MCP servers and transports — replacing stderr logging with stdout logging would corrupt the JSON-RPC stream. Because the stream change is not behavior-preserving, the rule never reports `console.error` or `console.warn` and offers no suggestion to replace them.

**Why the exclusion does not extend to `log` / `info` / `debug`**

The risk above comes from the stream *change* (stderr → stdout), not from workflow commands as such. `console.log`, `console.info`, and `console.debug` already write to `process.stdout` (`console.debug` is an alias of `console.log` in Node.js), and their replacements also write to `process.stdout` — `core.info` writes a plain message, `core.debug` writes a `::debug::` workflow command. Since both sides use the same stream, the substitution cannot move output onto a stdio protocol channel that was previously clean: a process that owns stdout for framing is already corrupting it by calling `console.log` at all. No stdio-owning file exclusion is therefore applied to these three methods.


### `no-child-process-interpolated-command`

Disallow interpolated template literals and dynamic string concatenation as command arguments to shell-evaluated `child_process` calls.

Why: command strings evaluated by a shell (`exec`, `execSync`, `spawn` / `spawnSync` with `shell: true`, and `execFile` / `execFileSync` with `shell: true`) can become shell-injection vectors when command content is assembled dynamically.

**Detected forms (when bound to `child_process` / `node:child_process`):**
- `const { execSync } = require("child_process"); execSync(\`git checkout ${branch}\`)`
- `const cp = require("child_process"); cp.exec("git checkout " + branch)`
- `const run = cp.execSync; run("git checkout " + branch)` — member alias call.
- `spawn(\`git checkout ${branch}\`, { shell: true })` — shell-enabled spawn.
- `execFileSync("git " + branch, ["status"], { shell: true })` — shell-enabled execFileSync.
- `spawn("git checkout " + branch, ...opts)` — spread options are treated conservatively as potentially shell-enabled.
- ESM imports are recognized (`import { execSync } from "node:child_process"`).
- `` execSync(`git checkout ${branch}`.trim()) `` — chained string-normalizing methods (`trim`, `trimStart`, `trimEnd`, `toLowerCase`, `toUpperCase`, `toLocaleLowerCase`, `toLocaleUpperCase`, `replace`, `replaceAll`, `normalize`) are unwrapped before the check.
- `` execSync("git checkout PLACEHOLDER".replace("PLACEHOLDER", () => branch)) `` — a `.replace()` / `.replaceAll()` replacer callback's return value is also inspected.

**Not flagged:**
- Fully static command strings (`"git status"`, `` `git status` ``, and fully static `+` concatenations), including when a string-normalizing method is chained onto them.
- `spawn(cmd, [args])` / `spawnSync(cmd, [args])` without `shell: true`.
- `execFile` / `execFileSync` without `shell: true`.

**Out of scope:** github-script's injected `exec.exec(...)` / `exec.getExecOutput(...)`. Those are covered by `no-exec-interpolated-command`, which targets `@actions/exec` argument-splitting correctness rather than shell injection.

**Safer alternatives:**
- Use a static executable and pass arguments as an array (`execFileSync("git", ["checkout", branch])`).
- Avoid `shell: true` unless strictly required.


### `require-execsync-try-catch`

Require `execSync` calls sourced from `child_process` to be wrapped in `try/catch`.

Why: `execSync` throws an `Error` containing child-process result fields (for example `status`, `signal`, `stdout`, `stderr`) when the child process exits with a non-zero status code or is killed by a signal. An unhandled throw crashes the action without surfacing a useful diagnostic.

**Detected forms:**
- `const { execSync } = require("child_process"); execSync(...)` — destructured.
- `const cp = require("child_process"); cp.execSync(...)` — namespace access.
- `const run = cp.execSync; run(...)` — aliased via member expression.
- `import { execSync } from "child_process"; execSync(...)` — ESM named import.
- Both `"child_process"` and `"node:child_process"` specifiers are recognized.

**Not flagged:**
- `execSync` from any module other than `child_process` / `node:child_process`.
- Calls already inside an enclosing `try { ... } catch { ... }` block.

### `require-execfilesync-try-catch`

Require `execFileSync` calls sourced from `child_process` to be wrapped in `try/catch`.

Why: `execFileSync` has identical throw-on-failure semantics to `execSync` — it throws an `Error` containing child-process result fields (for example `status`, `signal`, `stdout`, `stderr`) when the child process exits with a non-zero status code or is killed by a signal. An unhandled throw crashes the action without surfacing a useful diagnostic.

**Detected forms:**
- `const { execFileSync } = require("child_process"); execFileSync(...)` — destructured.
- `const cp = require("child_process"); cp.execFileSync(...)` — namespace access.
- `const run = cp.execFileSync; run(...)` — aliased via member expression.
- `import { execFileSync } from "child_process"; execFileSync(...)` — ESM named import.
- Both `"child_process"` and `"node:child_process"` specifiers are recognized.

**Not flagged:**
- `execFileSync` from any module other than `child_process` / `node:child_process`.
- Calls already inside an enclosing `try { ... } catch { ... }` block.

**Out of scope:** `execFile` (the async, callback-based sibling) is intentionally excluded. The async form accepts a callback and does not throw synchronously; errors are delivered through the callback or the returned `ChildProcess` event emitter, so a synchronous try/catch provides no protection.

### `require-spawn-error-listener`

Require child processes created with async `spawn()` to register an `'error'` event listener.

Why: when `spawn()` cannot launch the executable (for example `ENOENT` or `EACCES`), Node emits an `'error'` event on the returned `ChildProcess` instead of throwing synchronously. Without an attached listener, that event is unhandled and crashes the action.

**Detected forms (when bound to `child_process` / `node:child_process`):**
- `const { spawn } = require("child_process"); const child = spawn(...)`
- `const cp = require("child_process"); const child = cp.spawn(...)`
- `import { spawn } from "child_process"; const child = spawn(...)`

The rule then looks for `child.on("error", ...)` or `child.once("error", ...)` on that same variable anywhere it is referenced, including nested callbacks in the same file.

**Out of scope:**
- Assignment-expression forms such as `child = spawn(...)`
- Inline chains such as `spawn(...).on("error", ...)`
- Passing the child process to a helper function that registers the listener later
- `spawn` identifiers that are not bound to Node's `child_process` module

### `prefer-get-error-message-over-string`

Prefer `getErrorMessage(err)` over `String(err)` when interpolating a caught error into a template literal, when `getErrorMessage` is already resolvable in scope.

`String(err)` on an `Error` produces the redundant `"Error: message"` prefix and does not sanitize GitHub's HTML error-page responses, while `getErrorMessage(err)` handles both correctly. Several `actions/setup/js` files already import `getErrorMessage` elsewhere yet still call `String(err)` at other call sites in the same file — this rule catches that inconsistency.

**Detected forms:**
- `` `Failed: ${String(err)}` `` — `String(...)` wrapping a caught-error identifier inside a template literal expression, when `getErrorMessage` is resolvable in the enclosing scope (import, function declaration, or earlier declaration).

**Not flagged:**
- `String(err)` outside of a template literal.
- `String(value)` where `value` is not a caught error (not bound by a `catch` clause or the first parameter of an inline `.catch()`/`.then()` rejection handler).
- `String(err)` when `getErrorMessage` is not resolvable in scope.
- Tagged template literals — values are passed to the tag function as-is, not string-coerced.

The rule provides an autofix suggestion that replaces `String(err)` with `getErrorMessage(err)`.

### `require-rmsync-try-catch`

Require `fs.rmSync` calls in `actions/setup/js` scripts to be wrapped in `try/catch`.

`rmSync` throws synchronously on permission errors, invalid paths, or unexpected filesystem state; an unhandled throw crashes the action without surfacing a useful diagnostic.

**Not flagged:** Calls already inside an enclosing `try { ... } catch { ... }` block.

### `no-core-error-then-process-exit`

Disallow `core.error()` immediately followed by `process.exit(nonzero)`.

Prefer `core.setFailed(msg)` to signal action failure; it marks the action failed and allows post-action cleanup hooks to run. In standalone `node` scripts, `process.exit(nonzero)` does fail the step, but `core.setFailed` is more portable.

The rule provides an autofix suggestion that replaces `core.error(msg); process.exit(...);` with `core.setFailed(msg); return;`.

### `no-core-error-then-process-exitcode`

Disallow `core.error()` immediately followed by `process.exitCode = nonzero`.

Prefer `core.setFailed(msg)` to signal action failure; it marks the action failed and allows post-action cleanup hooks to run. Unlike `process.exit(1)`, `process.exitCode = 1` does not halt execution immediately.

The rule provides an autofix suggestion that replaces `core.error(msg); process.exitCode = nonzero;` with `core.setFailed(msg);` (at module top level) or `core.setFailed(msg); return;` (inside `main()`).

### `no-exec-interpolated-command`

Disallow passing an interpolated template literal or dynamic string concatenation as the command argument to `@actions/exec`'s `exec.exec(...)` / `exec.getExecOutput(...)` calls.

`@actions/exec` splits the command string by spaces, so values containing spaces silently break argument boundaries. Use a static command string and pass all arguments in the `args` array instead, for example `exec.exec("git", ["checkout", branchName])`.

**Detected forms:**
- `` exec.exec(`git checkout ${branchName}`) `` — interpolated template literal.
- `exec.exec("git " + branchName)` — dynamic string concatenation.
- `` exec.exec(`git checkout ${branchName}`.trim()) `` — chained string-normalizing methods (`trim`, `trimStart`, `trimEnd`, `toLowerCase`, `toUpperCase`, `toLocaleLowerCase`, `toLocaleUpperCase`, `replace`, `replaceAll`, `normalize`) are unwrapped before the check.
- `` exec.exec("git checkout PLACEHOLDER".replace("PLACEHOLDER", () => branchName)) `` — a `.replace()` / `.replaceAll()` replacer callback's return value is also inspected.

**Not flagged:**
- Static command strings, including string concatenation of only static expressions and chained string-normalizing methods on them.
- Arguments passed correctly via the `args` array.

### `no-setfailed-then-exit-zero`

Disallow resetting the exit code to success (`process.exit(0)` or `process.exitCode = 0`) after `core.setFailed()`.

Doing so silently resets the exit code to success, hiding the failure that `core.setFailed()` already recorded.

**Detected forms:**
- `core.setFailed(msg); process.exit(0);`
- `core.setFailed(msg); process.exitCode = 0;`

The rule provides an autofix suggestion: replace `process.exit(0)` with `return;`, or remove the `process.exitCode = 0;` assignment.

### `no-err-stack-then-string-fallback`

Disallow the `err.stack || String(err)` (and equivalent) fallback pattern for formatting caught errors.

Prefer `getErrorMessage(err)` from `error_helpers.cjs`. The `err.stack` ternary/logical-OR pattern surfaces noisy stack frames; `getErrorMessage()` returns a clean, consistent message.

**Detected forms:**
- `err && err.stack ? err.stack : String(err)`
- `err instanceof Error ? err.stack : String(err)`
- `err.stack || String(err)`

The rule provides an autofix suggestion that replaces the pattern with `getErrorMessage(err)` (ensure `getErrorMessage` is imported from `error_helpers.cjs` before applying).

### `no-string-fallback-for-non-string-message`

Disallow `typeof <x>.message === "string" ? <x>.message : String(<container>)` when the fallback stringifies a different container object instead of the `.message` value itself.

Why: when `.message` exists but is non-string (for example an object), stringifying the container often yields `"[object Object]"` and loses the intended message value.

**Flagged form:**
```js
return typeof err.message === "string" ? err.message : String(err);
```

**Safe alternative:**
```js
return typeof err.message === "string" ? err.message : String(err.message);
```

### `no-caught-error-interpolation`

Disallow directly interpolating a caught error variable in a template literal (for example `` `Failed: ${err}` ``).

Directly interpolating a caught error is unsafe — for `Error` objects it produces `"Error: message"` (a redundant prefix); for non-`Error` throws it produces `"[object Object]"`. Use `${getErrorMessage(err)}` if it is available, or `${String(err)}` as an import-free alternative.

**Not flagged:**
- Error variables passed through `getErrorMessage(...)` or `String(...)` before interpolation.
- Identifiers that are not caught-error bindings (not bound by a `catch` clause, an inline `.catch()`/`.then()` rejection handler, or an inline `'error'` event listener).

The rule provides autofix suggestions to wrap the interpolated expression in `getErrorMessage(err)` (when resolvable in scope) or `String(err)` (import-free fallback).

### `no-core-error-then-setfailed`

Disallow a redundant `core.error()` call immediately before `core.setFailed()` with the same message.

`core.error()` immediately before `core.setFailed()` with the same message is redundant: `core.setFailed()` already logs an error annotation and marks the action failed.

The rule provides an autofix suggestion that removes the redundant `core.error()` call.

### `require-escaped-regexp-interpolation`

Require interpolated values inside a `new RegExp()` template literal to be passed through a regex-escaping helper (or already marked as escaped) before use.

Interpolating an unescaped, user- or runtime-controlled value directly into a `new RegExp(...)` template literal allows regex metacharacters (`. * + ? ^ $ { } ( ) | [ ] \`) in that value to change the meaning of the pattern, which can cause unintended matches or a ReDoS-prone expression.

**Detected forms:**
- `` new RegExp(`^${value}$`) `` — interpolated identifier not passed through an escaping helper and not obviously pre-escaped.

**Not flagged:**
- `` new RegExp(`^${escapeRegExp(value)}$`) `` — interpolated value passed through a call whose name matches an escaping-helper pattern (contains both "escape" and "reg", e.g. `escapeRegExp`, `utils.escapeRegex`).
- `` new RegExp(`^${escapedValue}$`) `` — interpolated identifier whose name starts with `escaped` (e.g. `escapedValue`, `ESCAPED_NAME`).
- Static (non-interpolated) template literals.

### `no-json-stringify-equality`

Disallow comparing two `JSON.stringify()` results with an equality operator (`===`, `!==`, `==`, `!=`). `JSON.stringify()` output depends on object key insertion order, so two deeply-equal objects built with keys inserted in a different order serialize to different strings and are reported as unequal.

**Flagged form:**
```js
return JSON.stringify(normalizedLeft) === JSON.stringify(normalizedRight);
```

**Safe alternative:**
```js
return deepEqual(normalizedLeft, normalizedRight);
```

**Not flagged:**
- `JSON.stringify(value) === "{}"` — only one operand is a `JSON.stringify()` call, so no key-order ambiguity exists.
- Non-equality operators such as `<`, `>` or `+`.
- A locally shadowed `JSON` binding.

### `no-json-stringify-set-or-map`

Disallow `JSON.stringify()` directly on `Set` and `Map` instances. Their entries are not own enumerable properties, so serialization silently produces `{}`.

**Flagged form:**
```js
const serverNames = new Set(["api", "web"]);
JSON.stringify(serverNames);
```

**Safe alternatives:**
```js
JSON.stringify([...serverNames]);
JSON.stringify(Object.fromEntries(cache));
```

### `require-nan-check-after-split-index-parse`

Require NaN validation after parsing a value selected from `split(...)[index]`. A malformed delimited string can otherwise silently pass `NaN` to downstream API calls.

**Flagged form:**
```js
const discussionNumber = parseInt(endpoint.split(":")[1], 10);
getDiscussionNodeId(owner, repo, discussionNumber);
```

**Safe alternative:**
```js
const discussionNumber = parseInt(endpoint.split(":")[1], 10);
if (Number.isNaN(discussionNumber)) throw new Error("invalid discussion number");
getDiscussionNodeId(owner, repo, discussionNumber);
```

### `require-error-code-in-thrown-error`

Require errors thrown in files that import `error_codes.cjs` to include an imported standardized `ERR_*` code. This keeps error-code coverage consistent for log and dashboard filtering.

**Flagged form:**
```js
const { ERR_API } = require("./error_codes.cjs");
throw new Error("failed to fetch");
```

**Safe alternative:**
```js
const { ERR_API } = require("./error_codes.cjs");
throw new Error(`${ERR_API}: failed to fetch`);
```

### `require-error-code-for-github-api-throw`

In files that already import `./error_codes.cjs`, require `throw new Error(...)` messages to include a standardized code when an earlier call in the same function uses `githubClient.rest.*`, `.paginate(...)`, or `.graphql(...)`.

**Flagged form:**
```js
const { ERR_API } = require("./error_codes.cjs");
await githubClient.rest.pulls.get({ owner, repo, pull_number });
throw new Error("failed to fetch pull request");
```

**Safe alternative:**
```js
const { ERR_API } = require("./error_codes.cjs");
await githubClient.rest.pulls.get({ owner, repo, pull_number });
throw new Error(`${ERR_API}: failed to fetch pull request`);
```

### `require-invalid-date-check-before-compare`

Require validation of `new Date(...)` and `Date.parse(...)` results before relational comparisons. Invalid dates and NaN timestamps compare as neither greater nor less than other values, silently defeating time-window checks.

**Flagged form:**
```js
const createdAt = new Date(run.created_at);
if (createdAt < cutoff) archive(run);
```

**Safe alternative:**
```js
const createdAt = new Date(run.created_at);
if (Number.isNaN(createdAt.getTime())) throw new Error("invalid created_at");
if (createdAt < cutoff) archive(run);
```

### `require-sync-exec-timeout`

Require `execSync`, `execFileSync`, and `spawnSync` calls from `child_process` to use a positive `timeout`. Without one, a hung child process can block the action until the job-level timeout kills it.

**Flagged form:**
```js
const { execSync } = require("child_process");
execSync("git status");
```

**Safe alternative:**
```js
const { execSync } = require("child_process");
execSync("git status", { timeout: 5_000 });
```

### `require-lastindex-reset-before-global-exec-loop`

Require module-scoped global or sticky regexes to reset `.lastIndex` before a `while ((match = RE.exec(text)))` loop. Stateful regexes otherwise resume from a previous invocation and can skip matches.

**Flagged form:**
```js
const RE = /foo/g;
while ((match = RE.exec(text)) !== null) process(match);
```

**Safe alternative:**
```js
const RE = /foo/g;
RE.lastIndex = 0;
while ((match = RE.exec(text)) !== null) process(match);
```

### `require-page-counter-increment-in-while-true-loop`

Require a numeric page counter immediately preceding a terminating `while (true)` pagination loop to be incremented or reassigned when used by the loop.

**Flagged form:**
```js
let page = 1;
while (true) {
  const { data } = await github.rest.issues.listComments({ page });
  if (data.length === 0) break;
}
```

**Safe alternative:**
```js
let page = 1;
while (true) {
  const { data } = await github.rest.issues.listComments({ page });
  if (data.length === 0) break;
  page++;
}
```

### `require-http-response-error-listener`

Require the response object passed to `http.request()` / `http.get()` / `https.request()` / `https.get()` callbacks to register an `'error'` event listener.

Why: Node emits `'error'` on the `IncomingMessage` (the response) — not on the request — for socket-level failures that occur while the body is streamed, such as reset connections, decompression failures, or aborted sockets. A `req.on("error", ...)` listener does not catch those, so the unhandled response `'error'` event becomes an uncaught exception that crashes the action.

**Flagged form:**
```js
const http = require("http");
const req = http.request(options, res => {
  let data = "";
  res.on("data", chunk => {
    data += chunk;
  });
  res.on("end", () => resolve(data));
});
req.on("error", reject);
```

**Safe alternative:**
```js
const http = require("http");
const req = http.request(options, res => {
  let data = "";
  res.on("data", chunk => {
    data += chunk;
  });
  res.on("end", () => resolve(data));
  res.on("error", reject);
});
req.on("error", reject);
```

**Out of scope:**
- `http`/`https` identifiers that are not statically bound through `require("http")` / `require("https")` / `require("node:http")` / `require("node:https")`, including bindings created by a locally shadowed `require` or reassigned after initialization
- Request calls without a response callback, or callbacks whose response parameter is destructured
- `fetch`-based HTTP calls (covered by `require-fetch-try-catch` and `require-fetch-timeout`) and non-standard HTTP client libraries

### `require-getexecoutput-exitcode-check`

Require the `exitCode` returned by `@actions/exec`'s `getExecOutput()` or the exit code returned by `exec()` to be read (destructured, accessed, or captured) whenever the call passes `{ ignoreReturnCode: true }`.

Why: `getExecOutput()` and `exec()` throw automatically on a non-zero exit code by default. Passing `ignoreReturnCode: true` suppresses that behavior, making the caller solely responsible for detecting failure. Discarding `exitCode` or the returned exit code (e.g. only destructuring `{ stdout }` from `getExecOutput()`, or a bare `await exec.exec(...)` statement whose returned exit code is never captured) silently swallows command failures — the action proceeds with empty or stale output as if the command had succeeded.

**Flagged form:**
```js
const { stdout } = await exec.getExecOutput("git", ["diff", "--name-only"], { ignoreReturnCode: true });
return stdout.split("\n");

await exec.exec("git", ["diff", "--exit-code", "."], { ignoreReturnCode: true });
```

**Safe alternative:**
```js
const { stdout, exitCode } = await exec.getExecOutput("git", ["diff", "--name-only"], { ignoreReturnCode: true });
if (exitCode !== 0) {
  throw new Error(`git diff failed with exit code ${exitCode}`);
}
return stdout.split("\n");

const exitCode = await exec.exec("git", ["diff", "--exit-code", "."], { ignoreReturnCode: true });
if (exitCode !== 0) {
  throw new Error(`git diff failed with exit code ${exitCode}`);
}
```

**Out of scope:**
- Calls without `ignoreReturnCode: true` in a statically-inspectable options object (the default throw-on-failure behavior already surfaces failures)
- Options whose `ignoreReturnCode` value can't be statically resolved: a non-object-literal identifier (`options`), an options literal built only from spreads (`{ ...opts }`), or one where a spread follows the flag (`{ ignoreReturnCode: true, ...opts }`). An explicit `ignoreReturnCode: true` written after a spread (`{ ...opts, ignoreReturnCode: true }`) is resolvable and still checked
- Results forwarded to a helper function that checks `exitCode` internally, or destructured into an array pattern

### `prefer-actions-exec-over-child-process`

Prefer `@actions/exec`'s `exec()` / `getExecOutput()` over `child_process`'s `exec()`, `execSync()`, `execFile()`, and `execFileSync()` to spawn processes.

Why: modules loaded by `actions/github-script` steps already provide the `@actions/exec` toolkit (bound to `exec` and passed through by `setupGlobals`) without needing an extra dependency. `child_process.exec()` / `execSync()` / `execFile()` / `execFileSync()` all run a command to completion and return or capture its output — exactly what `@actions/exec`'s `exec()` and `getExecOutput()` already do, with consistent GitHub Actions logging, cross-platform argument handling, and (for `getExecOutput()`) throw-on-non-zero-exit behavior built in.

**Flagged form:**
```js
const { execFileSync } = require("child_process");
const branch = execFileSync("git", ["rev-parse", "--abbrev-ref", "HEAD"], { encoding: "utf8" }).trim();
```

**Safe alternative:**
```js
const { stdout } = await exec.getExecOutput("git", ["rev-parse", "--abbrev-ref", "HEAD"]);
const branch = stdout.trim();
```

`promisify()`-wrapped bindings are resolved too, so `const execAsync = promisify(exec); await execAsync("git status");` is flagged as well. Since a promisified `exec()` / `execFile()` resolves to the captured output rather than a `ChildProcess` handle, the handle-retention exemption below does not apply to those calls.

**Scope:** only files carrying the `/// <reference types="@actions/github-script" />` triple-slash reference are checked. That marker is how `actions/setup/js` identifies modules loaded by `actions/github-script` steps, which are the only ones guaranteed to have the `exec` global. The directory also contains standalone Node entry points (for example the mcp-scripts MCP server) and the modules they load; those processes never get `setupGlobals()`-injected toolkit globals — `shim.cjs` only backfills `core` and `context`, not `exec` — so they carry no marker and the rule stays silent there.

**Out of scope:**
- `child_process.spawn()` and `child_process.spawnSync()` — used for long-running, detached, or interactively-streamed processes (background servers, sidecars, and similar) for which `@actions/exec` has no equivalent, since `exec()` / `getExecOutput()` always wait for the command to finish before resolving
- `exec()` / `execFile()` calls that retain the returned `ChildProcess` handle (used as a value: assigned, returned, member-accessed, passed to another call, ...) — those callers can write to `child.stdin`, stream `child.stdout`, or manage the process lifecycle, which `@actions/exec` cannot express; only calls whose result is discarded (pure callback style) are flagged
- Calls to `exec`/`execSync`/`execFile`/`execFileSync` from any module other than `child_process` (or `node:child_process`)
