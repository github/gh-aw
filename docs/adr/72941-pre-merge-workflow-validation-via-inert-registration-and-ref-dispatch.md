# ADR-72941: Pre-Merge Workflow Validation via Inert Registration and `--ref` Dispatch

**Date**: 2026-10-29
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent (PR #72941) [TODO: verify]

---

### Context

GitHub Actions only allows manual dispatch of a workflow whose file already exists on the repository's default branch. Authors of new or changed agentic workflows want to validate candidate workflows before merging. `gh aw run --ref` already dispatches a branch's compiled workflow content, but it cannot register a new workflow and cannot reproduce a real `pull_request_target` event. Users hit opaque GitHub errors ("not found on the default branch", "does not have 'workflow_dispatch' trigger") with no guidance (issue #72647). Any workaround must not weaken the trust boundary around elevated credentials and untrusted PR code.

### Decision

We will not add an ephemeral or tool-managed registration mechanism, nor any bypass of GitHub's default-branch requirement. Instead, `gh aw run` will keep its existing `--ref` behavior and translate the two relevant GitHub dispatch errors into actionable diagnostics (a shared `workflowDispatchRegistrationHint` in `pkg/cli/run_workflow_validation.go`, used by `formatWorkflowRunError` and `validateRemoteWorkflow`). The documented pre-merge path is a maintainer-reviewed, inert, dispatch-only bootstrap lock file (`permissions: {}`, job guarded by `if: ${{ false }}`) merged at the candidate's exact lock-file path, followed by `gh aw run --ref <branch>` against the published candidate. The primary driver is preserving security boundaries while making platform constraints explicit.

### Alternatives Considered

#### Alternative 1: Ephemeral registration built into `gh aw run`/`trial`

The CLI could temporarily commit a workflow to the default branch (or swap the default branch) to register it, dispatch, then revert. This would offer a one-command experience, but it mutates the default branch automatically, needs elevated write permissions, and risks leaving production state inconsistent. Rejected; the PR explicitly states "No ephemeral registration or elevated-trust bypass is introduced."

#### Alternative 2: Simulate `pull_request_target` by dispatching the PR ref with elevated credentials

The tool could run untrusted PR workflow content under privileged credentials to emulate `pull_request_target`. This reproduces event behavior more faithfully but is the classic "pwn request" pattern and breaks fork trust rules. Rejected on security grounds; docs instead recommend a controlled repository with test credentials.

#### Alternative 3: Documentation only, no CLI diagnostics

Updating docs alone would be smaller, but users would still see raw GitHub errors without a pointer to the guidance. Rejected because error-time hints are where users actually discover the constraint.

### Consequences

#### Positive
- Users get actionable error messages linking to the "Run from a feature branch" guidance.
- No new privileged code paths; default-branch changes remain human-reviewed.
- Help text and flag description clarify that `--ref` selects content, not registration.

#### Negative
- New workflows still require merging a bootstrap file to the default branch before any live test.
- `pull_request_target` behavior cannot be validated end-to-end in the production repository.
- Diagnostics depend on matching GitHub error-message substrings, which may break if GitHub changes wording.

#### Neutral
- Regression tests cover ref forwarding (local/remote), dispatch diagnostics, and help text.
- The bootstrap pattern is plain GitHub Actions YAML, not compiler output, so it lives outside compiler guarantees.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
