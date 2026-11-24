# ADR-80754: Split Compilation Pipeline into Focused Stages

**Date**: 2026-11-24
**Status**: Draft
**PR**: #80754
**Linked issue**: #80720

## Context

`pkg/cli/compile_pipeline.go` had grown to 1162 lines, above the 800-line threshold recorded in issue #80720. Its two orchestrators, `compileSpecificFiles` and `compileAllFilesInDirectory`, were both marked `nolint:largefunc`. Each duplicated per-file compilation bookkeeping (stats, results, lock-file tracking, batch-tool inputs). Batch tool runners, duplicate-name notices, purge tracking, and post-processing shared the same file.

## Decision

Split the pipeline by responsibility and move the shared per-file bookkeeping into one stage type used by both orchestrators:

- `compile_pipeline.go` (238 lines): the two orchestrators, which now delegate to `compilePipelineStage`.
- `compile_pipeline_stages.go` (new, 124 lines): `compilePipelineStage` holds the compiler, config, stats, results, workflows, lock files, batch-tool options, and success/failure counters. Its `compile()` and `recordFile()` methods implement per-file bookkeeping for both paths.
- `compile_pipeline_batch_tools.go` (new, 295 lines): batch linters, scanners, and external tool runners.
- `compile_pipeline_notices.go` (new, 155 lines): duplicate-name warnings, batch notices, and MCP progress writing.
- `compile_pipeline_postprocess.go` (new, 195 lines): post-processing and output.
- `compile_pipeline_purge.go` (new, 63 lines): purge tracking.

The PR description states that the public API and compilation behavior are preserved. This draft has not independently verified that claim.

## Alternatives Considered

1. **Split files only, keeping per-file bookkeeping duplicated in each orchestrator.** This is a smaller change. It leaves the duplicated compile-and-record logic in place and keeps both orchestrators large.
2. **Move the pipeline into a separate package.** This gives a stronger boundary. It would require exporting internals that are currently package-private in `pkg/cli`, which enlarges the API surface. It is not part of this PR's diff.

## Consequences

**Positive**

- The orchestrator file drops from 1162 to 238 lines, and each concern lives in a focused file.
- Per-file bookkeeping lives in one place and serves both the specific-file and directory paths.
- The new `compile_pipeline_stages_test.go` (105 lines) covers stage bookkeeping, per the PR description.

**Negative**

- Five new files in `pkg/cli`. The stage struct holds pointers to shared stats and results, so the order of stage calls becomes an implicit contract.
- The diff also regenerates 71 `.lock.yml` files that change the pinned Claude Code CLI version from `2.1.331` to `2.1.325` (observed in `pr-files.json` patches). This is unrelated to the decision and must be confirmed or reverted separately.
</content>
<parameter name="path">/home/runner/work/gh-aw/gh-aw/docs/adr/80754-split-compile-pipeline-into-focused-stages.md