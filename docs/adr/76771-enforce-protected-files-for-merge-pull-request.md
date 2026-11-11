# ADR-76771: Enforce Protected-File Checks for merge-pull-request

**Date**: 2026-11-11
**Status**: Draft
**Deciders**: pelikhan (PR author) [TODO: verify other reviewers involved in the decision]

---

### Context

`merge-pull-request` is a safe-output handler that merges a pull request on behalf of a workflow. Before this change it relied on protected-file caution notices in the PR rather than inspecting the actual file changes, so a PR without those notices could merge changes to protected files (workflow, engine, and manifest files) into an otherwise eligible feature branch. `create-pull-request` already defines a protected-file set, matching rules, and exclusions, and the merge path needs the same protection without a second, drifting definition. The merge path must fail closed: incomplete or unverifiable file evidence must block the merge rather than allow it.

### Decision

We will block protected-file changes by default in `merge-pull-request`. The handler reuses the `create-pull-request` protected set, matching rules, and exclusions; lists the complete live PR file list, including deleted paths and both sides of renames; and fails closed on missing, malformed, duplicate, or incomplete evidence, including PRs over GitHub's 3000-file listing limit. The same check repeats at final revalidation before the merge API call. The primary driver is safety: the handler inspects what a merge would actually change instead of trusting notices.

### Alternatives Considered

#### Alternative 1: Keep relying on caution notices (status quo)

Leave protected-file handling to the caution notices already present on PRs. This is the lowest-effort option and adds no API calls. We rejected it because the notices describe what a PR is supposed to contain and do not verify the diff. A PR without notices could merge protected files into an eligible branch, which is the gap this change closes.

#### Alternative 2: Check protected files only at evaluation time

Evaluate the file list once, when the merge is first evaluated, and skip re-checking before the merge call. This is simpler and needs fewer list calls. We rejected it because the PR head or file set can change between evaluation and merge. The final revalidation narrows that window, although it does not close it entirely (see Negative consequences). [TODO: verify whether single-evaluation was seriously discussed during review.]

#### Alternative 3: Reuse create-pull-request's request-review or fallback-to-issue policies

Support the same `request-review` and `fallback-to-issue` policies that `create-pull-request` offers when protected files are touched. We rejected these for merge-pull-request because they assume a new PR or issue can be created as a fallback, and a merge has no equivalent artifact. The PR intentionally leaves them unsupported; the handler accepts only `blocked`, `allowed`, or a GitHub Actions expression.

### Consequences

#### Positive
- Protected-file changes are blocked by default, so merges no longer depend on caution notices being present.
- The protected set, matching rules, and exclusions are shared with `create-pull-request`, so a single definition governs both handlers.
- Fail-closed evidence handling means a partial or malformed file list cannot authorize a merge.
- Re-checking at final revalidation catches file changes made after the initial evaluation.

#### Negative
- PRs with more than 3000 changed files are blocked, because the file listing cannot be verified completely.
- Each evaluation and revalidation adds paginated file-list API calls.
- A race window remains between final revalidation and the merge API call. GitHub atomically enforces only the evaluated head SHA, not file state.
- `protected-files: allowed` is an explicit workflow-owned opt-out that removes this protection for that workflow.
- Teams that need review-based handling of protected files must merge those PRs manually, since `request-review` and `fallback-to-issue` are unsupported here.

#### Neutral
- Compiler configuration, the JSON schema, audit failure codes, tool descriptions, and docs are updated to match.
- Object-form scoped exclusions (`{ policy, exclude }`) are supported for `merge-pull-request`, so changes to the exclusion list affect this handler as well as `create-pull-request`.
- The `changed_files` count is added to the policy snapshot, so a change in file count between evaluation and revalidation is treated as a policy change.
- The PR reports focused parser and compiler tests, focused JavaScript suites, and a full `make agent-report-progress` pass with no lock-file drift across 365 workflows. [TODO: verify these results against CI before accepting the ADR.]

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
