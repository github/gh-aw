# ADR-32227: Centralize Condition and Token Helpers and Normalize Entity Helper Naming

**Date**: 2026-05-15
**Status**: Draft
**Deciders**: pelikhan, Copilot


---

## Part 1 — Narrative (Human-Friendly)

### Context

`pkg/workflow` had three independent forms of organizational drift that made symbol location hard to predict from a function's purpose. First, `ConditionNode`-returning `build*Condition` helpers (`buildCommentAuthorAssociationCondition`, `buildSkipAuthorAssociationsCondition`, `buildDetectionSuccessCondition`, `buildDetectionPassedCondition`) lived in compiler files (`compiler_pre_activation_job.go`, `compiler_safe_outputs_job.go`) even though `expression_builder.go` is the canonical home for `ConditionNode` construction. Second, the create-entity parser methods used inconsistent names (`parseIssuesConfig`, `parseDiscussionsConfig`, `parsePullRequestsConfig`) that did not match the verb-prefixed convention used by `generateCreateIssuesJob` and peers, and three update-entity files carried a redundant `_helpers` suffix (`update_issue_helpers.go`, `update_discussion_helpers.go`, `update_pull_request_helpers.go`) inconsistent with the create-entity file naming. Third, token-resolution functions (`computeEffectivePRCheckoutToken`, `computeStaticCheckoutToken`, `computeProjectToken`, `computeProjectURLAndToken`) lived in `safe_outputs_config_helpers.go` alongside unrelated config serialization utilities, and used `compute*` naming despite being resolution-precedence functions semantically aligned with the other `resolve*`/`getEffective*` helpers already in `github_token.go`. These three drifts continue the file-placement-by-convention work established in [ADR-27325](27325-organize-workflow-helpers-by-semantic-responsibility.md) and extended by [ADR-28282](28282-relocate-outlier-functions-to-semantic-homes.md) and [ADR-29336](29336-relocate-misplaced-functions-to-semantic-homes.md).

### Decision

We will consolidate ownership of three concern groups in `pkg/workflow` so that symbol location and naming match responsibility. All `ConditionNode`-returning `build*Condition` helpers will reside in `expression_builder.go`, the canonical `ConditionNode` home, with compiler files consuming them rather than owning them. The create-entity parser methods will be renamed to the verb-prefixed convention (`parseCreateIssuesConfig`, `parseCreateDiscussionsConfig`, `parseCreatePullRequestsConfig`), and the three update-entity files will drop the redundant `_helpers` suffix (`update_issue.go`, `update_discussion.go`, `update_pull_request.go`) to mirror the create-entity naming. Token-resolution helpers will move to `github_token.go` and adopt the `resolve*` prefix (`resolvePRCheckoutToken`, `resolveStaticCheckoutToken`, `resolveProjectToken`, `resolveProjectURLAndToken`), leaving `safe_outputs_config_helpers.go` focused on safe-output config serialization. All moves and renames are intra-package and behavior-preserving; the change is purely structural.

### Alternatives Considered

#### Alternative 1: Leave Helpers in Their Current Files and Document the Drift

Inline comments could note that, for example, `buildDetectionSuccessCondition` semantically belongs with the expression builders but lives in `compiler_safe_outputs_job.go` for historical reasons. This was rejected because documentation without structural enforcement degrades: future contributors continue placing similar helpers in whichever file they happened to be editing, and the drift compounds. This alternative was already considered and rejected in [ADR-28282](28282-relocate-outlier-functions-to-semantic-homes.md) for the same reason, and applying the same fix here keeps the codebase consistent with prior decisions.

#### Alternative 2: Keep Existing Names and Only Move Functions

A narrower refactor could relocate the helpers without renaming them, preserving `parseIssuesConfig` and `computeEffectivePRCheckoutToken` to minimize call-site churn. This was rejected because the inconsistent verbs (`parse*` without entity, `compute*` for resolution-precedence logic) actively mislead readers about each function's role. Renaming alongside relocation lets the new file location and the new prefix reinforce each other, and the call-site churn is mechanical and contained to a single PR.

#### Alternative 3: Introduce a `helpers/` Subpackage

All cross-cutting utilities could be lifted into a `pkg/workflow/helpers/` subpackage to enforce separation. This was rejected because it would introduce a new import boundary and force every consumer to take a new dependency, which is much heavier than the actual problem (file placement within a single package). The existing semantic-file-naming convention from [ADR-27325](27325-organize-workflow-helpers-by-semantic-responsibility.md) already solves this with no new package surface.

### Consequences

