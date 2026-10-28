# ADR-72542: Static Review Warnings for Privileged Checkout and Cross-Run Artifact Execution

**Date**: 2026-10-28
**Status**: Draft
**Deciders**: pelikhan, [TODO: verify other reviewers]

---

### Context

Workflows triggered by privileged events (`pull_request_target`, `workflow_run`, `issue_comment`, `repository_dispatch`, `pull_request_review`, `pull_request_review_comment`) run with write tokens and secrets. The checkout that the compiler manages already enforces strict `pull_request_target` policy, actor guards, and a pinned checkout action. Author-written steps can still bypass these protections. Examples include `actions/checkout` with `allow-unsafe-pr-checkout`, PR-head refs, `gh pr checkout` or `git fetch refs/pull/...`, and `workflow_run` consumers that download artifacts from another run into the workspace and then execute them. The compiler already has an offline AST lint registry (`pkg/workflow/lint`) that parses Actions/AW YAML, Actions expressions, and Bash/sh scripts (`mvdan.cc/sh`). Any new protection must not ban trusted custom actions, safe base checkouts, or data-only artifact consumers, and compilation must stay offline and deterministic.

### Decision

We will add two built-in lint rules to the default registry: `privileged-checkout` and `artifact-execution`. Both are warning-level review diagnostics in strict and non-strict mode, and they are not mandatory compiler rejections or runtime enforcement. The rules use bounded static analysis. They parse Actions expressions and Bash/sh command ASTs instead of matching text, and they report only high-confidence patterns: true or dynamic unsafe-checkout opt-outs, PR/dispatch/upstream-head selectors, literal `refs/pull/` refs, direct `gh pr checkout` and Git source-selection calls, cross-run downloads into the workspace root or an unknown path, and later direct execution inside the literal download destination in the same execution block. The existing strict PRT policy, template-injection protections, and embedded checkout pin stay unchanged and independent. The rules also do not invent vulnerability status for historical or unknown checkout SHAs.

### Alternatives Considered

#### Alternative 1: Hard compiler rejection of unsafe checkout and artifact patterns

Make these patterns compile errors, at least in strict mode, in the same way the existing strict `pull_request_target` policy blocks compiler-managed checkout. This would give the strongest guarantee. It was rejected because the analysis is bounded. It does not follow environment indirection, `cd`, nested commands, third-party action internals, or cross-job flows, and it cannot verify author-owned provenance checks. Some intentional, trusted execution chains would be blocked with no clean opt-out. Warnings let reviewers decide without breaking existing workflows.

#### Alternative 2: Text/regex-based detection or whole-program provenance analysis

A regex scan of step source would be simpler but noisy. It would match expressions inside quoted strings and miss bracket-access selectors. Whole-program taint and provenance tracking across jobs, actions, and artifacts would cover more cases, but it is far more complex and would need network lookups, for example to resolve action tags or affected-version data. The AST-based bounded approach reuses existing parsers and execution-graph traversal, keeps false positives low, and keeps compilation offline.

#### Alternative 3: Rely only on documentation and existing strict PRT policy

Keep the current protections and document the risks of custom checkout and artifact use. This adds no maintenance cost. It was rejected because custom steps can cross privileged boundaries without any compiler signal, and the existing policy only covers compiler-managed checkout.

### Consequences

#### Positive
- Authors and reviewers get compile-time signals for common privileged-checkout and artifact-poisoning patterns ("pwn request" style) in custom steps.
- Analysis based on parsed ASTs and expressions reduces false positives compared with text matching. Benign patterns (safe base checkouts, current-run downloads, data-only consumers in dedicated directories) are not flagged.
- Compilation stays offline and deterministic. No action tags are resolved, and no SHAs are labeled vulnerable without evidence.

#### Negative
- These are warnings only, so a workflow with the flagged patterns still compiles and runs. Disabling lint removes the diagnostics.
- Coverage is bounded. Indirection, `cd`, aliases, wrappers, other interpreters, third-party action internals, cross-job flows, and dynamic paths are not modeled, so a clean result does not prove safety.
- Intentional trusted execution chains may still produce warnings, because author-owned validation is not interpreted as proof.
- About 400 lines of new rule logic plus tests and spec text add maintenance to the lint package and the threat-detection spec (version 1.0.74).

#### Neutral
- No new `CTR-*` threat identifier, permission grant, or lock-file schema change is introduced.
- Malformed modeled shell syntax produces an incomplete-analysis warning. Unsupported shells are skipped.
- Workflow-linter reference docs and the compiler threat-detection spec (§7.4.2) are updated to describe the scope and limits of both rules.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
