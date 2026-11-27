# ADR-81692: Split repo-memory generation into focused files

**Status:** Draft
**Date:** 2026-11-27
**PR:** [#81692](https://github.com/github/gh-aw/pull/81692)
**Linked issue:** #81667 (file-diet request)

## Context

`pkg/workflow/repo_memory.go` had grown to 1087 lines, above the 800-line threshold referenced by issue #81667. A single file mixed three concerns:

- configuration parsing and types (`RepoMemoryConfig`, `RepoMemoryEntry`, `extractRepoMemoryConfig`, `applyRepoMemory*`)
- artifact upload and workflow step generation (`generateRepoMemorySteps`, sanitize/filter/validation/upload step generators)
- push-job and GitHub App token generation (`buildPushRepoMemoryJob`, `repoMemoryApp*`)

The matching test file, `repo_memory_test.go`, was 1905 lines. The PR body states the refactor must not change implementation behavior. Whether compiled lock output is unchanged is not confirmed by the PR evidence reviewed here.

## Decision

Split the repo-memory code by responsibility into four files in package `workflow`, as a mechanical move:

- `repo_memory_config.go` (424 added lines): types, default branch naming, config parsing.
- `repo_memory_steps.go` (304 lines): artifact upload, filter, validation, and upload step generators, step IDs, base-commit outputs.
- `repo_memory_push_job.go` (257 lines): push job construction, setup/checkout/download steps, job condition, outputs.
- `repo_memory_app_token.go` (124 lines): GitHub App token mint and target-selection helpers.

Tests are split to mirror these files. Two new tests cover app-token target selection and read-target gating.

Why: each file stays under 500 lines, responsibilities map to file names, and a mechanical move is easier to verify in review.

Evidence: `pr-files.json` shows `repo_memory.go` removed (1087 deletions) and four new source files. The function list in `pr.diff` shows the same signatures moved. Comparing non-blank added and removed lines, only 2 removed lines have no added counterpart. The added-only lines are file headers, imports, build tags, and new test assertions.

## Alternatives Considered

These alternatives are inferred from PR evidence. The author did not record them.

1. **Keep the single file (status quo).** Rejected: the file stays at 1087 lines, above the threshold, and mixes three concerns.
2. **Split only the tests, or only the configuration code.** Rejected: this leaves the production generation code (steps and push job) in one large file, so the size problem persists.
3. **Split into many small files (one per generator or helper).** Rejected: this creates more navigation overhead for a file that has three cohesive responsibilities plus a small app-token domain.

## Consequences

**Positive**
- Each source file is under 500 lines, and each file name indicates its responsibility.
- The move is mechanical, so reviewers can check it against the removed file by diffing function bodies.
- Two new tests add coverage for app-token target selection and read-target gating.

**Negative**
- Helpers used across domains now live in different files. Readers follow references across files, although the package API is unchanged.
- Line-level `git blame` history for the moved functions is disrupted by the move.
- The PR evidence reviewed here does not confirm that tests pass, that lint and build succeed, or that lock files are unchanged. These acceptance criteria from #81667 must be verified before merge.
