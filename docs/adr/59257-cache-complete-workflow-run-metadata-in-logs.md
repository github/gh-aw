# ADR-59257: Cache complete workflow run metadata in logs

**Date**: 2026-09-07
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

`gh aw logs` previously omitted workflow-run metadata that downstream consumers had to retrieve separately with `gh api`. This PR updates the logs processing pipeline under `pkg/cli/` to fetch the full GitHub Actions run response, persist it as `run.json` beside each run's cached artifacts, and reuse that cache when possible. The diff also changes report-building logic so repository, actor, event, SHA, workflow path, and run attempt data come from GitHub metadata first and are overridden only by non-empty `aw_info.json` values. The repository needs an explicit decision for whether workflow run metadata should be treated as a first-class cached input of the logs subsystem rather than as an optional post-processing lookup.

### Decision

We will fetch the complete GitHub Actions workflow-run API response during `gh aw logs` processing, cache it per run as `run.json`, and use it as the baseline source for run metadata in logs reports. We will preserve and refresh that cache for both newly downloaded and previously cached runs, and we will exclude `run.json` from artifact inventories while retaining it during storage pruning. Non-empty `aw_info.json` fields will remain authoritative overrides so workflow-emitted metadata can correct or augment GitHub-provided values without discarding the GitHub baseline.

### Alternatives Considered

#### Alternative 1: Keep metadata uncached and require downstream consumers to call `gh api`

This matches the previous behavior where logs output omitted some workflow-run metadata and consumers had to fetch it separately. It was considered because it keeps the logs pipeline simpler and avoids storing another API response on disk. It was not chosen because the PR description and diff both show repeated downstream need for repository, actor, event, attempt, commit, and workflow-path data, making separate follow-up API calls redundant and harder to reuse.

#### Alternative 2: Cache only a reduced summary instead of the full workflow-run API response

The project could translate the GitHub response directly into `run_summary.json` fields and avoid storing raw API output. This was considered because it would reduce stored data volume and keep the cache schema narrower. It was not chosen because the diff explicitly adds `run.json`, validates malformed or mismatched cached responses, and uses the raw response as a reusable source of truth that can backfill older summaries when metadata fields are later needed.

#### Alternative 3: Treat `aw_info.json` as the sole source of metadata

Another option would be to continue relying on workflow-emitted metadata files for repository, actor, SHA, event, and attempt data. This was considered because `aw_info.json` is already part of the logs ecosystem and can contain workflow-specific context. It was not chosen because `aw_info.json` may be absent or partially populated, and the diff intentionally changes precedence so GitHub API metadata provides a reliable baseline while `aw_info.json` overrides only non-empty fields.

### Consequences

#### Positive
- Downstream logs consumers can read complete run metadata from cached output without making separate GitHub API calls.
- Newly downloaded runs and previously cached runs share the same metadata source and healing behavior, improving consistency of `run_summary.json` and report output.
- The logs report gains richer repository, organization, actor, attempt, SHA, and event fields even when `aw_info.json` is missing or incomplete.

#### Negative
- Each processed run now requires an additional GitHub Actions API request and an extra cached file, increasing API usage and local storage.
- The logs subsystem must maintain parsing, cache validation, and merge logic for raw workflow-run responses in addition to existing summary and jobs caches.
- Metadata precedence is more complex because the system must combine GitHub baseline fields with selective `aw_info.json` overrides.

#### Neutral
- `run.json` becomes a preserved internal cache artifact that is intentionally excluded from artifact listings shown to users.
- Existing cached runs may be rewritten as metadata is backfilled or healed, even when no new workflow artifacts are downloaded.
- Tests now need to cover both cache reuse behavior and metadata precedence between GitHub responses and `aw_info.json`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
