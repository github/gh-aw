---
title: GitHub Actions Compiler Threat Detection Changelog
description: Version history and dated mapping audits for the compiler threat detection specification
sidebar:
  order: 1006
---

# GitHub Actions Compiler Threat Detection Changelog

**Specification**: https://github.com/github/gh-aw/blob/main/specs/compiler-threat-detection-spec.md
**Editors**: GitHub Next (GitHub, Inc.)

This changelog records the version history and the dated mapping audits for `specs/compiler-threat-detection-spec.md`. The specification itself holds only normative requirements; every version bump and every daily optimizer audit entry is recorded here.

## Version History

| Version | Change |
|---|---|
| 1.0.72 | Added a separate compiler-only `artipacked` exception for agent checkouts immediately followed by compiler-owned credential cleanup and verification. Requires current-compilation provenance, unique full-block parsed-field matching, and exact adjacency; altered, authored, ambiguous, and interrupted blocks remain warned. Preserves runtime YAML, per-checkout authentication, other diagnostics, and standalone lint. Maps adversarial, multi-repository, sparse-checkout, compilation, and provenance-reset regressions. |
| 1.0.71 | Specified the compiler-only `artipacked` warning exception for retained-credential checkouts in the exact generated `safe_outputs` PR checkout/authentication block. Requires current-compilation provenance, a unique full-block parsed-field match, and provenance reset between workflows; modified or ambiguous blocks remain ineligible. Preserves authored-step and other-job diagnostics, other rules, errors, analysis failures, standalone lint, and runtime authentication. Maps exclusion, multi-repository, compiler integration, and reuse regressions. No new threat identifier, permission grant, or lock-file schema change. |
| 1.0.70 | Specified generated admission-output gating for agent, threat-detection, evals, and conclusion usage/session steps. Disabled admission emits no activation admission-output references; dry-run component execution omits admission environment bindings. Preserves component-specific budget selection and independent grader, daily-credit, and job-duration bindings. Maps admission-only lint compilation and component/usage regressions. Regeneration removes stale references; no new threat identifier, permission grant, lint exception, authentication behavior, or lock-file schema change. |
| 1.0.69 | Restored CTR-005's `detection-gate: status-only` syntax and added a bounded TLA+ model for the validated exception: detection completion ordering, failure/warn continuation, and prohibited agent-data access. No new threat identifier, permission grant, or lock-file schema change. |
| 1.0.68 | Specified the supporting runtime privacy contract for CLI proxy and MCP gateway startup diagnostics: name-only proxy forwarding, redaction of all six runner proxy variables and late mask registrations, coalesced credential overlaps, private capture, withheld gateway configuration stdout, and bounded fail-closed CLI publication. Mapped concrete regression suites and added proxy-bearing upstream-failure and gateway late-mask coverage. Does not claim arbitrary-log confidentiality or additional CTR-017 static analysis; no new compiler rejection, threat identifier, permission grant, or lock-file schema change. |
| 1.0.67 | Audit-only clarification of retained PR-head checkout after source-branch deletion. Maps same-repository `pull_request` ref isolation and actual checked-out baseline export to RS-05a, preserves fatal checkout errors and independent safe-output lifecycle checks, and leaves CTR-008/028/030 unchanged. No new threat identifier, permission grant, or lock-file schema change. |
| 1.0.66 | Extended CTR-005 with custom-job ordering and detection gates. The compiler checks the final graph, rejects unordered and pre-detection downstream jobs, and adds non-overridable successful-job and clean-output guards to ordinary downstream custom jobs, including reusable workflow calls. Adds a contract-validated `detection-gate: status-only` exception for leaf status-reporting jobs that cannot consume agent-derived data. Failure, cancellation, skips, missing results, and warn-mode findings block ordinary custom jobs; disabled detection remains exempt. Mapped the transitive Actions/AW `custom-job-detection` subset, the opt-out contract, graph/condition regressions, and regenerated lock files. No new threat identifier or lock-file schema change. |
| 1.0.65 | Added `otlp-secret-leak`, an Actions/AW AST lint subset of CTR-017 that detects secret-tainted OTLP span/resource attributes and modeled Actions environment flows with value-free diagnostics. Documented that the check is advisory and cannot identify hard-coded credentials, arbitrary runtime values, or collector trust. No new threat identifier or lock-file schema change. |
| 1.0.64 | Adds the compromised-intermediate-CA threat and maps bootstrap/runtime controls to security architecture §6.8: independently authenticated AWF/Copilot checksum manifests, literal frontmatter manifest-pin overrides bound to explicit versions, compiler-pinned Compose bytes, authenticated apt signing key, HTTPS-only redirects, actual Go release-toolchain verification and bounded runtime inventory. Overrides propagate through matching detection/eval installations; Copilot overrides bypass toolcache activation. Preserves CTR-011 destination authorization as a separate boundary and records broader download enforcement as a candidate. Unreviewed release downloads without a supplied pin fail closed; no new threat identifier or lock-file schema change. |
| 1.0.63 | Audit-only clarification of the Copilot SDK runtime boundary: explicit staged-only skill registration with ambient discovery disabled, hard rejection of file and symlink roots, and unchanged permission-denial counting and termination. Separates prompt recovery guidance from authorization and preserves CTR-023 engine-capability checks and CTR-028/030 generated provenance requirements. No new threat identifier or lock-file schema change. |
| 1.0.62 | Added authored `pull-request-target-checkout` (CTR-008 subset) and `prompt-expression` (CTR-010 subset) rules, bringing the AST registry to 23 rules. Shared existing base-ref and literal-pair trust predicates with compiler checkout validation and reused the Actions expression visitor for prompt checks. Preserved authored strict/non-strict checkout policy, import uncertainty, exact allowlist matching, value-free prompt diagnostics, and native Actions evidence boundaries. Prompt checks reject multiline/malformed interpolations and blocked property traversal without claiming the full context allowlist. Added registry, CLI, compiler-policy, cancellation, and repository-workflow regressions. No new threat identifier or lock-file schema change. |
| 1.0.61 | Added known compiler threat IDs to every built-in AST rule, propagated registry-owned references through text/JSON diagnostics and rule listings, and moved detailed lint reference content out of the CLI page. Unmapped extension rules can omit the reference. Checks URL-filter SSL prerequisites in compiler-accepted non-strict `sandbox.agent.args`; legacy `network.firewall` blocks require migration rather than an invalid SSL-setting remedy. Added mapping, output, and source-to-compiler regressions without changing the compiler schema. No new threat identifier or lock-file schema change. |
| 1.0.60 | Reviewed the threat catalog against the 18-rule Actions/AW registry and added three authored-AW checks: privileged PR-target role admission (CTR-008 subset), safe-output shell expansion (CTR-009), and URL-filter SSL prerequisites (CTR-011 subset), bringing the registry to 21 rules. Shared the existing lexical shell detector with compiler validation without changing its policy. Preserved strict/non-strict role severity and scoped warning suppression, import uncertainty, value-free diagnostics, and native Actions evidence boundaries. Added registry contracts, CLI regressions, shared-detector tests, and repository-workflow coverage. No new threat identifier or lock-file schema change. |
| 1.0.59 | Required registry-wide positive/negative contracts across supported ASTs and strict/non-strict AW. Added debug-only CLI visibility for secret-leak coverage warnings while preserving internal diagnostics, error findings, analysis failures, and other rules' warnings. Corrected JavaScript object-rest member provenance without filtering genuine secret-derived values. No new threat identifier or lock-file schema change. |
| 1.0.58 | Added AST-only observable subsets for tool scope (CTR-003), sandbox configuration (CTR-004), image argument injection (CTR-013), explicit checkout expansion (CTR-033), and fork author-association values (CTR-034), bringing the registry to 18 rules. Preserved import uncertainty and authored strict policy without inferring runtime provenance, capabilities, visibility, or authorization. Refined opt-in JavaScript masking/public-metadata semantics while retaining unresolved-flow warnings. No new threat identifier or lock-file schema change. |
| 1.0.57 | Converted further compiler policies into AST linters: bare network allowances (CTR-011 subset), unconstrained wildcard PR-branch push targets (CTR-012 subset), enabled Node install hooks (CTR-014), and bare label allowlists (CTR-015). Rules declare AW/Actions AST targets; the CLI accepts `.md`, `.yml`, and `.yaml` without compilation. AW-only checks visit explicit execution nodes and report unavailable generated-flow coverage. Preserved source strict-policy severities, always-warning push findings, deprecated constraint aliases, import uncertainty, and valid domain/label patterns. No new threat identifier or lock-file schema change. |
| 1.0.56 | Added independently observable explicit permission-scope and invalid OIDC-read checks (CTR-001 subset), plus authored AW source checks for disabled version checking (CTR-018), conditional imports (CTR-020), and private-to-public flow opt-outs (CTR-031). Extended the CTR-017 subset with opt-in native Go TypeScript compiler-backed JavaScript analysis. Documented strict-policy severities, native Actions/source-evidence boundaries, explicit JavaScript compiler/source configuration, and deferred authorization/import-resolution obligations. No new threat identifier or lock-file schema change. |
| 1.0.55 | Mapped independent Actions/AW AST lint checks for immutable action references (CTR-002), direct template injection (CTR-006), modeled secret disclosure (CTR-017), workflow-run scope (CTR-021), and literal job timeouts (CTR-026). Documented observable subsets, concrete implementation/test evidence, and candidates requiring source policy, semantic analysis, or runtime evidence. No new `CTR-*` identifier, replacement of existing conformance requirements, or lock-file schema change. |
| 1.0.54 | Added MCP-COMPILE-001–006 for the host-side compilation boundary: startup-only write capability, dedicated read-only mode plus `--no-emit --no-check-update`, early rejection of writing/scanner options, filesystem and Docker image-store preservation, cached-only remote imports and nested includes rooted at the repository, generated-YAML validation independent of stale locks, and write-enabled authorization/emission. Preserved trusted CLI no-emit import population. Mapped concrete regression tests and a bounded TLA+ model with synthetic negative controls and reachability witnesses. No new generated-workflow `CTR-*` rule or lock-file schema change. |
| 1.0.53 | Audit-only clarification that same-repository bot checkout on `pull_request` and `pull_request_target` `ready_for_review` and `reopened` events is a runtime trust control governed by Security Architecture Specification RS-05a, not a new compiler threat rule. |
| 1.0.52 | Audit-only clarification of the safe-output patch/bundle runtime integrity boundary (PTI-001–007 and bounded TLA+ evidence), plus CTR-033 / T-CTR-048 for explicit `checkout.multi: true`, single-repository expression validation in agent and safe-output jobs, mode-preserving merges, and bounded TLA+ negative controls and reachability witnesses. No new generated-workflow compatibility change beyond CTR-033. |
| 1.0.51 | Extends CTR-008 to reject `pull_request_target` with `on.roles` containing `read` or `all` in strict mode; in non-strict mode, the dangerous feature flag suppresses only the roles warning. |
| 1.0.50 | Extended CTR-032 with creating-run traceability for every issue comment, PR description, and PR review comment from first publication. Specified HTML execution markers except percent-encoded Markdown anchor markers for review comments and replies, independent of visible footers, with forged-marker and destination API filtering coverage required. |
| 1.0.49 | Added CTR-032 for step-scoped safe-output publishing identity, replacement of forged execution-provenance comments, and fail-closed runtime metadata validation. |
| 1.0.48 | Audit-only clarification that same-repository checkout for allow-listed App comments and reviews is a runtime trust control governed by Security Architecture Specification RS-05a, not a new compiler threat rule. |
| 1.0.47 | Audit-only clarification that same-repository checkout for allow-listed App comments is a runtime trust control governed by Security Architecture Specification RS-05a, not a new compiler threat rule. |
| 1.0.46 | Extended CTR-017 OTLP exclusions to evals AWF sandboxes and mapped evals-enabled/disabled regression coverage. |
| 1.0.45 | Extended CTR-017 with OTLP collector endpoint/header exclusions for agent and inline/external threat-detection AWF sandboxes when `--exclude-env` is supported; retained host-side exporter access and mapped OTLP-enabled/disabled regression coverage. |
| 1.0.44 | Clarified CTR-001: the `id-token: write` audience/trust-policy reminder is informational and does not increment compiler warnings or block warnings-as-errors validation; OIDC permission enforcement remains unchanged. |
| 1.0.43 | Clarified CTR-008 for the scoped `pull_request_target` checkout policy: exact literal allowlist matching, warning-only risk acknowledgment, and rejection of configured `fetch` values. |
| 1.0.42 | Audit-only clarification that same-repository bot PR checkout authorization is a runtime trust control governed by Security Architecture Specification RS-05a, not a new compiler threat rule. |
| 1.0.41 | Added CTR-031 for strict rejection and non-strict warning of `tools.github.private-to-public-flows`, which can expose private data through public logs or destinations. |
| 1.0.40 | Added CTR-030 for PR-base provenance of activation sparse checkout before loading imports or skills; preserves non-PR refs and the `GITHUB_TOKEN` same-repo fallback guard. |
| 1.0.39 | Added CTR-029 for fail-closed authorization of allowlisted GitHub Apps that send `repository_dispatch`; a conclusive absent collaborator lookup is accepted only for the allowlisted App on that trigger. |
| 1.0.38 | Audit-only review of the safe-outputs workspace checkout discovery path; the agent-writable workspace trust boundary is a runtime control recorded in the safe-outputs (Threat T7, RCR1–RCR7) and checkout-behavior (§3.5, T-CHK-016) specifications, and is not a new compiler threat class. |
| 1.0.37 | Added CTR-028 for base-branch agent configuration restore provenance on pull-request triggers; the restore is emitted after the PR checkout and before any step that installs agent content, for every engine. |
| 1.0.36 | Audit-only review; Opengrep build-reproducibility alerts (`github-actions-npm-install-non-deterministic`, `actions-uv-pip-install-non-deterministic`, `actions-pip-install-inline-no-hash-check`, `github-actions-setup-node-missing-version`, `dockerfile-non-sha-pinned-image`) are external-scanner findings outside conformance scope; no new CTR rule required. |
| 1.0.35 | Audit-only review; open code-scanning alerts (#681/#678/#676/#675 allocation-overflow, #679 useless-assignment, #674/#669/#668/#667 bad-redirect-check, #663 http-to-file-access, #657 smoke-test dummy, #652/#651 stale GraphQL-injection claim, #680 out-of-context stray commit artifacts) are not new compiler threat classes. |
| 1.0.34 | Added CTR-027 for trusted same-repository allowlisted bot synchronization and fail-closed confused-deputy handling. |
| 1.0.33 | Audit-only review; issue #59894's `close_issue.cjs` command-injection claim is a false positive (no `exec`/subprocess call exists in the file). |
| 1.0.32 | Audit-only review; #675–677 and #667–669/#674 are not new threat classes. |
| 1.0.31 | Audit-only review; #672 is not a new threat class. |
| 1.0.30–1.0.27 | CTR-001, CTR-004, and CTR-006 mapping synchronization. |
| 1.0.26 | Added CTR-026 timeout-expression rejection. |
| 1.0.25–1.0.23 | Added CTR-025 and mapping-only audit updates. |
| 1.0.22 | Added suppression and optimizer-failure requirements. |
| 1.0.20–1.0.15 | Added CTR-022, CTR-023, and CTR-021. |
| 1.0.14–1.0.8 | Added CTR-020, CTR-019, CTR-018, CTR-017, and CTR-016. |
| 1.0.7–1.0.0 | Established CTR-001–015, conformance model, and daily reconciliation. |

## Mapping Audits

### Mapping Audit (2026-10-24)

Reviewed the admission-output gating split in [PR #71784](https://github.com/github/gh-aw/pull/71784), extracted from [PR #71711](https://github.com/github/gh-aw/pull/71711) as a partial response to #71710. The compiler now gates admission environment bindings on effective opt-in, propagates enablement into evals and threat-detection execution, and conditionally emits conclusion usage/session admission metadata. Independent environment bindings remain intact.

`TestAdmissionOptIn`, `TestAdmissionComponentBudgetOutputs`, `TestAdmissionSessionOutputs`, `TestAdmissionComponentExecutionWiring`, and `TestUsageActivityGenerationIncludesJobResults` cover the generated bindings. `TestAdmissionOutputGatingCompilesWithLint` compiles workflows with and without detection/evals jobs across both opt-in and dry-run states, requires zero warnings, and checks the emitted output references. Dry-run retains admission jobs, forwarded outputs, and conclusion metadata while omitting admission bindings from component execution; the shared `admissionBudgetOutputsEnabled` helper centralizes only the execution-budget gate. This fixes generated expression consistency without disabling lint or enabling admission unnecessarily. It does not add a threat identifier, checkout lint exception, repository-memory change, or runtime authentication change, and does not claim live admission-service or budget-enforcement evidence.

### Mapping Audit (2026-10-22)

Reviewed the shared PR checkout change in
[`PR #71210`](https://github.com/github/gh-aw/pull/71210),
`actions/setup/js/checkout_pr_branch.cjs`, its regression suite, and Security
Architecture Specification RS-05a. The same-repository `pull_request` fast path
now fetches `+refs/pull/<number>/head:refs/remotes/origin/pr-head` and resets the
local source-named branch from that fetched ref, matching the existing
fork/other-event path. A deleted source branch no longer prevents checkout of
the retained PR head; stale local branches and event SHAs cannot select the
baseline. This is runtime ref isolation, not a new compiler-detectable threat.

The real-Git regressions in `actions/setup/js/checkout_pr_branch.test.cjs`
first reproduce the missing-source-branch fetch failure, then verify exact PR
content, local branch, fetched ref, actual checked-out baseline, and unchanged
shallow/full-clone status for open and closed payloads. They also verify that
the deleted remote branch is not recreated. These isolated local tests do not
prove GitHub's ref-retention policy, live API behavior, or atomicity between
PR-state checks and later effects.

Existing missing-retained-ref and error regressions preserve the distinction
between live-state-confirmed lifecycle invalidation and authentication,
permission, network, or unknown failures. Failed checkout still stops the
agent rather than continuing on base content. Successful retained-head
checkout does not bypass the safe-output manager's live-state checks;
intentional closed-event workflows remain supported. Actor trust, CTR-008
trusted checkout policy, CTR-028/030 configuration provenance, permissions,
conformance test identifiers, and generated lock formats are unchanged.

### Mapping Audit (2026-10-19)

Reviewed staged skill registration in `actions/setup/js/copilot_sdk_session.cjs`,
the permission handler in `copilot_sdk_permissions.cjs`, the mocked session and
driver regressions, SDK driver §3.5/§4.5, and Copilot Opt's recovery guidance.

The driver registers the explicit staged root without ambient discovery in
normal and bare sessions. Absent roots remain empty; files, symlinks, and dangling
symlinks fail before SDK session creation. This is a root-type check, not
recursive entry validation or proof of staged instruction provenance. CTR-028
and CTR-030 continue to govern the compiler-generated provenance controls.

The permission handler rejects shell chains containing unauthorized segments
unless the full command is explicitly allowed. Its rejection callback, not
generic tool errors such as a missing skill, increments the denial budget.
Copilot Opt retains its three-denial threshold and reads staged phase files
directly; prompt guidance does not broaden permissions or bypass enforcement.
CTR-023 still concerns compiler-side engine capability, not a promise that
agent-generated commands will be permitted.

Evidence is in `actions/setup/js/copilot_sdk_session.test.cjs` (staging in
normal/bare sessions, ambient exclusion, and invalid roots) and
`actions/setup/js/copilot_sdk_driver.test.cjs` (permission matching and denial
guard termination). These mocked regressions do not establish native SDK skill
discovery, filesystem integrity, or live workflow recovery. No new `CTR-*` rule,
conformance test ID, permission, suppression, or lock-file schema is introduced.

### Mapping Audit (2026-10-16)

Reviewed the same-repository bot checkout exception for `pull_request` and
`pull_request_target` `ready_for_review` and `reopened` events in
`actions/setup/js/checkout_pr_branch.cjs`, the regression tests, RS-05a, and the
bounded checkout authorization model.

The bot exception still requires a Bot sender whose login exactly matches the
runtime actor and positive runtime, PR-head, and PR-base repository IDs that all
match. Forks, unsupported actions, inconsistent provenance, and non-bot actors
retain the collaborator permission floor. This is a runtime trust clarification,
not a compiler-generated construct or a new `CTR-*` rule.

### Mapping Audit (2026-10-14)

Reviewed `transport_integrity.cjs`, both PR handlers, publication retry callbacks,
and the transport regression tests. Captured validation bytes are consumed
through private snapshots; nonregular files fail promptly, integrity errors
remain permanent, and only registered local Git cleanup may proceed after failure.
Full byte checks remain mandatory at trust boundaries, without full artifact
scans around unrelated local commands. Filesystem causes and safe diagnostics
remain available without publishing artifact contents.

The controls are runtime obligations in Safe Outputs PTI-001–007, with bounded
state-machine evidence in `workflow-security/SafeOutputTransport.tla`. They
complement artifact provenance, detection, authorization, and CTR-005 routing
rather than adding a compiler-detectable construct or a new `CTR-*` rule.

### Mapping Audit (2026-10-12)

Reviewed the allow-listed GitHub App checkout exception for `pull_request_review`
in `actions/setup/js/checkout_pr_branch.cjs`, together with its regression tests,
RS-05a requirements and validation mapping, and the comment/review checkout TLA+
model.

Findings and disposition:

- The review exception requires a Bot sender whose login exactly matches the
  review author and canonically matches the runtime actor, an explicit `on.bots`
  allow-list entry, and a positive runtime repository ID equal to the API-verified
  PR head and base repository IDs.
- Fork PRs, unlisted bots, identity mismatches, missing repository provenance,
  and other actors retain the collaborator-permission floor.
- The compiler emits no generated-workflow construct for this runtime trust
  decision. No `CTR-*` rule is warranted; RS-05a and its formal model specify
  the authorization boundary.

### Mapping Audit (2026-10-09)

Reviewed the allow-listed GitHub App checkout exception for `issue_comment` and `pull_request_review_comment` in `actions/setup/js/checkout_pr_branch.cjs`, together with its regression tests and RS-05a validation evidence.

Findings and disposition:

- The exception requires a Bot sender whose login exactly matches the comment author and canonically matches the runtime actor, an explicit `on.bots` allow-list entry, and a positive runtime repository ID equal to the API-verified PR head and base repository IDs.
- Fork PRs, unlisted bots, identity mismatches, missing repository provenance, and other actors retain the collaborator-permission floor. App slug and `[bot]` identities are treated as equivalent only for comparison with the runtime actor.
- No compiler-generated construct or active CTR rule changed. This is a runtime checkout authorization control governed by Security Architecture Specification RS-05a; no new `CTR-*` rule is warranted.

### Mapping Audit (2026-09-23)

Reviewed the safe-outputs checkout-resolution change in `actions/setup/js/find_repo_checkout.cjs` (workspace git-scan fallback reading `remote.origin.url` under a per-invocation `safe.directory` override) together with its callers in `create_pull_request.cjs`, `push_to_pull_request_branch.cjs`, and `safe_outputs_handlers.cjs`.

Findings and disposition:

- The discovery read is confined to `$GITHUB_WORKSPACE`, uses only `git config --get` (which executes no hooks, pagers, filesystem monitors, or credential helpers), and does not mutate the process environment, so it does not create a compiler-detectable unsafe generated-workflow construct.
- Remote-host spoofing through an agent-planted `.git/config` was identified as a real runtime risk and remediated in the runtime path by constraining scanned remotes to the host of `GITHUB_SERVER_URL` or `github.com`; cross-repository allowlist validation continues to precede any durable trust or push.
- No `CTR-*` rule applies: the compiler emits no construct that selects a safe-outputs checkout directory, and no existing rule mapping changed. The controls are recorded in the Safe Outputs MCP Gateway Specification (Threat T7, RCR1–RCR7) and the Checkout Behavior Specification (§3.5, T-CHK-016).

### Mapping Audit (2026-09-15)

No compiler/parser source diff exists beyond the single squashed commit state (`d8e1aa7`); no candidate threat surfaced from `pkg/workflow/`, `pkg/parser/`, or `actions/setup/` source changes in the review window.

Open high-severity code-scanning alerts (0 open critical) were surveyed across the full result set (~260+ alerts) and are dominated by three Opengrep "pr-action" rule families, all concerning build-reproducibility/dependency-pinning rather than the compiler-generated-workflow threat classes in scope:

- `github-actions-npm-install-non-deterministic` (the large majority of alerts) flags `npm install` (as opposed to `npm ci`) across generated `.lock.yml` files. Tracing the source, every compiler-generated instance (`pkg/workflow/codex_engine.go`'s `BuildStandardNpmEngineInstallStepsNoCooldown`/`generateCodexDockerSbxCLIInstallStep`, and `pkg/workflow/copilot_engine_installation.go`) is a global CLI install with an exact pinned version (e.g. `npm install --ignore-scripts -g @openai/codex@0.153.4`, `npm install --ignore-scripts --no-save @github/copilot-sdk@<version>`), which is the scanner's own documented exception case ("Global installs with pinned versions... are acceptable"). No compiler-side gap exists. One hand-authored, non-compiler-generated workflow (`.github/workflows/test-copilot-github-integration.yml`, no `gh-aw-metadata`/`gh-aw-manifest`/generation header) does install `@github/copilot` unpinned; this is a workflow-hygiene issue in a file outside the compiler's conformance scope (specification Section 1: `pkg/workflow/`, `pkg/parser/`, `actions/setup/`), not a `CTR-*` gap.
- `actions-uv-pip-install-non-deterministic` flags `uv pip install --quiet ... numpy pandas matplotlib seaborn scipy` (no `--frozen`) sourced from the shared markdown snippets `.github/workflows/shared/python-dataviz.md` and `.github/workflows/shared/trending-charts-simple.md`, propagated into several `.lock.yml` files. This concerns third-party visualization dependency reproducibility, not a compiler-injected unsafe behavior; no untrusted input reaches the install command and no privilege-escalation, sandbox-bypass, injection, or unsafe-output-route threat class applies.
- `dockerfile-non-sha-pinned-image` (1 instance, `FROM alpine:3.24` in the repository `Dockerfile`) and `actions-pip-install-inline-no-hash-check` (`.github/workflows/daily-geo-optimizer.lock.yml`, sourced from documentation-only example text in `pkg/workflow/pip_validation.go`) and `github-actions-setup-node-missing-version` (`.github/workflows/format-and-commit.yml`, a hand-authored maintenance workflow, not compiler output) are repository build-tooling and non-compiler-generated-workflow findings, outside `pkg/workflow/`/`pkg/parser/`/`actions/setup/` conformance scope per specification Section 1, which explicitly excludes "external scanner ecosystems."

None of these findings correspond to the five compiler threat classes in specification Section 3 (privilege escalation, sandbox bypass, injection, unsafe output/supply-chain routes, compile-time drift), and CTR-014 (Supply Chain Attack via Install Scripts) already covers the adjacent, in-scope risk of untrusted pre/postinstall script execution — a distinct concern from install-command determinism. No new `CTR-*` rule is warranted; no implementation change is required.

No live `threat-detection-suppress` annotation exists in any workflow frontmatter (only illustrative documentation examples), so no `SLA_BREACH` applies. No suppression is older than 10 or 20 business days because none exist.

### Mapping Audit (2026-09-13)

No compiler/parser source diff exists beyond the single squashed commit state (`0489fac`, "Share API rate-limit state across multi-target logs downloads", #60531); no candidate threat surfaced from `pkg/workflow/`, `pkg/parser/`, or `actions/setup/` source changes in the review window.

Open high/critical code-scanning alerts were reviewed against conformance scope (specification Section 1):

- Alerts #681, #678, #676, #675 (`go/allocation-size-overflow` in `pkg/workflow/compiler_yaml_ai_execution.go`, `pkg/workflow/mcp_cli_mount.go`, `pkg/workflow/mcp_github_config.go`) are the same previously-assessed class of finding — `make([]T, 0, n+m)` capacity hints computed from in-process, already-bounded slice/map lengths (step lines, server-name lists, guard-policy counts), not attacker-controlled magnitudes. No CTR rule applies; consistent with the 2026-09-09/2026-09-10 disposition for this alert class.
- Alert #679 (`go/useless-assignment-to-field` in `pkg/cli/logs_orchestrator_stdin.go`) and alerts #674, #669, #668, #667 (`go/bad-redirect-check` in `pkg/cli/*.go`, `pkg/workflow/graders_config.go`) are code-quality/heuristic-mismatch findings in `pkg/cli/`, outside the `pkg/workflow/`/`pkg/parser/`/`actions/setup/` conformance scope in specification Section 1; the `bad-redirect-check` alerts recur the 2026-09-10 audit's path-containment-guard disposition.
- Alert #663 (`js/http-to-file-access` in `scripts/ensure-docs-slide-pdf.js`) is outside conformance scope (build tooling, not compiler/parser/setup) and already carries an in-code CodeQL suppression comment explaining the fixed-URL, validated-content-type, signature-checked download path.
- Alerts #652/#651 (`workflow-go-graphql-injection-sprintf`, Semgrep) target `pkg/cli/project_command.go` lines 269/272 from an earlier commit (`f5d0fffd`). The current file has no `fmt.Sprintf`-built GraphQL query for the `owner` parameter; `validateOwner`/`getOwnerNodeId` pass `owner` through `runProjectGraphQLQueryWithVariables(..., map[string]any{"login": owner})` using named GraphQL variables, and no `escapeGraphQLString` function exists anywhere in the repository. The finding is stale against current source and also outside `pkg/cli/` conformance scope; no CTR rule applies.
- Alert #657 (`workflow-security-finding-1`) is an explicit smoke-test dummy warning ("Smoke test dummy warning — Run 34294409797") from the `smoke-claude` workflow, not a real finding.
- Alert #680 (`workflow-out-of-context`) flags three files unexpectedly committed in `0489fac`: `test_dup_import` (a 2.3 MB compiled Go ELF binary), `tmp/smoke_test_22524436360.go` (a leftover smoke-test placeholder marked "can be safely removed" in its own header comment), and `{outname}.f` (an empty file with an unresolved template variable name). None of the three are referenced by any build script, Go import, or workflow, and the scanner rates them Low (3/10) as out-of-context artifacts rather than a compiler-generated-workflow threat; they represent stray commit hygiene, not a `CTR-*` detection gap, so no rule change applies. Removal is a housekeeping action outside the specification's scope (compiler rule catalog), not a threat-detection rule.

No live `threat-detection-suppress` annotation exists in any workflow frontmatter (only illustrative documentation examples), so no `SLA_BREACH` applies.

### Mapping Audit (2026-09-11)

Issue #59894 (`[sighthound] Security findings in github/gh-aw`) reported a Critical CWE-78 command-injection finding claiming untrusted `commentBody`/`params` reach `exec` in `actions/setup/js/close_issue.cjs` (and a `pkg/workflow/js/close_issue.cjs` mirror) around lines 5 and 1110. Verification against conformance scope found this to be a false positive: `close_issue.cjs` is 402 lines total (no line 1110 exists), contains no `exec`/`child_process`/`spawn` call anywhere, and `pkg/workflow/js/close_issue.cjs` does not exist in the repository. `commentBody` in `close_entity_helpers.cjs` flows only into the authenticated GitHub API client (`callbacks.addComment`), never into a shell or subprocess argument, so CTR-006 (Template Injection) and CTR-013 (Argument Injection via Package/Image Names) triggers do not apply and no new `CTR-*` rule is warranted. No `threat-detection-suppress` annotation was added because the finding does not correspond to any real code path the compiler needs to suppress — it is an inapplicable external scan result about nonexistent code, not an active false-positive-prone compiler rule.

Open high/critical code-scanning alerts (#675–#677 `go/allocation-size-overflow`) remain unchanged from the 2026-09-10 audit disposition: in-process, schema-validated capacity-hint computations, not exploitable by untrusted input. No live `threat-detection-suppress` annotation exists in any workflow frontmatter. No compiler/parser source diff exists beyond the single squashed commit state, so no candidate threat surfaced from source changes.

CTR-027 records the runtime pre-activation control for allowlisted bots that synchronize PRs they did not open. The allowlist alone MUST NOT override confused-deputy protection. Authorization is permitted only for `pull_request` or `pull_request_target` `synchronize` events when repository IDs match (or the head repository name matches the base repository if IDs are unavailable), the original PR author satisfies `on.roles`, the bot is installed and active, and the actor is not `dependabot[bot]`. Missing provenance, fork PRs, untrusted authors, inactive bots, and Dependabot author mismatches fail closed. The implementation logs the decision inputs and trust outcome without logging credentials or event payload contents.

### Mapping Audit (2026-09-10)

CTR-001–026 have implementation and test references with no `TODO` placeholders. The available repository history is a single squashed commit (`099efdd`, dated 2026-09-09 17:19 -0700); no additional compiler/parser diff exists beyond that state, so no new candidate threat surfaced from source changes. No live `threat-detection-suppress` annotation exists in any workflow frontmatter (only illustrative documentation examples in `.github/aw/syntax-agentic.md` and reference docs), so no `SLA_BREACH` applies.

Open high/critical code-scanning alerts were reviewed against conformance scope: alert #677 (`go/allocation-size-overflow` in `pkg/workflow/mcp_setup_generator.go`) was already assessed in the 2026-09-09 audit as an in-process, schema-validated map capacity computation, not a new compiler threat class. Alerts #675–#676 (`go/allocation-size-overflow` in `pkg/workflow/mcp_github_config.go`) are the same class of finding — capacity hints computed from already-bounded, schema-validated slice/map lengths (`dynamicEnclaveGitHubGuardRepos`, `staticEnclaveGitHubGuardPolicies`) — and are not exploitable by untrusted input; no CTR rule applies. Alerts #667–#669 and #674 (`go/bad-redirect-check`) flag `strings.HasPrefix` path-containment guards in `pkg/cli/add_package_manifest_imports.go`, `pkg/cli/add_package_manifest_includes.go`, and `pkg/workflow/graders_config.go`; these are manifest path-traversal guards, not HTTP redirect validators, and already carry in-code rationale comments explaining the CodeQL heuristic mismatch. Other reviewed alerts (`js/http-to-file-access`, `workflow-security-finding-1`, `workflow-out-of-context`, `workflow-go-graphql-injection-sprintf`) are outside `pkg/workflow/`, `pkg/parser/`, and `actions/setup/` conformance scope per specification Section 1.

Historical audits through 2026-09-09 confirmed existing coverage or recorded mapping-only updates for CTR-001, CTR-004–007, CTR-009–012, CTR-016–021, CTR-023, and CTR-025; no audit added an uncovered threat class.
