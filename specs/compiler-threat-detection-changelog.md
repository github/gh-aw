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
