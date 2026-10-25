---
title: Workflow Linters
description: Built-in Actions/AW lint rules, compiler threat references, analysis boundaries, and performance diagnostics.
---

`gh aw lint` checks authored AW Markdown and parsed Actions YAML without recompilation. See the [CLI reference](/gh-aw/setup/cli/#lint) for command options and file discovery.

## Built-in rules

The compiler threat IDs reference the [Compiler Threat Detection Specification](https://github.com/github/gh-aw/blob/main/specs/compiler-threat-detection-spec.md). Each mapping covers only the observable checks described here, not full conformance to the threat rule. Extension rules can omit a threat ID when no mapping is known.

| Built-in rule | Threat ID | ASTs | Checks |
|---|---|---|---|
| `action-pin` | `CTR-002` | AW, Actions | All action and reusable-workflow references are well-formed literals without expressions, control characters, or whitespace. Remote references use full commit SHAs; Docker actions use digests. Local paths are exempt from immutable pinning, not format validation. |
| `job-timeout` | `CTR-026` | AW, Actions | An explicit workflow/job timeout is a positive integer literal, not an expression. Omission is allowed. |
| `needs-consistency` | — | AW, Actions | Dependencies name existing jobs without self-edges or cycles. Literal `needs` accesses name direct dependencies and declared job outputs; prompt references must be available during activation. Ordering-only dependencies are valid. |
| `template-injection` | `CTR-006` | AW, Actions | Known attacker-controlled expressions are not interpolated into executable `run` or `actions/github-script` code. Use environment bindings and quoted shell expansions or `process.env` instead. |
| `workflow-run-scope` | `CTR-021` | AW, Actions | `workflow_run` selects workflows and has a positive branch scope, not just wildcard, negated-only, or empty branch filters. |
| `secret-leak` | `CTR-017` | AW, Actions | Symbolic secrets do not reach modeled disclosure sinks. AW-only checks report missing generated execution coverage. |
| `otlp-secret-leak` | `CTR-017` | AW, Actions | Secret-tainted expressions are not exported through OTLP span/resource attributes. Actions AST analysis follows modeled environment bindings; AW-only analysis checks direct references. Does not detect hard-coded credentials or arbitrary runtime values. |
| `permission-scope` | `CTR-001` | AW, Actions | Explicit permissions avoid blanket `write-all` grants; OIDC does not use invalid `id-token: read`. Scoped writes and `id-token: write` are not automatically violations. |
| `version-check` | `CTR-018` | AW | Authored source does not disable `check-for-updates`. |
| `conditional-import` | `CTR-020` | AW | Configuration import entries do not contain `if`, even if false or null. Runtime prompt conditionals are unaffected. |
| `private-to-public` | `CTR-031` | AW | GitHub tool configuration does not opt out of private-to-public flow protections, globally or for selected servers. |
| `install-scripts` | `CTR-014` | AW | Authored Node configuration does not enable dependency install hooks. Other runtimes and arbitrary shell commands are not inferred. |
| `allowed-labels` | `CTR-015` | AW | Issue, discussion, discussion-update, and PR creation label allowlists do not contain a bare `*`. Narrow patterns and literal required-label names are unaffected. |
| `network-scope` | `CTR-011` | AW | Network allowances do not contain a bare `*`, including alongside `defaults`. Domain wildcards such as `*.example.com` are allowed; omission does not imply missing firewall protection. |
| `push-target` | `CTR-012` | AW | A literal wildcard PR-branch push target has an authored title or label constraint. Respects deprecated aliases and defers when unresolved imports may supply constraints. |
| `tool-scope` | `CTR-003` | AW | Shell refusal does not conflict with CLI/gh proxies; `min-integrity: none` declares shell intent; cache memory avoids repository-wide scope. Missing prerequisites defer when imports may supply them. |
| `sandbox-config` | `CTR-004` | AW | The agent sandbox is not explicitly disabled and internal agent/MCP command, environment, argument, image, or version overrides are not enabled. Empty overrides are ignored. |
| `image-argument` | `CTR-013` | AW, Actions | Job/service and authored MCP container image operands do not start with `-`. Does not inspect registries or infer dynamically computed images. |
| `checkout-mode` | `CTR-033` | AW | Repository-list expansion is explicitly boolean; literal lists require multi mode, which requires a literal non-root prefix and scoped allowlist without LFS, submodules, current identity, or wiki expansion. Does not prove runtime selector trust or repository visibility. |
| `fork-association` | `CTR-034` | AW | Explicit fork admission uses GitHub's uppercase author-association enum. Empty lists and repository wildcard patterns are valid; generated admission guards are not reconstructed. |
| `pull-request-target-roles` | `CTR-008` | AW | `pull_request_target` does not admit explicit `read` or `all` roles. Checkout disablement and risk acknowledgment do not bypass this policy. Does not infer checkout safety or generated authorization. |
| `pull-request-target-checkout` | `CTR-008` | AW | Checkouts on `pull_request_target` use supported base-repository/ref forms or exact allowed literal repository/ref pairs, without additional fetches. Omission is not `checkout: false`. Does not resolve imported checkout policy or infer generated provenance. |
| `prompt-expression` | `CTR-010` | AW | Authored prompt interpolations are valid, single-line Actions expressions without blocked JavaScript property traversal through dot or literal-index access. Does not implement the compiler's complete context allowlist or inspect imported prompts. |
| `gateway-dispatch-literals` | — | AW | Every key and value inside `on.gateway-dispatch` is literal. Rejects strings containing `${{`, including incomplete expressions, throughout nested objects and lists. Shares the compiler's mandatory check before gateway normalization/lowering. Expressions outside this section and compiler-generated dispatch metadata are unaffected. |
| `safe-output-shell-expansion` | `CTR-009` | AW | Authored `safe-outputs.steps[*].run` scripts avoid the shell expansion patterns blocked by the compiler/harness: parameter transformation, indirect expansion, command substitution, and backticks. Arithmetic and ordinary variables remain allowed. |
| `firewall-ssl-bump` | `CTR-011` | AW | Nonempty literal `--allow-urls` in `sandbox.agent.args` requires `--ssl-bump` in the same list. Unsupported legacy `network.firewall` blocks require migration, even with `ssl-bump: true`. Does not prove runtime HTTPS enforcement. |
| `custom-job-detection` | `CTR-005` | AW, Actions | Custom jobs run before `agent` or after successful `detection`, including transitive dependencies. Checks every `||` branch when a status function overrides Actions' implicit success gate. Contract-valid leaf jobs with `detection-gate: status-only` may report job results after detection failure. Compiler-owned jobs and disabled detection are exempt. |
| `grader-file-io` | — | AW | Custom `graders.<id>.script` JavaScript ASTs do not access filesystem/host capabilities or load modules. Requires the optional native JavaScript parser; missing parser configuration produces a visible coverage warning. |
| `grader-shell-file-write` | — | AW | Repository-owned `graders.operational-value.run` Bash ASTs avoid detected file writes and unchecked shell execution. Loads confined evaluator source without executing it; no optional JavaScript parser required. |
| `artipacked` | — | AW, Actions | `actions/checkout` explicitly disables credential persistence. Omitted, enabled, or dynamic persistence produces a review warning; later authenticated Git operations may require it. During compilation only, exact compiler-generated blocks can exempt PR checkouts in `safe_outputs` whose trusted handlers require retained credentials, or agent checkouts immediately followed by compiler-owned credential cleanup and verification. Authored steps and standalone `gh aw lint` checks remain unchanged. |
| `cache-poisoning` | — | AW, Actions | Release-event and tag-push workflows avoid cache restoration through `actions/cache`, `actions/cache/restore`, `actions/setup-go`, and `actions/setup-node`. Save-only and lookup-only actions are exempt. |
| `excessive-permissions` | `CTR-001` | Actions | Workflow-wide write grants are moved to the jobs that require them. Job-level writes and AW root grants are not workflow-wide permissions. |
| `obfuscation` | — | AW, Actions | Runner-interpolated fields avoid literal-only string expressions such as `${{ '' }}`. Ordinary expressions, fallback strings, and boolean conditions are unaffected. |

### Historical zizmor regressions

These checks build on the security research, audit definitions, rule names, and remediation guidance of [zizmor](https://github.com/zizmorcore/zizmor), an [MIT-licensed](https://github.com/zizmorcore/zizmor/blob/main/LICENSE) security scanner maintained by the zizmor maintainers and contributors. Credit for the upstream audits belongs to that project; the linked repository fixes below are gh-aw's regression evidence, not the origin of those audits.

gh-aw independently implements selected checks in Go against its Actions/AW ASTs, without embedding or invoking zizmor or fetching action metadata. The upstream [audit documentation](https://docs.zizmor.sh/audits/) is authoritative for zizmor's behavior; gh-aw's coverage and severity choices are documented here and do not imply parity or upstream endorsement.

| Historical finding | Fix evidence | AST coverage |
|---|---|---|
| Persisted checkout credentials ([`artipacked`](https://docs.zizmor.sh/audits/#artipacked)) | [#2991](https://github.com/github/gh-aw/pull/2991), [#14449](https://github.com/github/gh-aw/pull/14449), [#25873](https://github.com/github/gh-aw/pull/25873) | `artipacked` checks explicit checkout steps, including imported setup steps once present in the compiled graph. |
| Release dependency-cache poisoning ([`cache-poisoning`](https://docs.zizmor.sh/audits/#cache-poisoning)) | [#5214](https://github.com/github/gh-aw/pull/5214), [#5872](https://github.com/github/gh-aw/pull/5872), [#19865](https://github.com/github/gh-aw/pull/19865) | `cache-poisoning` checks enabled and default Go/Node caching on release/tag workflows. |
| Overbroad permission placement ([`excessive-permissions`](https://docs.zizmor.sh/audits/#excessive-permissions)) | [#5229](https://github.com/github/gh-aw/pull/5229) | `excessive-permissions` checks workflow-wide writes; `permission-scope` already checks blanket grants. Authored AW agent-write authorization is not inferred. |
| Literal-expression obfuscation ([`obfuscation`](https://docs.zizmor.sh/audits/#obfuscation)) | [#4398](https://github.com/github/gh-aw/pull/4398), [#14257](https://github.com/github/gh-aw/pull/14257) | `obfuscation` parses expression roots in outputs, environment bindings, and other runner fields. |
| Executable template injection ([`template-injection`](https://docs.zizmor.sh/audits/#template-injection)) | [#2871](https://github.com/github/gh-aw/pull/2871), [#9147](https://github.com/github/gh-aw/pull/9147), [#25888](https://github.com/github/gh-aw/pull/25888) | `template-injection` retains shell/heredoc checks and also inspects `actions/github-script` inputs, without requiring a JavaScript parser. |
| Mutable action references ([`unpinned-uses`](https://docs.zizmor.sh/audits/#unpinned-uses)) | [#2686](https://github.com/github/gh-aw/pull/2686), [#55641](https://github.com/github/gh-aw/pull/55641) | Existing `action-pin` checks remain authoritative; no duplicate pinning rule is registered. |

The four new rules produce warnings in both strict and non-strict mode. They identify reviewable configuration, not proven credential disclosure or cache compromise. Checkout checks do not inspect artifact contents or Git authentication requirements. Cache checks do not infer publishing from job names, resolve cache provenance, or prove job conditions. Known pre-v4 Go and pre-v5 Node tags retain their disabled automatic-cache defaults; SHA-pinned versions with unknown defaults require an explicit cache opt-out to avoid warnings. Explicit Node `cache` still enables restoration even with `package-manager-cache: false`.

AW checks inspect explicit execution blocks, including `safe-outputs.steps`, custom jobs, and post-steps, but do not load imports or reconstruct generated defaults. When a compiled Actions AST is supplied, it owns execution checks; the companion AW document does not duplicate steps removed or transformed by compilation. Obfuscation checks skip prompts, grader code, and unrelated AW configuration. Invalid expressions or malformed action inputs fail analysis with value-free errors while retaining findings from valid scopes.

This is not complete zizmor parity. Creator-verification findings such as [#50015](https://github.com/github/gh-aw/pull/50015) require external provenance data; static ASTs do not establish creator trust. Existing specialized trigger and permission rules remain separate, and zizmor ignore comments are not AST-rule suppressions.

`otlp-secret-leak` reports direct secret references in authored `observability.otlp.attributes` and `resource-attributes`, and follows modeled environment flows to Actions `GH_AW_OTLP_ATTRIBUTES` and `OTEL_RESOURCE_ATTRIBUTES` bindings. Diagnostics identify only the configuration path, never the value. The check is advisory; it does not inspect literal credentials, arbitrary runtime-generated values, or collector trust.

## Severity and source policy

Action pinning, workflow-run branch scope, blanket write permissions, disabled version checking, private-to-public opt-outs, enabled Node install hooks, and bare network wildcard allowances are errors for an AW document with strict mode enabled (the AW default), and warnings for explicit non-strict AW. Native Actions policy-sensitive findings are warnings; AW-only rules skip native Actions without an AW document. Conditional imports, bare label-allowlist wildcards, invalid OIDC read grants, missing or malformed `workflow_run.workflows`, invalid explicit timeouts, and direct template injection are errors. Unconstrained wildcard push targets remain warnings even in strict mode. The linter does not reconstruct CLI/repository strict overrides, load imported configurations, infer missing permissions, or determine whether each repository-write grant is authorized.

Malformed `uses` references are always `action-pin` errors, including native Actions and non-strict AW. This includes invalid repository/path syntax, missing refs, malformed image digests, expressions, invalid UTF-8, control characters, and Unicode whitespace or line separators. Valid but mutable tags, branches, and abbreviated SHAs retain the policy-sensitive severity above. Single-quoted, double-quoted, and plain references are checked after YAML parsing; version labels belong in YAML comments outside the scalar. Comments and imports not present in the AST are not analyzed. Check either format with `gh aw lint --rules action-pin .github/workflows/example.yml` or the corresponding `.md` source.

Tool-scope and sandbox-config use the authored strict-policy error/warning distinction, including explicit non-strict sandbox opt-outs and internal customizations. Image-argument, checkout-mode, and fork-association findings are always errors. Checkout and tool checks defer absent prerequisites that unresolved imports may supply; they still report explicit contradictions. Image checks cover image operands, not inferred package-manager arguments.

Pull-request-target-roles rejects unsafe role admission in strict mode and warns in non-strict mode. Only the non-strict warning can be suppressed with boolean `features.dangerously-allow-roles-read-and-pull-request-target: true`; this does not suppress other checks. Safe-output-shell-expansion and firewall-ssl-bump are errors in either mode. The firewall rule inspects authored AWF arguments; `sandbox.agent.args` is an internal customization rejected in strict mode. The shell rule shares the compiler's lexical policy, including matches in quoted text or comments, and reports script-relative lines without script contents. It does not apply to ordinary Actions steps or infer safe-output jobs from their names.

Pull-request-target-checkout is an error in strict mode and a warning in non-strict mode. Neither trigger-risk acknowledgment nor the role-warning opt-out suppresses it. Base checkouts accept the compiler's base repository and omitted/base-SHA/base-ref forms. External pairs must match `on.pull_request_target.allowed-checkouts` exactly, including case, and cannot use expression or wildcard refs, `refs/pull/...`, omitted refs, wiki expansion, or additional `fetch` values. Missing checkout configuration, missing literal pair operands, and otherwise eligible pairs without an authored allowlist defer when unresolved imports may supply policy; explicit unsafe refs and fetches still report.

Prompt-expression findings are errors in either mode and include prompt-relative line numbers, never expression contents. The shared Actions lexer handles quoted `}}` delimiters. Blocked property names follow case-insensitive Actions semantics; string literals containing those names are not property accesses. Computed property values, frontmatter expressions, imported content, and the full context allowlist remain outside this rule. A malformed interpolation reports an error and stops further prompt traversal while retaining earlier findings.

Gateway-dispatch-literals findings are errors in either strict mode and identify the offending configuration path without including expression contents. Run `gh aw lint --rules gateway-dispatch-literals .github/workflows/example.md` to check declarations without compiling. Normal compilation enforces the same rule even when lint is not run; the rule inspects authored gateway declarations, not the lowered Actions `repository_dispatch` listener.

Firewall argument checks inspect separate `--allow-urls`, URL operands and `--allow-urls=...` tokens before an option terminator (`--`). A literal `--ssl-bump` or `--ssl-bump=true` satisfies the lint prerequisite unless followed by `--ssl-bump=false`; the last SSL setting wins. The rule does not interpret arbitrary command strings, resolve imports, or validate runtime AWF option semantics. Internal `sandbox.agent.args` overrides require `strict: false`; adding SSL inspection does not make them valid in strict mode. A non-strict example is:

```aw wrap
strict: false
sandbox:
  agent:
    args: [--ssl-bump, --allow-urls, "https://example.com/path"]
```

The compiler rejects `network.firewall` regardless of its SSL setting. Remove that legacy block and use `network.allowed` for domain policy; do not add `network.firewall.ssl-bump` as a remedy.

### Needs consistency

`needs-consistency` reports warnings in either strict mode. Dot and literal bracket accesses, including bare job and step `if` conditions, are parsed as Actions expressions. A transitive dependency is not available in `needs`: the consumer must declare the producer directly. Job properties are `result` and `outputs`. Output accesses require a producer `outputs` entry; reusable-workflow outputs are exempt because their declarations are external. Native Actions parsing already rejects missing dependency targets and cycles before linting.

The compiled Actions graph takes precedence over companion AW source. AW-only checks cover explicit `jobs`, `safe-jobs`, and `safe-outputs.jobs`, normalize safe-job dependency IDs, and defer missing imported or compiler-generated producers and outputs. Compiler-owned scopes and safe-job defaults are not reconstructed. Imports also defer missing direct edges that may be inherited. Root execution scopes require the compiled graph for dependency analysis. Wildcard projections, whole-context reads, and computed indexes do not select a statically known producer or output; their nested literal accesses are still checked. Dependencies used only for ordering are not treated as unused.

Prompt checks use activation's dependencies and report prompt-relative lines. Authored AW analysis recognizes independent prompt producers, `on.needs`, and jobs that depend on `pre_activation` without depending on `activation`. A reference such as `${{ needs.release.outputs.release_id }}` is flagged when `release` runs after activation. Prefer reading data staged by agent steps; use `on.needs` only for producers that can safely run before activation. The compiler automatically defers direct, declared custom-job output and result interpolations to the agent job when their producer runs between activation and the agent. Compiled graph analysis recognizes those late bindings and checks them against the agent's dependencies. Compound expressions and template conditions are not late-bound; stage their data explicitly instead.

Bare `if` and `elseif` template conditions receive the same dependency and declared-output checks as interpolations. Prompt syntax errors remain the responsibility of `prompt-expression` and do not suppress independent job-graph diagnostics from `needs-consistency`.

```bash wrap
gh aw lint --rules needs-consistency .github/workflows/example.md
```

### Custom job detection gates

`custom-job-detection` is an error in either strict mode. Custom jobs must run before `agent` or after `detection`, never between them. A custom job downstream of `agent`, including through another custom job or a compiler-owned post-agent job, must directly need `detection`. Jobs that need only `detection` are checked too. Without a status function, Actions implicitly requires all dependencies to succeed. Conditions using `always()`, `failure()`, or `cancelled()` must additionally require `needs.detection.result == 'success'` on every runnable branch:

```aw wrap
jobs:
  report:
    needs: [agent, detection, scan]
    if: always() && needs.detection.result == 'success' && needs.scan.result == 'failure'
    runs-on: ubuntu-latest
    steps:
      - run: echo "Report a failed scan after successful threat detection"
```

`needs.detection.result != 'skipped'` is insufficient: failed detection must block the job too. The rule parses Actions expressions rather than matching text; quoted guards and an unguarded `||` branch do not satisfy it. Complex equivalent predicates that cannot be proven safe should use the explicit equality guard.

For a notification that must run after failed or warn-mode detection, a downstream leaf custom job may set `detection-gate: status-only`. The compiler preserves its authored condition and adds `detection` to `needs`, but rejects artifact downloads, references to non-detection-gated job outputs, `GH_AW_AGENT_OUTPUT`, agent/safe-output file paths, and opaque reusable workflow calls. It permits job results and outputs from compiler-owned jobs gated on detection, such as `safe_outputs`. The linter reports violations of the same contract.

The compiler independently adds a direct `detection` dependency and an outer guard requiring both `needs.detection.result == 'success'` and `needs.detection.outputs.detection_success == 'true'` to downstream custom jobs, including reusable workflow calls. This blocks failed, cancelled, skipped, or missing detection results and warn-mode findings even when detection's job status is successful. Custom `if` expressions cannot override this guard. A job required by detection that also depends on agent is rejected because adding the guard would create a cycle.

The rule follows transitive dependencies in the supplied graph. It inspects top-level custom `jobs`, not compiler-managed jobs or `safe-outputs.jobs`. When a compiled Actions AST is supplied, its dependencies and conditions are authoritative; a companion AW document identifies which jobs are authored. Without a companion, compiler-owned names, including generated state-push and memory-maintenance jobs, are exempt. This name-based standalone classification does not establish the provenance of arbitrary native Actions jobs. Actions graphs without both `agent` and `detection` are outside this policy. AW-only analysis does not resolve imports; lint shared fragments explicitly as well as their consumers. Fragments assume detection is enabled unless their source explicitly disables it.

Custom jobs cannot create an inactive built-in job such as `evals` by reusing its reserved name. Configure the corresponding feature to customize an existing compiler-generated job, or choose a different custom-job name. Authored jobs using maintenance names remain subject to detection gating when source is available.

### Custom grader file operations

`grader-file-io` rejects file operations in authored custom JavaScript graders in either strict mode. It uses the [native JavaScript parser](#optional-native-javascript-analysis), reports script-relative lines, and never executes grader code:

```bash wrap
gh aw lint --rules grader-file-io --json \
  --javascript-module /path/to/gh-aw/pkg/workflow/taint/javascript \
  /path/to/repository/.github/workflows/example.md
```

The rule rejects module loading, including `require`, static imports, dynamic imports, and relative helper modules that could hide file I/O. It also rejects unbound filesystem and host capabilities such as `fs`, `process`, `module`, Deno/Bun file APIs, and browser file readers or pickers. Aliasing an acquired capability does not avoid its acquisition finding. Ordinary local bindings and object keys named `fs` or `require`, comments, string literals, and recorded file-path data do not constitute file access.

All executable grader script bodies are checked, including disabled definitions, nested functions, parameter defaults, and otherwise unmodeled syntax retained by the parser. Built-in grader configuration, Markdown examples, other script hooks, and native Actions without an AW companion are outside this rule's scope. Lint imported grader fragments explicitly; configuration imports are not resolved.

Without `--javascript-module`, each custom script gets a visible coverage warning rather than a claim that it passed. Once a parser is configured, unavailable or malformed ASTs fail lint. Findings are errors. The rule does not prove arbitrary reflection or dynamically generated code safe and does not replace the isolated grader worker's filesystem, process, and code-generation restrictions. Capture evidence before grading and use only the immutable in-memory `session`, `config`, and `helpers`.

### Operational-value shell file writes

`grader-shell-file-write` is a default AW error rule in either strict mode.
It loads `graders.operational-value.run` from a confined repository `.sh` file
and parses Bash ASTs without executing source. Disabled definitions are checked
too; invalid paths, missing files, and malformed or unanalyzable source fail
explicitly. `./` paths resolve from the declaring workflow file:

```bash
gh aw lint --rules grader-shell-file-write .github/workflows/example.md
```

The checker walks branches, functions, substitutions, and static nested shell
commands. It detects file-writing redirections, destination-bearing `tee`, common
mutation commands, and in-place `sed`/`perl`. Stdout/stderr descriptor duplication,
`/dev/null`, comments, quoted text, and read-only `jq` filters are not file writes.
Dynamic shell execution and sourced helpers are rejected as unchecked. Compiler
validation shares this checker, so bypassing lint does not permit detected writes.

Lint imported fragments explicitly; configuration imports are not recursively
resolved. Diagnostics use evaluator-relative lines, without source contents.
This finite analysis does not model arbitrary interpreter programs or prove
filesystem/network isolation. Bash graders remain trusted code in the separate
job; microVM isolation is future work.

## Secret-flow analysis

The `secret-leak` rule tracks symbolic secret references, `github.token`, and known app-token action outputs through environment scopes, shell assignments, step outputs, and dependent job outputs. It detects disclosures to logs, step summaries, workflow outputs, request URLs/bodies, known publication inputs, cache keys, and artifacts containing modeled secret-bearing files. Authentication headers, token inputs, and the reusable workflow `secrets:` channel are not disclosure sinks. Boolean comparisons and predicates filter secret values; formatting, JSON serialization, encoding, and `add-mask` registration do not sanitize subsequent uses.

In Bash/sh, recognized `test`/`[` string and file predicates and the `:`, `true`, and `false` builtins do not disclose their arguments. Presence-only alternate expansions such as `${TOKEN:+present}` do not contain the token, but secret-bearing replacements remain tainted. Nested command disclosures, parameter-expansion error messages, and `set -x` output are still checked. Numeric comparisons, malformed tests, and potentially word-splitting predicates retain coverage warnings because their error diagnostics can expose operands.

Unknown file reads produce secret-flow coverage warnings when they could select a modeled secret-bearing file or involve a secret-bearing filename. Unknown output targets warn when the output or filename is tainted; unrelated public file operations do not introduce secret-flow warnings.

For `.lock.yml` inputs, a companion `.md` adds authored-policy and prompt checks to the existing execution graph. Native `.yml`/`.yaml` inputs never acquire an assumed AW companion. Explicit `.md` inputs inspect the AW AST independently: the prompt, top-level `steps`/`post-steps`, authored `jobs` and `safe-outputs.jobs` (including legacy `safe-jobs`), and script hooks. No compiled graph is fabricated. Secret analysis retains a coverage warning for missing imports, generated steps, cross-job flows, and compiler defaults; scripts without an observable shell remain unknown.

Analysis is conservative within Bash/sh control flow and can report infeasible paths. It does not execute analyzed code or inspect external actions, sourced shell scripts, Python/PowerShell programs, dynamically constructed shell code, or arbitrary filesystem operations. JavaScript inspection requires the explicit configuration below. Imported runtime prompt files are not recursively loaded. Authentication arguments are treated as intentional credential use; endpoint trust is not inferred. A clean result is not proof that a workflow cannot leak secrets.

## Diagnostics

Text findings include the linter rule ID and its known compiler threat ID, for example `[safe-output-shell-expansion] (CTR-009)`. `--list-rules` includes the same threat ID after the rule description. JSON diagnostics contain the file, rule ID, optional `threat_rule_id`, severity, AST path, optional script-relative line, message, and symbolic source locations, never resolved secrets or script contents. The threat ID is omitted for unmapped rules. Errors produce a nonzero exit status; coverage warnings alone do not.

## Optional native JavaScript analysis

This analysis is experimental and opt-in. `--javascript-module` enables JavaScript secret-flow analysis and the custom-grader file-I/O rule using Microsoft's Go TypeScript compiler through the upstream-exported, **unstable** `@typescript/native-preview/unstable/sync` and AST APIs. The exact supported package version is `7.0.0-dev.20260707.2`. Node.js 22.12 or newer and its platform-native compiler executable must already be installed; the CLI does not download dependencies or assume its own developer `node_modules` directory exists.

For a gh-aw source checkout, install the optional pinned dependency and supply absolute paths:

```bash wrap
npm ci --prefix /path/to/gh-aw/pkg/workflow/taint/javascript --ignore-scripts
gh aw lint --rules secret-leak --json \
  --javascript-module /path/to/gh-aw/pkg/workflow/taint/javascript \
  --javascript-root /path/to/repository
```

The checker follows literal `node`/`nodejs` file and `--eval` invocations, `--print` expressions, `shell: node {0}`, `actions/github-script` scripts, and available relative CommonJS/ES module imports. It tracks symbolic environment/argument/stdin sources, aliases, object members, local function calls, selected stdin callbacks, JSON/string transformations, and known file writes/reads. Object rest preserves member shapes: credential-bearing environment members do not taint an unrelated public timeout, while a secret-derived timeout remains tainted. Logging, summaries, outputs, and non-authentication request fields are disclosure sinks. Comparison/presence results and type-proven public length/index metadata remove values; arbitrary object properties and numeric coercions do not. Genuine escaped, single-record stdout `add-mask` registrations are recognized without sanitizing later uses or captured stdout. Dynamic reads remain explicit gaps rather than invented all-environment return values.

`--javascript-mode fast` is the CLI default when JavaScript is enabled. With a companion AW document, it analyzes authored JavaScript rather than generated runtime wrappers, while retaining the compiled shell, prompt, and job/output checks. Native Actions files without an AW companion retain their explicit JavaScript entry points. Entry sources across Markdown inputs are deduplicated and parsed in bounded native batches; syntax failures are isolated per script. Additional sources discovered through data flow are parsed on demand.

Script hooks are checked at executable schema locations, not by recursively matching every key named `script`:

| Source | Execution contract |
|---|---|
| `mcp-scripts.<tool>.script` / `.run` | JavaScript tool body or Bash; JavaScript returns are outputs to the agent. Every tool inherits the shared server environment: tool env mappings merge in sorted tool-name order, with later values winning. Python and Go retain unsupported-language warnings. |
| `safe-outputs.scripts.<handler>.script` | Invoked handler body and returned output, with `safe-outputs.env` overriding workflow env. |
| `graders.<grader>.script` | Pure custom JavaScript over immutable session data in an isolated downstream job, without filesystem, process, or Node environment access. Encoded grader payloads are not Actions-interpolated. |
| `graders.operational-value.run` | Trusted repository Bash in the separate graders job, reading unified-session JSONL via `$1` / `GH_AW_SESSION_FILE`, JSON config via `GH_AW_GRADER_CONFIG`, and emitting legacy metric arrays. Dedicated Bash AST write checking is independent of optional JavaScript secret-flow analysis. Not yet microVM-isolated. |
| Memory-tool validation hooks | Object/string `validation` and legacy aliases, including list configurations; only the validator's sanitized environment is inherited. Encoded script payloads are not Actions-interpolated. |
| `engine.driver` | Inline Node source or available JavaScript driver files, with `engine.env` and then `sandbox.agent.env` overriding workflow env. Repository driver files are not Actions-interpolated. Other languages and unavailable files retain warnings. |
| `engine.behaviors` | Node harness, invoked `parseLog` function, and MCP configuration adapter. The harness inherits engine execution env; log parsing and MCP configuration run in separate steps without `engine.env`. |

Inline hook Actions expressions use the script-writing step's workflow env; execution-step overrides affect the hook's runtime environment, not those expressions.

Agent inputs, grader payload data, memory contents, imported configuration, and generated runtime configuration are not inferred. These boundaries retain explicit coverage warnings.

Fast mode uses embedded, reviewed API transfer summaries for masking/escaping, OTLP credential collection, Git authentication environment construction, and error helpers. Summaries preserve field-sensitive returns and callback effects; masking is not declassification. Module paths, source SHA-256 hashes, transitive dependency hashes, and the summary-model version must match. Unknown or changed runtime APIs produce coverage warnings rather than silently falling back to expensive traversal. Maintainers must review the contract and dependencies before updating a catalog hash; the source-identity regression test detects drift.

Use `--javascript-mode deep` to traverse available runtime module bodies instead of summaries and to analyze generated JavaScript in paired lock files. Deep mode can be substantially slower and still has unsupported-flow limits. The direct Go API preserves deep behavior when its mode is unset.

`--javascript-root` bounds source reads to the supplied repository, including symlink confinement. Repeated `--javascript-runtime-path 'runner-directory=repository-relative-directory'` flags map generated runtime asset paths to **operator-verified matching source**. For example, only when the workflow uses the exact checkout's gh-aw setup assets, pass `--javascript-runtime-path '${RUNNER_TEMP}/gh-aw/actions=actions/setup/js'`. A mapping does not declare a helper safe: fast mode requires a matching reviewed summary, and deep mode analyzes source. Unknown interpreter paths are not identified by basename.

Missing or incompatible compiler installations, unresolved sources/calls/modules, cycles, unsupported syntax, loop convergence, and traversal/budget limits retain incomplete-analysis warnings. These warnings are visible without `--debug` whenever JavaScript is explicitly enabled. Fast mode also reports excluded generated and cross-job JavaScript flows. Unknown boundaries do not invent transformed stdout or confirmed leaks. Without `--javascript-module`, Bash analysis remains active and records unavailable JavaScript analysis at secret-bearing Node command boundaries; additional JavaScript inspection is opt-in. Findings require review and are not a vulnerability count. An empty errors-only report does not imply complete coverage or whole-program JavaScript safety.

## Linter performance

`DEBUG=workflow:lint:perf` emits per-rule start/completion markers, elapsed nanoseconds, diagnostic counts, and failure status to stderr. Separate markers measure diagnostic sorting/deduplication and total runner time. These markers contain no AST contents, source symbols, or error text and do not change JSON diagnostics or enable coverage warnings:

```bash wrap
DEBUG=workflow:lint:perf DEBUG_COLORS=0 gh aw lint --rules= --json \
  > lint.json 2> lint-performance.log
```

Go execution traces include `workflow:lint`, `workflow:lint:rule:<id>`, and `workflow:lint:diagnostics` regions. Native JavaScript compiler time is included in the calling rule's wall time; the external compiler's CPU work is not part of a Go CPU profile.
