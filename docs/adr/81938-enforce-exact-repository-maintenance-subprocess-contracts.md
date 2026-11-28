# 81938. Enforce exact-repository maintenance subprocess contracts

- **Status:** Draft
- **Date:** 2026-11-28
- **PR:** [#81938](https://github.com/github/gh-aw/pull/81938)
- **Issue:** None linked

## Context

Automation such as CAO runs the `gh aw` maintenance commands (`update`, `upgrade`, `enable`, `disable`) as subprocesses against a repository that is not the caller's working directory. Before this change, those commands had gaps that made them unreliable to script:

- `enable` and `disable` with `--repo` still resolved workflow files from the local checkout, so a caller without the target checked out could not select registered workflows.
- `disable` treated run cancellation as best effort. A cancellation failure printed a warning and the workflow was disabled anyway. Already-disabled workflows skipped cancellation entirely, so their in-progress runs were not stopped.
- Child processes were buffered with `CombinedOutput` and were not bound to the command context. Callers saw no output until the child exited, and cancellation did not stop running children.
- `enable` skipped a workflow that had no GitHub entry and failed to compile, after printing only a warning.
- `update` and `upgrade` logged failures from codemods, action pinning, container pins, and compilation as warnings and still reported completion. Extension-release selection, including `--pre-releases`, and the `--repo` and PR flags were not handled consistently across the relaunch into the target checkout.

The PR goal is to make these commands usable against exact repositories with reliable subprocess semantics. CAO keeps responsibility for authorization and confirmation.

## Decision

Treat the maintenance commands as subprocess contracts for exact repositories:

1. **Targeting.** With `--repo [HOST/]owner/repo`, `enable` and `disable` select registered `.lock.yml` workflows through the GitHub API and do not need a local checkout. Workflow IDs match exactly, and `.md` and `.lock.yml` suffixes are accepted. Omitting IDs selects registered agentic workflows only, not ordinary Actions YAML files. Every selection is resolved and validated before any mutation.
2. **Release selection.** `upgrade` selects extension releases, including prereleases with `--pre-releases`, before it enters the target checkout. `--repo` and PR flags are preserved across relaunch. `upgrade --audit` rejects `--repo`, `--org`, and `--enterprise`.
3. **Fail-closed cancellation.** `disable` paginates all in-progress runs for each targeted workflow, including workflows that are already disabled. Each cancellation must succeed before the disable call runs. A listing, cancellation, or disable failure returns a nonzero exit status.
4. **Propagated errors.** `update` and `upgrade` return errors from codemods, action and action-reference updates, container pin updates, compilation, and artifact or pin-file writes. They do not report completion after a failure, and they do not create a pull request.
5. **Streamed, cancellable subprocesses.** Child `gh` and `git` processes run under the command context. Their output is streamed while they run. Cancellation terminates checkout and extension descendants, and child exit status is preserved.
6. **Documented best-effort exceptions.** Extension-release discovery, registry digest resolution, and rate-limit-only source skips in `update` stay non-fatal. Their skips appear in diagnostics or `--jsonl` events.

## Alternatives Considered

1. **Keep warn-and-continue semantics and document them.** A caller would keep getting a zero exit status after a cancellation or disable failure. An orchestrator could then report a workflow as disabled while its runs continue. Rejected.
2. **Require callers to clone the target and use the local-checkout path.** This needs no CLI change, but every caller would then own checkout, cleanup, and workflow-file parity. The API already provides the registration data the CLI needs. Rejected.
3. **Poll cancelled runs to a terminal state before disabling.** This would stop runs more completely than acknowledgement alone. It adds latency, timeouts, and a second polling contract. Deferred. The PR documents acknowledgement-only cancellation and snapshot scope instead.
4. **Add a separate maintenance subcommand family for subprocess callers.** This would leave existing commands unchanged but duplicate flags, help text, and documentation for the same operations. Rejected in favor of additive `--repo` semantics on the existing commands.

## Consequences

**Positive**

- Scripted callers get a meaningful exit status. A failed cancellation prevents the disable for that workflow.
- Exact-repository `enable` and `disable` no longer need a local checkout. Invalid selections fail before any mutation.
- Streamed output and context-bound child processes let callers observe progress and stop long operations.
- `update` and `upgrade` no longer report success after a failed step.

**Negative**

- Stricter failure semantics change behavior for existing automation. For example, `enable` now fails when a workflow with no GitHub entry does not compile, where it previously skipped the workflow with a warning.
- Multi-workflow selections can partially apply. Changes already made are not rolled back when a later step fails.
- Cancellation covers only the in-progress snapshot. Queued runs and runs started after discovery are not covered, and GitHub's acknowledgement is not polled to completion.
- Best-effort discovery remains, so a zero exit status does not prove that every release, source, or digest was refreshed.
- Paginated run listing and subprocess streaming add API calls and code complexity.
- CAO's `dashboard_actions.go` is outside this repository. The contract is verified against the CLI and Go entry points, not a pinned CAO revision, so caller conformance is not established by this repository.