#### Positive
- All `ConditionNode` construction helpers live in `expression_builder.go`, making expression-tree building logic discoverable in one file rather than scattered across compiler files.
- Create-entity parsers (`parseCreateIssuesConfig`, `parseCreateDiscussionsConfig`, `parseCreatePullRequestsConfig`) match the verb-prefixed naming of `generateCreateIssuesJob` and peers, so reading a call site immediately conveys "this is the create-issue parser."
- Token-resolution functions are co-located in `github_token.go` with the other token helpers and consistently named with the `resolve*` prefix, so future contributors locating token-precedence logic find it by file name and recognize related helpers by prefix.
- Reinforces the semantic file-organization convention from [ADR-27325](27325-organize-workflow-helpers-by-semantic-responsibility.md), [ADR-28282](28282-relocate-outlier-functions-to-semantic-homes.md), and [ADR-29336](29336-relocate-misplaced-functions-to-semantic-homes.md), extending it to expression builders, parser naming, and token resolution.

#### Negative
- `git blame` on the relocated functions surfaces the relocation commit rather than original authorship without `--follow`, adding friction for anyone tracing the history of a builder or resolver.
- Renames touch a large number of call sites and tests in a single PR (every `parseIssuesConfig`/`parseDiscussionsConfig`/`parsePullRequestsConfig`/`compute*Token` caller), increasing the surface area for accidental conflicts with concurrent in-flight branches.
- The `_helpers` suffix on file names is no longer reserved as a marker for "helper-only" files, which slightly weakens the file-name convention if it had been load-bearing elsewhere; current usage in this PR confirms it was redundant.

#### Neutral
- No public package API changes; all renamed functions are unexported and all moves are intra-package.
- `safe_outputs_config_helpers.go` narrows in scope to safe-output config serialization utilities only.
- Documentation and code comments referencing the old function/file names are updated alongside the rename (e.g., `DEVGUIDE.md`, `config_helpers.go` docstrings, test names referring to `parseDiscussionsConfig`).

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### ConditionNode Builder Placement (`pkg/workflow`)

1. All exported and unexported functions that return `ConditionNode` and follow the `build*Condition` naming pattern **MUST** reside in `pkg/workflow/expression_builder.go`.
2. New `ConditionNode`-returning builder functions **MUST** be added to `expression_builder.go` and **MUST NOT** be placed in compiler job files such as `compiler_pre_activation_job.go` or `compiler_safe_outputs_job.go`.
3. Compiler job files **MUST** consume `ConditionNode` builders by calling helpers in `expression_builder.go` rather than defining their own builder logic inline.

### Create-Entity Parser Naming (`pkg/workflow`)

1. Compiler methods that parse a single create-entity safe-output configuration block **MUST** be named with the `parseCreate{Entity}sConfig` pattern (e.g., `parseCreateIssuesConfig`, `parseCreateDiscussionsConfig`, `parseCreatePullRequestsConfig`).
2. Create-entity parser methods **MUST NOT** use the bare `parse{Entity}sConfig` form without the `Create` verb prefix.
3. New create-entity safe-output parsers **MUST** follow the `parseCreate{Entity}sConfig` naming pattern.

### Update-Entity File Naming (`pkg/workflow`)

1. Files owning the configuration type and helpers for a single update-entity safe output **MUST** be named `update_{entity}.go` (e.g., `update_issue.go`, `update_discussion.go`, `update_pull_request.go`).
2. Update-entity files **MUST NOT** carry a redundant `_helpers` suffix; the `update_` prefix already communicates the file's purpose.

### Token Resolution Helper Placement and Naming (`pkg/workflow`)

1. Functions that resolve a GitHub token by walking a precedence chain across safe-output configuration **MUST** reside in `pkg/workflow/github_token.go`.
2. Token-resolution functions **MUST** use the `resolve*` prefix when their primary responsibility is to walk a precedence chain and return a resolved token (e.g., `resolvePRCheckoutToken`, `resolveStaticCheckoutToken`, `resolveProjectToken`, `resolveProjectURLAndToken`).
3. Token-resolution functions **MUST NOT** use the `compute*` prefix; `compute*` is reserved for pure derivations that do not perform precedence resolution.
4. `safe_outputs_config_helpers.go` **MUST NOT** contain token-resolution helpers; its scope **MUST** be limited to safe-output config serialization utilities.
5. New token-precedence resolvers **MUST** be added to `github_token.go` with the `resolve*` prefix.

### Behavior Preservation

1. The rename and relocation **MUST NOT** alter the runtime behavior of any compiled workflow output.
2. Tests that exercise the renamed parsers and resolvers **MUST** be updated to call the new names, and **MUST NOT** rely on the old symbol names being preserved as aliases.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement — in particular, placing a `ConditionNode` builder outside `expression_builder.go`, naming a create-entity parser without the `Create` verb, retaining a `_helpers` suffix on an update-entity file, or placing a token-precedence resolver outside `github_token.go` or under the `compute*` prefix — constitutes non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/25893510358) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
