# ADR-78857: Merge Imported Cache Configuration into Compiled Workflows

**Date**: 2026-11-18
**Status**: Draft
**Deciders**: pelikhan (PR author); [TODO: verify reviewers and final decider]

---

### Context

Shared workflow files pulled in through `imports:` can declare a `cache:` block. The import pipeline already collected these entries into `MergedCaches`, but nothing in `pkg/workflow` consumed them. As a result, imported cache configuration was silently dropped: the compile succeeded with no warning, no cache steps were generated, and the detection-enabled agent job kept `cache-mode: read` (see ADR-72543). The same block declared in the importing workflow works. The bug is tracked in #78854. Shared workflows already contribute `jobs`, `checkout`, and `tools`, so importers should be able to share a dependency cache without repeating its configuration.

### Decision

We will merge `cache:` entries declared in imported shared workflows into the compiled workflow's cache configuration. Imported entries are appended in discovery order, followed by entries from the importing workflow, and the combined list drives both cache step generation and the cache-write policy from ADR-72543. The primary driver is correctness: a cache declaration in an imported file must produce the same compiled output as the same declaration written locally.

### Alternatives Considered

#### Alternative 1: Warn on imported `cache:` and leave it unmerged

Emit a compile-time warning when an imported file declares `cache:`, and document that cache configuration is not merged. This is the smallest change and would end the silent failure. It was not chosen because shared workflows would still be unable to provide a writable dependency cache, so every importer would keep repeating the block. The PR description does not record a rejection rationale [TODO: verify author reasoning]. The issue treats this option as a minimum fix, not the preferred one.

#### Alternative 2: Deduplicate cache entries by `key` with main-workflow precedence

Treat entries with the same `key` as overrides, with the importing workflow winning, similar to how `mcp-servers:` is merged (imports override same-named main servers, first-wins across imports) and how job outputs are merged in ADR-72407. This avoids duplicate cache steps for a shared key. It was not chosen in this PR because `key` values can contain expressions, and it is unclear whether `path` lists should be merged or replaced. The implementation appends every entry without deduplication. How the generator handles two entries with the same key is [TODO: verify].

### Consequences

#### Positive
- Imported `cache:` declarations now generate cache and restore steps, which fixes the silent drop reported in #78854.
- Shared workflows can own a dependency cache once instead of requiring every importer to copy the block.
- Merge order is deterministic (discovery order, then the importing workflow), so compiled output is reproducible.
- A regression test (`TestImportedCacheGeneratesStepsAndEnablesCacheWrites`) covers both an imported and a local configuration, asserting four `actions/cache` steps and no `cache-mode: read`.

#### Negative
- Existing workflows that import a shared file with `cache:` will get different lock files on the next compile: new cache steps and a different cache-mode. This corrects previously wrong output, but it is still a visible change.
- Imported cache entries now enter the dependency-cache path from ADR-72543. On trusted events, that path gives the detection-enabled agent job the platform's default write-capable token before detection runs. A shared file can therefore widen write capability for importers that were previously restore-only [TODO: verify the effective `cache-mode` for an imported-only configuration on trusted events].
- Duplicate `key` values across imports or the main workflow produce separate cache entries with no warning [TODO: verify generator behavior].

#### Neutral
- The import reference (`docs/src/content/docs/reference/imports.md`) now lists `cache` as a supported import field and documents its merge order.
- Unlike `checkout` (ADR-26292), where main entries come first and win through first-seen-wins deduplication, cache entries are not deduplicated. Ordering therefore affects only the order of generated steps, not precedence.
- Implementation is a `mergeImportedCaches` helper in `pkg/workflow/cache_import_merge.go`, invoked from `mergeImportedWorkflowConfiguration` in `compiler_orchestrator_workflow.go`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
