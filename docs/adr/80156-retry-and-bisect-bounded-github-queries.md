# ADR-80156: Retry and Bisect Bounded GitHub Queries

**Date**: 2026-11-22
**Status**: Draft
**Deciders**: pelikhan (PR author); gh-aw maintainers [TODO: verify reviewers]

---

### Context

The shared MCP scripts `github-issue-query` and `github-pr-query` (defined in `.github/workflows/shared/github-queries-mcp-script.md`) let daily reporting workflows collect PRs and issues over a date window by calling `gh`. Each bounded query is capped at `RANGE_LIMIT` items (default 1,000), and the previous code exited with an error when a window reached that cap. Any single transient `gh` failure or malformed JSON response also aborted the whole collection. Linked issue #80151 records a daily performance summary run that reported incomplete because its 90-day candidate data could not be collected. The fix must keep the exact timestamp filter and must not return truncated data without an error.

### Decision

We will retry each GitHub CLI query up to three times when it fails or returns non-array JSON, and we will split any date window that reaches the result cap at its midpoint day, recursing until every window returns fewer items than the cap. Results from all windows are merged, deduplicated by `number`, and filtered by exact `updatedAt` timestamps; a single day that remains saturated fails explicitly. The primary driver is completeness: daily reports must not silently omit items.

### Alternatives Considered

#### Alternative 1: Keep failing fast at the cap

Keep the previous behavior: exit with "retry with a smaller date range" when a window saturates, and leave recovery to the caller. This is the simplest option and the status quo. We rejected it because a rerun over the same window hits the same cap, and a single transient `gh` error still aborts the whole collection.

#### Alternative 2: Paginate the search results

Page through results with `gh api` cursors or GraphQL pagination instead of splitting ranges. We rejected it because GitHub's Search API limits each query to 1,000 results regardless of paging, so pagination cannot reach items beyond the cap; only narrower queries can. The unbounded path already pages through REST list endpoints, so pagination is not the missing piece. [TODO: verify Search API cap against current GitHub documentation]

#### Alternative 3: Retry only, without splitting

Add the retry helper but keep one query per window. We rejected it because retries cannot recover items beyond the cap: a saturated window returns the same 1,000 items on every attempt. It was a close call because it is the smaller change.

### Consequences

#### Positive
- Transient `gh` failures and malformed JSON responses are retried up to three times instead of aborting the collection.
- Saturated date windows are split recursively, so items beyond the cap are collected.
- Duplicate items across split windows are removed by `number`, and the exact `updatedAt` filter is preserved.
- A saturated single day fails with an explicit error rather than returning partial data.
- The behavior is covered by a stubbed-`gh` test for both the issue and PR scripts.

#### Negative
- Failure cases cost more API calls: up to three attempts per query, and each saturated window spawns two sub-queries, so dense ranges multiply search requests. [TODO: verify impact on search rate limits]
- Backoff is linear (1s, then 2s) with no jitter, so it may not ride out longer outages or secondary rate limits.
- The logic is duplicated across the issue and PR branches of the shared script and copied into six regenerated lock files, so a fix requires regenerating every consumer.
- Retries also apply to the unbounded `gh issue list`, `gh pr list`, and `gh api` paths, which changes their failure behavior too.

#### Neutral
- The six lock files (`daily-performance-summary`, `daily-regulatory`, and four `smoke-copilot` variants) change only in metadata and generated script bodies; no new runtime dependency is added.
- Splitting works on whole days because the search `updated:` filter is date-based; time-of-day precision comes from the final `updatedAt` filter.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
