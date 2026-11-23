---
title: Workflow Linters
description: Built-in Actions/AW lint rules, compiler threat references, analysis boundaries, and performance diagnostics.
---

`gh aw lint` checks authored AW Markdown and parsed Actions YAML without recompilation. See the [CLI reference](/gh-aw/setup/cli/#lint) for options and file discovery.

## Built-in rules

Threat IDs refer to the [Compiler Threat Detection Specification](https://github.com/github/gh-aw/blob/main/specs/compiler-threat-detection-spec.md). A mapping covers only the checks listed here. Extension rules may omit a threat ID.

| Built-in rule | Threat ID | ASTs | Checks |
|---|---|---|---|
| `action-pin` | `CTR-002` | AW, Actions | Action and reusable-workflow references are literals without expressions, control characters, or whitespace. Remote references use full commit SHAs; Docker actions use digests. Local paths skip pinning but get format checks. |
| `job-timeout` | `CTR-026` | AW, Actions | Explicit workflow and job timeouts are positive integer literals. Omission is allowed. |
| `needs-consistency` | — | AW, Actions | `needs` names existing jobs without self-edges or cycles. Literal accesses name direct dependencies and declared outputs. Prompt references must be available during activation. Ordering-only dependencies are valid. |
| `step-reference` | — | AW, Actions | Literal `steps` accesses name a step ID declared in the same job, including bare `if` conditions. Output declarations and ordering are not checked. |
| `template-injection` | `CTR-006` | AW, Actions | Known attacker-controlled expressions are not interpolated into executable `run` or reviewed action code inputs, including values transported through modeled shell step and job outputs. Use environment bindings and runtime data access instead. |
| `workflow-run-scope` | `CTR-021` | AW, Actions | `workflow_run` selects workflows and has a positive branch scope, not only wildcard, negated, or empty filters. |
| `dangerous-triggers` | `CTR-008`, `CTR-039` | AW, Actions | Target-trigger warnings and comment-trigger warnings correlated with actor admission and GitHub integrity filtering, including AW commands. Trusted roles, approved/merged filtering without lockdown override, or explicit GitHub disablement suppress the comment finding; literal AW target-risk acknowledgment suppresses only the target warning. |
| `secret-leak` | `CTR-017` | AW, Actions | Symbolic secrets do not reach modeled disclosure sinks. AW-only checks report missing generated execution coverage. |
| `otlp-secret-leak` | `CTR-017` | AW, Actions | Secret-tainted expressions are not exported through OTLP span or resource attributes. Hard-coded credentials and runtime values are not detected. |
| `permission-scope` | `CTR-001` | AW, Actions | Permissions avoid blanket `write-all`; OIDC does not use invalid `id-token: read`. Scoped writes and `id-token: write` are not violations. |
| `version-check` | `CTR-018` | AW | Authored source does not disable `check-for-updates`. |
| `conditional-import` | `CTR-020` | AW | Configuration import entries contain no `if`, even if false or null. Runtime prompt conditionals are unaffected. |
| `private-to-public` | `CTR-031` | AW | GitHub tool configuration does not opt out of private-to-public flow protections, globally or per server. |
| `install-scripts` | `CTR-014` | AW | Authored Node configuration does not enable dependency install hooks. Other runtimes are not inferred. |
| `allowed-labels` | `CTR-015` | AW | Label allowlists for issue, discussion, discussion-update, and PR creation contain no bare `*`. |
| `network-scope` | `CTR-011` | AW | Network allowances contain no bare `*`, including alongside `defaults`. Domain wildcards such as `*.example.com` are allowed. |
| `push-target` | `CTR-012` | AW | A literal wildcard PR-branch push target has an authored title or label constraint. Defers when imports may supply one. |
| `tool-scope` | `CTR-003` | AW | Shell refusal does not conflict with CLI/`gh` proxies; `min-integrity: none` declares shell intent; cache memory avoids repository-wide scope. Defers when imports may supply prerequisites. |
| `sandbox-config` | `CTR-004` | AW | The agent sandbox is not disabled, and internal agent or MCP command, environment, argument, image, or version overrides are not enabled. Empty overrides are ignored. |
| `image-argument` | `CTR-013` | AW, Actions | Job, service, and authored MCP image operands do not start with `-`. Registries and computed images are not inspected. |
| `unpinned-images` | — | AW, Actions | Job and service images use valid immutable digests. Literal matrix alternatives, including `include` values, are checked; other computed images produce an incomplete-analysis warning. |
| `environment-file-injection` | — | AW, Actions | Attacker-controlled data does not flow through modeled Bash/sh redirects or `tee` into `GITHUB_ENV` or `GITHUB_PATH`. Quoting alone does not prevent injected records. Literal writes and boolean-filtered values are allowed. |
| `checkout-mode` | `CTR-033` | AW | Repository-list expansion is an explicit boolean. Literal lists need multi mode, a literal non-root prefix, and a scoped allowlist without LFS, submodules, current identity, or wiki expansion. |
| `fork-association` | `CTR-034` | AW | Explicit fork admission uses GitHub's uppercase author-association enum. Empty lists and repository wildcards are valid. |
| `pull-request-target-roles` | `CTR-008` | AW | `pull_request_target` does not admit explicit `read` or `all` roles. Checkout disablement and risk acknowledgment do not bypass this. |
| `untrusted-roles-detection` | `CTR-005` | AW | `on.roles: all` with a trigger untrusted users can fire must not run with threat detection disabled (`threat-detection: false`, `enabled: false`, or `engine: false` without steps). Triggers only trusted actors can fire (`schedule`, `workflow_dispatch`, `push`, `merge_group`) are exempt; unknown triggers count as untrusted. A `safe_outputs` job without a `detection` job counts as disabled. The finding recommends NVX (`sandbox.agent.runtime: nvx`), which does not exempt the workflow. |
| `pull-request-target-checkout` | `CTR-008` | AW | Checkouts on `pull_request_target` use supported base-repository or ref forms, or exact allowed literal pairs, without extra fetches. Omitting `checkout` is not `checkout: false`. |
| `prompt-expression` | `CTR-010` | AW | Authored prompt interpolations are valid single-line expressions without blocked JavaScript property traversal. The full context allowlist and imported prompts are not checked. |
| `gateway-dispatch-literals` | — | AW | Every key and value in `on.gateway-dispatch` is literal. Strings containing `${{`, including incomplete expressions, are rejected at any depth. |
| `safe-output-shell-expansion` | `CTR-009` | AW | `safe-outputs.steps[*].run` scripts avoid blocked expansions: parameter transformation, indirect expansion, command substitution, and backticks. Arithmetic and ordinary variables remain allowed. |
| `safe-output-credentials` | `CTR-035`–`CTR-038` | AW | `safe-outputs.steps[*]` avoid the job's write credentials: no compiler-minted `steps.*-app-token` outputs, no `$GITHUB_ENV`/`$GITHUB_PATH` writes or `core.exportVariable`/`core.addPath`, no `actions/checkout` without `persist-credentials: false`, and no writes to agent output or patches under `/tmp/gh-aw`. Error in strict mode; warning when `strict: false`. |
| `firewall-ssl-bump` | `CTR-011` | AW | A nonempty literal `--allow-urls` in `sandbox.agent.args` requires `--ssl-bump` in the same list. Legacy `network.firewall` blocks require migration, even with `ssl-bump: true`. |
| `custom-job-detection` | `CTR-005` | AW, Actions | Custom jobs run before `agent` or after successful `detection`, including transitive dependencies. Every `||` branch is checked when a status function overrides the implicit success gate. Leaf jobs with `detection-gate: status-only` may report after detection failure. |
| `grader-file-io` | — | AW | Custom `graders.<id>.script` JavaScript accesses no filesystem or host capabilities and loads no modules. Requires the optional native JavaScript parser; without it, a coverage warning is shown. |
| `artipacked` | — | AW, Actions | `actions/checkout` explicitly disables credential persistence. Omitted, enabled, or dynamic persistence produces a review warning. Compiler-generated blocks can exempt trusted PR checkouts and credential-cleanup patterns when their paths are statically known and at most 13 directory components deep. |
| `cache-poisoning` | — | AW, Actions | `cache-mode` values are literal `read`, `write`, `write-only`, or `none`; omit the key for defaults. Release and tag workflows avoid cache restoration through `actions/cache`, `actions/cache/restore`, `actions/setup-go`, and `actions/setup-node`. Save-only and lookup-only actions are exempt. Effective `cache-mode: write` or `write-only` on events outside GitHub's write allowlist warns. Job names do not establish publisher trust. |
| `excessive-permissions` | `CTR-001` | Actions | Workflow-wide write grants are moved to the jobs that need them. |
| `obfuscation` | — | AW, Actions | Runner-interpolated fields avoid literal-only expressions such as `${{ '' }}`. |
| `bash-case-pattern` | — | AW, Actions | Bash `case` patterns inside `$(...)` use balanced `(pattern)` syntax for Bash 3.2 compatibility. Intentional unquoted glob matching remains allowed. |
| `privileged-checkout` | — | AW, Actions | Privileged triggers warn on enabled or dynamic `allow-unsafe-pr-checkout`, PR-head, dispatch, or upstream-head checkout selectors, literal `refs/pull/` refs, and direct `gh pr checkout` or Git calls selecting them. Trusted base checkouts are not flagged. |
| `artifact-execution` | — | AW, Actions | `workflow_run` warns when cross-run downloads extract into the workspace or an unknown path, or when later direct execution uses their literal destination. |
| `remote-script-execution` | — | AW, Actions | Bash/sh `run` scripts do not execute `curl` or `wget` output directly: pipes into a shell or interpreter, process substitution (`bash <(curl …)`), command substitution as code (`sh -c "$(curl …)"`, `eval`), and stdin redirects. Downloads to files, and pipes into data tools such as `jq` or `tee`, are not flagged. |

### Suppressing a diagnostic

A standalone YAML comment can acknowledge a false positive for specific rule IDs on the immediately following field:

```yaml
jobs:
  check:
    runs-on: ubuntu-latest
    # gh-aw: ignore[unpinned-images] -- Reviewed caller supplies an immutable digest.
    container: ${{ inputs.container-image }}
    steps:
      - run: echo ready
```

Use comma-separated IDs for multiple rules: `# gh-aw: ignore[template-injection, environment-file-injection] -- Reason`. A nonempty reason and exact registered rule IDs are required. Wildcards, empty lists, unknown IDs, and malformed or misplaced directives fail linting.

Ordinary notes containing `gh-aw:` are not directives. Suppression recognition is restricted to the `ignore` syntax; malformed suppression attempts still fail linting.

Place the comment at the field's indentation, with no blank line before the field. Other YAML comments may intervene. Before `- run:`, match the list marker's indentation; only that first field is targeted, not the whole step. A mapping or sequence field includes its authored child fields. Sibling fields and steps, other rules, and analysis failures remain visible. There is no file-wide or disable/enable directive. Comments inside strings, scripts, or the Markdown prompt do not suppress anything.

Directives work in Actions YAML and AW frontmatter. AW comments are not copied into compiled execution graphs: when linting a compiled workflow and its companion AW document, execution diagnostics use comments in the Actions YAML, while findings from authored AW fields use the frontmatter, including AW-owned findings from mixed-target rules. Suppressions on YAML anchors do not implicitly carry over to alias uses; an alias field needs its own directive, scoped to its actual descendants rather than similarly prefixed sibling keys.

Suppression acknowledges a diagnostic; it does not establish safety. These comments do not disable independent compiler validation, strict-mode security controls, runtime protections, or external tools such as actionlint and shellcheck.

### Privileged triggers

`dangerous-triggers` correlates `issue_comment`, `pull_request_review`, and `pull_request_review_comment` (plus AW command shorthands) with actor admission and GitHub integrity filtering. AW comment-trigger warnings require both broadened `on.roles` (`all`, `read`, or `triage`) and weak filtering (`tools.github.min-integrity: none` or `unapproved`, or lockdown overriding guard-policy fields). Omitted roles use the protected `admin`, `maintainer`, and `write` defaults, including for slash commands. These defaults and explicit trusted-only roles suppress this trigger finding even when the workflow intentionally reads untrusted GitHub content.

Explicit `approved` or `merged` filtering without a lockdown override, or `tools.github: false`, also suppresses the comment finding. `lockdown: true` warns only when it overrides configured guard-policy fields (`allowed-repos`, legacy `repos`, `min-integrity`, `blocked-users`, `trusted-users`, or `approval-labels`); disable lockdown before using approved/merged filtering as remediation. Explicit thresholds are validated even with lockdown enabled. Missing thresholds, unresolved imports, string GitHub shorthand, and bare lockdown without configured guard fields are unknown, not evidence of weak filtering: public repositories can apply approved filtering automatically. `integrity-proxy: false` does not disable MCP gateway filtering.

Compilation uses effective roles and resolved tools. Dry-run retains only authored `admin`, `maintainer`, or `maintain` roles, defaulting to `admin`, `maintainer` when none remain; lint evaluates those restricted roles, not authored broadened roles. Standalone lint does not resolve imports or determine repository visibility. Actions YAML without companion AW policy does not inherit gh-aw role defaults, and unknown filtering alone does not produce a comment warning. Authored centralized commands remain analyzed alongside generated `workflow_dispatch` triggers, without duplicating inline command expansions. AW-backed findings use authored AW paths for field-scoped suppression in compilation and companion-lock lint; graph-only findings use Actions paths.

Integrity filtering protects GitHub tool inputs, not event admission, arbitrary scripts, or PR-code execution. It does not suppress independent checkout or template-injection findings. `on.pull_request_target.acknowledge-risk: true` suppresses only the target-trigger warning on AW inputs; standalone Actions YAML has no AW acknowledgment. Compilation deduplicates CTR-008 only when the equivalent validator warning was actually emitted, not merely because strict mode is enabled.

The [Alpha-Omega event-policy guidance](https://alpha-omega.dev/blog/de-fanging-dangerous-triggers-in-github-actions-across-an-organization/) recommends an explicit GitHub Actions event allowlist, excluding `pull_request_target` and `issue_comment` when unnecessary. Evaluate the policy before enforcement. These policies are external to gh-aw: compilation does not query or configure them. A role check, risk acknowledgment, lint suppression, or absence of findings does not prove policy enforcement or that untrusted code is safe to execute with credentials.

### Privileged custom code

`privileged-checkout` and `artifact-execution` warn in strict and non-strict mode, are enabled in the default registry, and are not runtime enforcement. Skipping lint removes their diagnostics; strict `pull_request_target` policies still apply, and trigger-risk acknowledgment does not disable these checks.

`privileged-checkout` covers workflows declaring `pull_request_target`, `workflow_run`, `issue_comment`, `repository_dispatch`, `pull_request_review`, or `pull_request_review_comment`, including mixed triggers. An omitted or literal-false `allow-unsafe-pr-checkout` is not a finding; `true` or a dynamic value is. Selectors are inspected as Actions expressions. They include PR merge-commit SHAs and upstream `head_commit.id`. Shell checks inspect Bash/sh ASTs for direct `gh pr checkout`, and for `git fetch`, `checkout`, `switch`, or `clone` with event-controlled sources or literal PR refs. Fetch refspecs may carry a `+` force marker; plain clones inspect only the repository operand. A fetch alone is a source-selection warning, not evidence that code ran.

An exact `github.event.pull_request.merge_commit_sha` checkout is exempt when its job or step condition requires `github.event.pull_request.merged` to be true, so the revision has already landed. Guards may test the property directly or compare it to literal `true`; a conjunction needs one guarding branch, and every branch of a disjunction must guard. The proof follows declared `needs` edges. A status override such as `always()` inherits it only if the condition requires the dependency's success, or requires both `!cancelled()` and a non-skipped dependency result. Step-level `success()` does not prove dependency success. `closed` alone is insufficient.

`artifact-execution` recognizes `actions/download-artifact` with an explicit cross-run `run-id` and a potentially nonempty `github-token`, and `dawidd6/action-download-artifact`, whose default selects another run. Explicit current `github.run_id` is exempt; dynamic tokens count as potentially cross-run. Extraction into the workspace root or an ancestor warns, because downloads can overwrite code or configuration. Extract into a dedicated literal directory instead. Modeled execution sinks include direct script or interpreter calls, Node preload/import/loader operands (`-r`, `--require`, `--import`, `--loader`, `--experimental-loader`), local action paths, and package or build commands (`npm ci`, `npm run build`, `make`) inside the download destination. Data-only reads such as `cat`, `jq`, and checksums are not execution, and `npm run` without a script name only lists metadata. A successful producer does not prove artifact authenticity.

`runner.temp` expressions resolve to a distinct symbolic directory, and shell operands can use `$RUNNER_TEMP` or `${RUNNER_TEMP}`. Data-only downloads there do not warn about unknown paths or workspace overwrite. Direct execution from them still warns, and traversal to the workspace or a common ancestor is not exempt.

These are bounded static checks, not whole-program analysis or provenance verification. They do not follow arbitrary environment indirection, `cd`, nested command strings, aliases, command wrappers, other interpreters, third-party action internals, cross-job artifact flows, or dynamically computed paths. Unsupported shells are not analyzed. Implicit Windows PowerShell is not treated as Bash, but explicit Bash or sh overrides are. Malformed modeled shell syntax produces an incomplete-analysis warning. Missing findings do not establish that shell or artifacts are safe.

Compilation stays offline and deterministic. These rules neither resolve action tags nor classify unknown SHAs as vulnerable. Keep full-SHA pinning and update pins to reviewed patched releases, not mutable tags.

### Executable inputs and runner environment files

`template-injection` recognizes `actions/github-script.script`, `amadevus/pwsh-script.script`, `jannekem/run-python-script-action.script`, `cardinalby/js-eval-action.expression`, and `addnab/docker-run-action.run`. Action names match exactly, independently of their version or SHA; reviewed input names match case-insensitively, while diagnostics retain authored key casing. Other inputs are not assumed to be executable.

Output-flow analysis reuses the symbolic shell model, preserving known event/input origins through `GITHUB_OUTPUT`, environment aliases, and declared dependency outputs. Unknown action outputs are not automatically trusted or classified as attacker-controlled. AW-only analysis tracks explicit steps within each execution block; cross-job propagation requires the Actions AST. Unsupported interpreters, unresolved output names, and malformed modeled scripts produce coverage warnings. JavaScript-produced output propagation and arbitrary external-script behavior are outside this shell model.

Analysis does not mutate typed steps or remove their output declarations and emission-time interpolation safeguards. Findings collected before an analysis failure are preserved; malformed Actions expressions remain explicit failures, not warning-only success.

`environment-file-injection` is an error in strict and non-strict mode. An attacker-controlled multiline value can append extra environment records even when passed through a quoted shell variable or a fixed heredoc delimiter. An attacker-controlled path can change later executable lookup. The check is conservative: arbitrary shell validation is not treated as sanitization. PowerShell, other unsupported shells, and unavailable generated AW execution produce incomplete-analysis warnings rather than a clean bill of health. This rule does not replace the stricter prohibition on environment-file writes in authored safe-output steps.

Shell stdout follows ordered descriptor redirects and duplication, including routing through fd 2 into runner command files. Unmodeled persistent, dynamic, compound, or independently produced stderr flows remain coverage gaps. Unsupported Node commands with tainted inherited environment values also produce coverage warnings when JavaScript analysis is unavailable.

As with secret-flow coverage, the compiler omits environment-file and output-flow coverage warnings from its warning count. `gh aw lint` retains them. Confirmed findings still fail compilation.

### False-positive boundaries

| Check | Safe cases and conservative limits |
|---|---|
| Artifact preloads | Only explicit relative paths, absolute paths, and local file URLs for ESM imports/loaders are resolved as local preload files. CommonJS `--require` does not accept file URLs. Bare package specifiers are not workspace-relative paths; unresolved package resolution produces an incomplete-analysis warning, while known Node builtins are exempt. Options after eval/print payloads are inspected. `--version`, `-v`, and `--v8-options` do not execute preloads, including when supplied later. `--help` and `--check` can still load preloads and are not blanket exemptions. |
| Executable action inputs | Only the reviewed action/input pairs above are code sinks. Lookalike action names and unrelated data inputs are not sinks. Runtime environment access and expressions returning booleans do not trigger executable interpolation findings. |
| Output interpolation | Quoted runtime environment access, boolean comparisons, and boolean predicate results remain data. Taint is conservatively unioned across shell branches and assignments; overwrites and arbitrary sanitizers do not prove a value safe. Unknown outputs do not become attacker-controlled solely because another output is tainted, except where unresolved or multiline records require wildcard tracking. |
| Environment files | Literal filenames such as `'$GITHUB_ENV'`, `'$GITHUB_PATH'`, and `'$GITHUB_OUTPUT'` are ordinary working-directory-relative files, not runner command files. They retain ordinary file taint without publishing runner environment records or outputs. Expanded runner variables and their modeled aliases are checked. Constants and boolean-filtered values are allowed; quoting untrusted multiline data or using a fixed heredoc delimiter is not sanitization. |
| Image pins | An explicitly empty service `image`, including an empty fully substituted matrix alternative, disables that service and is allowed; an empty job-container image is not. Matrix values are substituted into the full image template before validation. A matrix `exclude` entry naming only the image axis removes that alternative, but a later `include` can restore it. Partial multi-axis exclusions remain conservative; unresolved expressions produce coverage warnings, not proof of mutable images. |

### Historical zizmor regressions

These checks build on the audits, rule names, and remediation guidance of [zizmor](https://github.com/zizmorcore/zizmor), an [MIT-licensed](https://github.com/zizmorcore/zizmor/blob/main/LICENSE) scanner, which deserves credit for them. The fixes below are gh-aw's regression evidence. gh-aw implements selected checks independently in Go and does not invoke zizmor. The [upstream audit documentation](https://docs.zizmor.sh/audits/) is authoritative for zizmor behavior.

| Historical finding | Fix evidence | AST coverage |
|---|---|---|
| Persisted checkout credentials ([`artipacked`](https://docs.zizmor.sh/audits/#artipacked)) | [#2991](https://github.com/github/gh-aw/pull/2991), [#14449](https://github.com/github/gh-aw/pull/14449), [#25873](https://github.com/github/gh-aw/pull/25873) | Explicit checkout steps, including compiled imported setup steps. |
| Release dependency-cache poisoning ([`cache-poisoning`](https://docs.zizmor.sh/audits/#cache-poisoning)) | [#5214](https://github.com/github/gh-aw/pull/5214), [#5872](https://github.com/github/gh-aw/pull/5872), [#19865](https://github.com/github/gh-aw/pull/19865) | Enabled and default Go and Node caching on release and tag workflows. |
| Overbroad permission placement ([`excessive-permissions`](https://docs.zizmor.sh/audits/#excessive-permissions)) | [#5229](https://github.com/github/gh-aw/pull/5229) | Workflow-wide writes. `permission-scope` already checks blanket grants. |
| Literal-expression obfuscation ([`obfuscation`](https://docs.zizmor.sh/audits/#obfuscation)) | [#4398](https://github.com/github/gh-aw/pull/4398), [#14257](https://github.com/github/gh-aw/pull/14257) | Expression roots in outputs, environment bindings, and other runner fields. |
| Executable template injection ([`template-injection`](https://docs.zizmor.sh/audits/#template-injection)) | [#2871](https://github.com/github/gh-aw/pull/2871), [#9147](https://github.com/github/gh-aw/pull/9147), [#25888](https://github.com/github/gh-aw/pull/25888) | Shell and heredoc checks, plus `actions/github-script` inputs, without a JavaScript parser. |
| Mutable action references ([`unpinned-uses`](https://docs.zizmor.sh/audits/#unpinned-uses)) | [#2686](https://github.com/github/gh-aw/pull/2686), [#55641](https://github.com/github/gh-aw/pull/55641) | `action-pin` remains authoritative; no duplicate rule is registered. |

The four newer rules warn in strict and non-strict mode. They flag reviewable configuration, not proven credential disclosure or cache compromise. Cache checks do not infer publishing from job names. Known pre-v4 Go and pre-v5 Node tags keep their disabled automatic-cache defaults, and SHA-pinned versions with unknown defaults need an explicit cache opt-out to avoid warnings. Explicit Node `cache` still enables restoration even with `package-manager-cache: false`.

AW checks inspect explicit execution blocks: `safe-outputs.steps`, custom jobs, and post-steps. They do not load imports or reconstruct generated defaults. When a compiled Actions AST is supplied, it owns execution checks. Invalid expressions or malformed action inputs fail analysis with value-free errors but keep findings from valid scopes.

This is not complete zizmor parity. Creator-verification findings such as [#50015](https://github.com/github/gh-aw/pull/50015) need provenance data that static ASTs lack.

`otlp-secret-leak` reports direct secret references in authored `observability.otlp.attributes` and `resource-attributes`, and follows modeled flows into `GH_AW_OTLP_ATTRIBUTES` and `OTEL_RESOURCE_ATTRIBUTES`. Diagnostics name only the configuration path. The check is advisory.

## Severity and source policy

With strict mode enabled (the AW default), these are errors: action pinning, workflow-run branch scope, blanket write permissions, disabled version checking, private-to-public opt-outs, enabled Node install hooks, and bare network wildcards. In explicit non-strict AW, they are warnings. Native Actions policy-sensitive findings are warnings, and AW-only rules skip native Actions. Conditional imports, bare label-allowlist wildcards, invalid OIDC read grants, missing or malformed `workflow_run.workflows`, invalid explicit timeouts, and direct template injection are always errors. Unconstrained wildcard push targets stay warnings even in strict mode. The linter does not reconstruct CLI or repository strict overrides, load imported configuration, infer missing permissions, or decide whether a repository-write grant is authorized.

Malformed `uses` references are always `action-pin` errors, in native Actions and non-strict AW alike. This covers invalid repository or path syntax, missing refs, malformed image digests, expressions, invalid UTF-8, control characters, and Unicode whitespace or line separators. Valid mutable tags, branches, and abbreviated SHAs keep the policy severity above. References are checked after YAML parsing, so version labels belong in YAML comments outside the scalar. Check either format with `gh aw lint --rules action-pin .github/workflows/example.yml` or the `.md` source.

Tool-scope and sandbox-config follow the strict-policy error and warning split, including explicit non-strict sandbox opt-outs and internal customizations. Image-argument, checkout-mode, and fork-association findings are always errors. Checkout and tool checks defer absent prerequisites that imports may supply, but still report explicit contradictions.

Pull-request-target-roles is an error in strict mode and a warning otherwise. Only the non-strict warning can be suppressed, with `features.dangerously-allow-roles-read-and-pull-request-target: true`, and that does not suppress other checks. Safe-output-shell-expansion and firewall-ssl-bump are errors in either mode. The firewall rule inspects authored AWF arguments; `sandbox.agent.args` is an internal customization rejected in strict mode. The shell rule shares the compiler's lexical policy, including matches in quoted text or comments, and reports script-relative lines. It does not apply to ordinary Actions steps.

Pull-request-target-checkout is an error in strict mode and a warning otherwise. Neither trigger-risk acknowledgment nor the role-warning opt-out suppresses it. Base checkouts accept the base repository and omitted, base-SHA, or base-ref forms. External pairs must match `on.pull_request_target.allowed-checkouts` exactly, including case, and cannot use expression or wildcard refs, `refs/pull/...`, omitted refs, wiki expansion, or extra `fetch` values. Missing configuration and eligible pairs without an authored allowlist defer when imports may supply policy. Explicit unsafe refs and fetches still report.

Prompt-expression findings are errors in either mode, with prompt-relative line numbers and never expression contents. The shared Actions lexer handles quoted `}}` delimiters. Blocked property names follow case-insensitive Actions semantics; string literals containing them are not accesses. Computed property values, frontmatter expressions, imported content, and the full context allowlist are out of scope. A malformed interpolation stops further prompt traversal but keeps earlier findings.

Gateway-dispatch-literals findings are errors in strict mode. They name the configuration path without expression contents. Check declarations without compiling using `gh aw lint --rules gateway-dispatch-literals .github/workflows/example.md`. The rule inspects authored gateway declarations, not the lowered Actions `repository_dispatch` listener.

Firewall argument checks inspect `--allow-urls` values, URL operands, and `--allow-urls=...` tokens before an option terminator (`--`). A literal `--ssl-bump` or `--ssl-bump=true` satisfies the prerequisite unless a later `--ssl-bump=false` appears; the last SSL setting wins. Internal `sandbox.agent.args` overrides require `strict: false`, and SSL inspection does not make them valid in strict mode. A non-strict example:

```aw wrap
strict: false
sandbox:
  agent:
    args: [--ssl-bump, --allow-urls, "https://example.com/path"]
```

The compiler rejects `network.firewall` regardless of its SSL setting. Remove that block and use `network.allowed` for domain policy. Do not add `network.firewall.ssl-bump` as a fix.

### Step references

`step-reference` warns in either strict mode when an expression references a missing step in the same job, including job outputs and step `if`, `with`, `env`, and `run` fields. A direct step access on the left of `||` with a non-empty string literal on the right is treated as intentional; function-wrapped, negated, and chained accesses still warn. This catches dangling references such as those fixed in [#71898](https://github.com/github/gh-aw/pull/71898). Quoted text, whole-context reads, wildcard projections, and dynamic indexes select no known step ID, but literal references nested in computed indexes are checked. Output names are not validated, since external actions can provide arbitrary outputs.

The compiled Actions graph is authoritative when supplied. AW-only analysis checks complete explicit `jobs`, `safe-jobs`, and `safe-outputs.jobs` scopes, including post-steps and custom job setup or pre-steps. Root steps, `safe-outputs` steps, compiler-owned job overrides, and jobs with `restore-memory` defer to the compiled graph. Unresolved imports or dynamic step IDs also defer missing-step checks.

### Needs consistency

`needs-consistency` warns in either strict mode. Dot and literal bracket accesses, including bare job and step `if` conditions, parse as Actions expressions. Transitive dependencies are not available in `needs`; a consumer must declare the producer directly. Job properties are `result` and `outputs`. Output accesses require a producer `outputs` entry, but reusable-workflow outputs are exempt because their declarations are external. Native Actions parsing rejects missing targets and cycles first.

The compiled graph takes precedence over companion AW source. AW-only checks cover explicit `jobs`, `safe-jobs`, and `safe-outputs.jobs`, normalize safe-job dependency IDs, and defer missing imported or compiler-generated producers. Compiler-owned scopes and safe-job defaults are not reconstructed. Wildcard projections, whole-context reads, and computed indexes select no known producer, but their nested literal accesses are checked. Dependencies used only for ordering are not reported as unused.

Prompt checks use activation's dependencies and report prompt-relative lines. AW analysis recognizes independent prompt producers, `on.needs`, and jobs that depend on `pre_activation` without depending on `activation`. For example, `${{ needs.release.outputs.release_id }}` is flagged when `release` runs after activation. Prefer reading data staged by agent steps. The compiler defers direct custom-job output and result interpolations to the agent job when their producer runs between activation and the agent, and compiled analysis checks those late bindings. Compound expressions and template conditions are not late-bound; stage their data explicitly instead.

Bare `if` and `elseif` template conditions get the same checks as interpolations. Prompt syntax errors belong to `prompt-expression` and do not suppress `needs-consistency` diagnostics.

```bash wrap
gh aw lint --rules needs-consistency .github/workflows/example.md
```

### Custom job detection gates

`custom-job-detection` is an error in either strict mode. Custom jobs must run before `agent` or after `detection`, never between them. A custom job downstream of `agent`, including through another custom job or a compiler-owned post-agent job, must directly need `detection`. Jobs that need only `detection` are checked too. Without a status function, Actions requires all dependencies to succeed. Conditions using `always()`, `failure()`, or `cancelled()` must also require `needs.detection.result == 'success'` on every runnable branch:

```aw wrap
jobs:
  report:
    needs: [agent, detection, scan]
    if: always() && needs.detection.result == 'success' && needs.scan.result == 'failure'
    runs-on: ubuntu-latest
    steps:
      - run: echo "Report a failed scan after successful threat detection"
```

`needs.detection.result != 'skipped'` is insufficient, because failed detection must also block the job. The rule parses expressions rather than matching text, so quoted guards and unguarded `||` branches fail. Use the explicit equality guard for complex predicates.

A downstream leaf custom job may set `detection-gate: status-only` to notify after failed or warn-mode detection. The compiler keeps the authored condition, adds `detection` to `needs`, and rejects artifact downloads, disallowed job outputs, `GH_AW_AGENT_OUTPUT`, agent or safe-output file paths, and opaque reusable workflow calls. Permitted inputs are job results, outputs from compiler-owned detection-gated jobs (such as `safe_outputs`), and outputs from authored custom jobs proven to run before `agent`. Pre-agent outputs must come from a direct dependency and pass through `env`, not executable interpolation.

The compiler adds a direct `detection` dependency and an outer guard requiring `needs.detection.result == 'success'` and `needs.detection.outputs.detection_success == 'true'` to downstream custom jobs, including reusable workflow calls. This blocks failed, cancelled, skipped, or missing results and warn-mode findings, even when detection's job status is successful. Custom `if` expressions cannot override the guard. A job required by detection that also depends on `agent` is rejected, since the guard would create a cycle.

The rule follows transitive dependencies and inspects top-level custom `jobs`, not compiler-managed jobs or `safe-outputs.jobs`. With a compiled AST, its dependencies and conditions are authoritative; a companion AW document identifies authored jobs. Without a companion, compiler-owned names (such as generated state-push and memory-maintenance jobs) are exempt, which does not establish provenance for arbitrary native jobs. Graphs without both `agent` and `detection` are outside this policy. Lint shared fragments explicitly, since AW-only analysis does not resolve imports.

Custom jobs cannot create an inactive built-in job such as `evals` by reusing its reserved name. Configure the feature to customize a compiler-generated job, or choose a different name.

### Custom grader file operations

`grader-file-io` rejects file operations in authored custom JavaScript graders in strict mode. It uses the [native JavaScript parser](#optional-native-javascript-analysis), reports script-relative lines, and never executes grader code:

```bash wrap
gh aw lint --rules grader-file-io --json \
  --javascript-module /path/to/gh-aw/pkg/workflow/taint/javascript \
  /path/to/repository/.github/workflows/example.md
```

The rule rejects module loading (`require`, static and dynamic imports, and relative helper modules that could hide file I/O). It also rejects unbound filesystem and host capabilities such as `fs`, `process`, `module`, Deno and Bun file APIs, and browser file readers or pickers. Aliasing an acquired capability does not avoid the finding.Ordinary local bindings, object keys named `fs` or `require`, comments, string literals, and recorded file-path data are not file access.

All executable grader script bodies are checked, including disabled definitions, nested functions, parameter defaults, and unmodeled syntax the parser retains. Built-in grader configuration, Markdown examples, other script hooks, and native Actions without an AW companion are out of scope. Lint imported grader fragments explicitly.

Without `--javascript-module`, each custom script gets a coverage warning rather than a pass. With a parser configured, unavailable or malformed ASTs fail lint. The rule does not prove reflection or generated code safe, and it does not replace the isolated grader worker's filesystem, process, and code-generation restrictions. Graders may use only the immutable in-memory `session`, `config`, and `helpers`.

## Secret-flow analysis

`secret-leak` tracks symbolic secret references, `github.token`, and known app-token action outputs through environment scopes, shell assignments, step outputs, and dependent job outputs. It detects disclosures to logs, step summaries, workflow outputs, request URLs and bodies, known publication inputs, cache keys, and artifacts containing modeled secret-bearing files. Authentication headers, token inputs, credential use in [safe-output integration jobs](#safe-output-integration-credentials), and the reusable workflow `secrets:` channel are not disclosure sinks. Boolean comparisons and predicates filter secret values. Formatting, JSON serialization, encoding, and `add-mask` registration do not sanitize later uses.

Named step outputs are same-job transport when the analyzer can identify the producing step and its consumers. For example, a credential resolver can write `credential=$TOKEN` to `$GITHUB_OUTPUT` and pass `${{ steps.resolver.outputs.credential }}` to checkout's `token` input. The output name grants no trust: the value stays tainted, so logging it, uploading it, publishing it, sending it to an unauthorized sink, or exposing it as a job or workflow output still reports a disclosure. The same applies to modeled `core.setOutput` calls and GitHub Script return values.

In Bash and sh, recognized `test`/`[` string and file predicates and the `:`, `true`, and `false` builtins do not disclose their arguments. Presence-only expansions such as `${TOKEN:+present}` do not contain the token, but secret-bearing replacements remain tainted. Nested command disclosures, parameter-expansion error messages, and `set -x` output are still checked. Numeric comparisons, malformed tests, and word-splitting predicates keep coverage warnings, since their errors can expose operands.

Unknown file reads produce coverage warnings when they could select a modeled secret-bearing file or involve a secret-bearing filename. Unknown output targets warn when the output or filename is tainted. Unrelated public file operations add no warnings.

### Safe-output integration credentials

Safe-output jobs are declared third-party integration boundaries. In `safe-outputs.jobs` (including imported jobs and legacy `safe-jobs`), secrets may be consumed through network endpoints, headers, request fields, and SDK calls without a comment or separate opt-in:

```aw wrap
safe-outputs:
  jobs:
    notify:
      description: Send a private notification to an external service
      runs-on: ubuntu-latest
      max: 1
      permissions: {}
      steps:
        - env:
            DESTINATION: ${{ secrets.PRIVATE_NOTIFICATION_ENDPOINT }}
            SERVICE_TOKEN: ${{ secrets.NOTIFICATION_SERVICE_TOKEN }}
          run: |
            jq -c '{text: .summary}' "$GH_AW_AGENT_OUTPUT" |
              curl --fail -sS -H 'Content-Type: application/json' \
                -H "X-Service-Token: $SERVICE_TOKEN" --data @- "$DESTINATION"
```

An SDK may consume a credential the same way, for example in an `actions/github-script` step within a declared safe-output job:

```yaml wrap
- uses: actions/github-script@v8
  env:
    SERVICE_TOKEN: ${{ secrets.NOTIFICATION_SERVICE_TOKEN }}
  with:
    script: |
      const client = getOctokit(process.env.SERVICE_TOKEN);
      await client.rest.users.getAuthenticated();
```

This is a job-provenance policy, not a curl option allow-list or a requirement for direct, immutable secret bindings. The declaration establishes intended credential use, not the secret's value, host, protocol, or recipient. Authors remain responsible for client configuration, recipient trust, and runtime network isolation.

The policy does not disable secret-leak lint for the job. Secrets stay tainted for explicit logs, xtrace, summaries, workflow outputs, artifacts, and known publication sinks, even alongside a network operation. Ordinary jobs and native Actions inputs without AW integration provenance keep conservative network checks; job or secret names and comments grant no exemption. Compiler analysis includes imported safe-output jobs. Standalone lint does not resolve imports.

SDK use does not imply SDK coverage; JavaScript inspection remains opt-in.

For upload bodies, curl `-T`/`--upload-file` treats both `-` and `.` as stdin (`.` selects non-blocking mode). Wget `--post-file` reads a regular file, and neither spelling consumes piped stdin. Ordinary-job network checks also follow tracked files and stdin in curl headers (`-H @file`, `--header=@-`), multipart fields (`-F 'name=<file'`, `--form='name=@-'`), and URL-encoded fields (`--data-urlencode 'name@file'`). Resolved shell variables work in separate and inline operands. Literal `--data-raw` and `--form-string` values are not file references. Unresolved sources and quoted or escaped multipart filenames produce coverage gaps.

### Scope and limitations

For `.lock.yml` inputs, a companion `.md` adds authored-policy and prompt checks to the existing execution graph. Native `.yml` and `.yaml` inputs never acquire an assumed companion. Explicit `.md` inputs are inspected through the AW AST: prompt, top-level `steps` and `post-steps`, authored `jobs` and `safe-outputs.jobs` (including legacy `safe-jobs`), and script hooks. No compiled graph is fabricated.Secret analysis keeps coverage warnings for missing imports, generated steps, cross-job flows, and compiler defaults.

Analysis is conservative within Bash and sh control flow and can report infeasible paths. It does not execute code or inspect external actions, sourced scripts, other interpreters, dynamically constructed shell code, or imported runtime prompt files. Authentication arguments count as intentional credential use. A clean result is not proof that a workflow cannot leak secrets.

## Diagnostics

Text findings include the rule ID and threat ID, for example `[safe-output-shell-expansion] (CTR-009)`. `--list-rules` shows the same threat ID. JSON diagnostics contain the file, rule ID, optional `threat_rule_id`, severity, AST path, optional script-relative line, message, and symbolic source locations, never resolved secrets or script contents. The threat ID is omitted for unmapped rules. Errors produce a nonzero exit status; coverage warnings alone do not.

`gh aw compile --json --actionlint --shellcheck` also reports external-linter findings in batch-scoped results (`scope: "batch"`, `workflow: "actionlint"` or `"shellcheck"`). Issues include `rule_id`, normalized `severity`, and `file`, plus YAML `line` and `column` for Actionlint. Standalone ShellCheck uses snippet-relative `script` coordinates and a one-based `step_index` when available. Free-text identities are omitted from JSON. `script.sanitized` marks snippets whose expressions were replaced before analysis. Tool or parse failures have a distinct `*_error` type, not a clean scan.

Non-strict compilation keeps these issues in `warnings`, even for high-severity findings. `--strict` reports them in `errors`, failing compilation. Per-workflow results still describe compilation, not the batch scan. Collecting diagnostics does not need `--dry-run`.

### Bash case-pattern compatibility

`bash-case-pattern` checks parsed `run` scripts with an omitted shell or explicit Bash, ignoring comments, quoted data, and non-Bash interpreters. It checks authored blocks or the compiled graph. It does not load imports, execute expressions, or replace `bash -n` and ShellCheck. Findings give the script-relative line without reproducing the pattern:

```bash
matched=$(case "$remote_ref" in ($source_ref) printf 1 ;; esac)
```

The opening parenthesis avoids a Bash 3.2 command-substitution parsing ambiguity. Quoting `$source_ref` would turn intentional glob matching into literal matching. For execution coverage, run `go test ./pkg/workflow -run TestGenerateFetchStep -count=1` with Bash 3.2 and a modern Bash on `PATH` separately.

### Remote script execution

`remote-script-execution` warns when a parsed Bash or `sh` `run` script executes network content without verification, a common supply-chain risk (compare the 2021 Codecov uploader compromise). It recognizes `curl` and `wget` writing to standard output, followed through `sudo`/`env` wrappers into `bash`, `sh`, `zsh`, `dash`, `ksh`, `python`, `node`, `perl`, `ruby`, `eval`, or `source`. Interpreters given a script file or inline literal program are not flagged. Program text inside quoted strings, PowerShell steps, and multi-step download-then-execute flows are not analyzed. Prefer a pinned action, or download a versioned artifact and verify it before execution:

```bash
curl -fsSLo tool.tar.gz "https://example.com/tool-v1.2.3.tar.gz"
echo "<sha256>  tool.tar.gz" | sha256sum -c -
```

### Comparing compiler revisions

Build the candidate from the intended source revision, not an installed release binary:

```bash
make build
git rev-parse HEAD
./gh-aw version
sha256sum ./gh-aw
actionlint --version
shellcheck --version
```

Record dirty-tree changes, the revision, binary checksum, linter versions, and source SHAs. Use identical source snapshots, locks, action-pin caches, and flags for both runs. Select action mode explicitly, because source builds can default to development mode, which adds Docker build actions absent from release locks. Pin the same setup-action SHA in both runs:

```bash
/absolute/path/to/compiler compile \
  --action-mode release --action-tag "$SETUP_ACTION_SHA" \
  --no-check-update --actionlint --shellcheck --json \
  > compile.json 2> compile.stderr
```

Retain exit status, stdout, stderr, and emitted locks per run. For the downstream regression set, use actionlint 1.7.12 and ShellCheck 0.11.0, with `--use-samples` for gh-aw-test compilers that support it and `--strict --schedule-seed githubnext/gh-cao --show-all` for CAO.

Compare normalized diagnostics by repository-relative workflow, rule, severity, location, and message, removing only disposable-directory prefixes. Include stderr-only findings from older compilers, and keep unchanged baseline findings separate from added ones. Exit 0 alone is not evidence of a clean run. Do not use `--approve`, blanket exceptions, or workflow dispatches to normalize a comparison.

## Optional native JavaScript analysis

This analysis is experimental and opt-in. `--javascript-module` enables JavaScript secret-flow analysis and the custom-grader file-I/O rule through Microsoft's Go TypeScript compiler, using the upstream-exported, **unstable** `@typescript/native-preview/unstable/sync` and AST APIs. The supported package version is `7.0.0-dev.20260707.2`. Node.js 22.12 or newer and its platform-native compiler executable must already be installed. The CLI downloads nothing.

For a gh-aw source checkout, install the pinned dependency and supply absolute paths:

```bash wrap
npm ci --prefix /path/to/gh-aw/pkg/workflow/taint/javascript --ignore-scripts
gh aw lint --rules secret-leak --json \
  --javascript-module /path/to/gh-aw/pkg/workflow/taint/javascript \
  --javascript-root /path/to/repository
```

The checker follows literal `node`/`nodejs` file and `--eval` invocations, `--print` expressions, `shell: node {0}`, `actions/github-script` scripts, and available relative CommonJS and ES module imports. It tracks symbolic environment, argument, and stdin sources, aliases, object members, local function calls, selected stdin callbacks, JSON and string transformations, and known file reads and writes. Object rest preserves member shapes: credential-bearing environment members do not taint an unrelated public timeout, but a secret-derived timeout stays tainted. Logging, summaries, and outputs are disclosure sinks. Non-authentication request fields are disclosure sinks outside declared safe-output integration jobs; inside them, the credential-consumption policy above applies. Comparison and presence results, and type-proven public length or index metadata, remove values. Arbitrary object properties and numeric coercions do not. Genuine escaped, single-record stdout `add-mask` registrations are recognized without sanitizing later uses. Dynamic reads remain explicit gaps.

`--javascript-mode fast` is the default when JavaScript is enabled. With a companion AW document, it analyzes authored JavaScript rather than generated runtime wrappers, while keeping compiled shell, prompt, and job/output checks. Syntax failures are isolated per script, and sources found through data flow are parsed on demand.

Script hooks are checked at executable schema locations, not by matching every key named `script`:

| Source | Execution contract |
|---|---|
| `mcp-scripts.<tool>.script` / `.run` | JavaScript tool body or Bash; JavaScript returns become agent outputs. Tools inherit the shared server environment; tool env mappings merge in sorted tool-name order, with later values winning. Python and Go keep unsupported-language warnings. |
| `safe-outputs.scripts.<handler>.script` | Invoked handler body and returned output, with `safe-outputs.env` overriding workflow env. |
| `graders.<grader>.script` | Pure custom JavaScript over immutable session data in an isolated downstream job, without filesystem, process, or Node environment access. Encoded grader payloads are not Actions-interpolated. |
| Memory-tool validation hooks | Object/string `validation` and legacy aliases, including list configurations; only the validator's sanitized environment is inherited. Encoded script payloads are not Actions-interpolated. |
| `engine.driver` | Inline Node source or available JavaScript driver files, with `engine.env` and then `sandbox.agent.env` overriding workflow env. Repository driver files are not Actions-interpolated. Other languages and unavailable files retain warnings. |
| `engine.behaviors` | Node harness, invoked `parseLog` function, and MCP configuration adapter. The harness inherits engine execution env; log parsing and MCP configuration run in separate steps without `engine.env`. |

Inline hook expressions use the script-writing step's workflow env. Agent inputs, grader payload data, memory contents, imported configuration, and generated runtime configuration are not inferred, and these boundaries keep explicit coverage warnings.

Fast mode uses embedded, reviewed API summaries for masking and escaping, OTLP credential collection, Git authentication environment construction, and error helpers. Summaries preserve field-sensitive returns and callback effects. Masking is not declassification. Module paths, source SHA-256 hashes, transitive dependency hashes, and the summary-model version must match. Unknown or changed runtime APIs produce coverage warnings rather than a silent fallback to slow traversal. Maintainers must review the contract and dependencies before updating a catalog hash; the source-identity regression test detects drift.

Use `--javascript-mode deep` to traverse runtime module bodies instead of summaries, and to analyze generated JavaScript in paired lock files. Deep mode can be much slower and still has unsupported-flow limits. The direct Go API keeps deep behavior when its mode is unset.

`--javascript-root` bounds source reads to the supplied repository, including symlink confinement. Repeated `--javascript-runtime-path 'runner-directory=repository-relative-directory'` flags map generated runtime assets to **operator-verified matching source**. For example, only when the workflow uses the exact checkout's gh-aw setup assets, pass `--javascript-runtime-path '${RUNNER_TEMP}/gh-aw/actions=actions/setup/js'`. A mapping does not declare a helper safe: fast mode requires a matching reviewed summary, and deep mode analyzes source. Unknown interpreter paths are not identified by basename.

Missing or incompatible compiler installations, unresolved sources, calls, or modules, cycles, unsupported syntax, loop convergence, and traversal or budget limits produce incomplete-analysis warnings. These are visible without `--debug` whenever JavaScript is explicitly enabled. Fast mode also reports excluded generated and cross-job flows. Unknown boundaries invent neither transformed stdout nor confirmed leaks. Without `--javascript-module`, Bash analysis stays active and records unavailable JavaScript analysis at secret-bearing Node command boundaries. Findings need review and are not a vulnerability count. An empty errors-only report implies neither complete coverage nor whole-program JavaScript safety.

## Linter performance

`DEBUG=workflow:lint:perf` writes per-rule start and completion markers, elapsed nanoseconds, diagnostic counts, and failure status to stderr. Separate markers time sorting, deduplication, and total runner time. The markers contain no AST contents, source symbols, or error text, and they do not change JSON diagnostics or enable coverage warnings:

```bash wrap
DEBUG=workflow:lint:perf DEBUG_COLORS=0 gh aw lint --rules= --json \
  > lint.json 2> lint-performance.log
```

Go execution traces include `workflow:lint`, `workflow:lint:rule:<id>`, and `workflow:lint:diagnostics` regions. Native JavaScript compiler time counts toward the calling rule's wall time, but the external compiler's CPU work is not in a Go CPU profile.
