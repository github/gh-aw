# ADR-79058: Inline gh-aw Release Highlights and Version in Update and Upgrade PRs

**Date**: 2026-11-18
**Status**: Accepted
**Deciders**: pelikhan (PR author)

---

### Context

`gh aw update` and `gh aw upgrade` open pull requests that change workflow pins and generated files, either in one repository or across many with `--org`. Their titles were generic (`Update workflows from source`, `Upgrade agentic workflows`), and the description linked to the matching gh-aw release without summarizing it, so a reviewer had to leave the PR to learn what changed. This extends [ADR-69378](69378-centralized-generated-pr-descriptions-with-custom-message-append.md), which centralized how these descriptions are built.

Release highlights are optional external data. Lookup failure, unavailable development releases, or an oversized summary must not prevent an otherwise valid PR. Org rollouts launch separate worker processes, so an in-memory cache cannot prevent repeated lookups. Versioned titles must also preserve discovery of marker-less single-repository PRs for deduplication and replacement.

### Decision

Generated update and upgrade titles include the running gh-aw version, for example `Update workflows from source (gh aw v1.2.3)`. Their descriptions retain the matching release link and include its curated `Release Highlights` section when available. Extraction recognizes H1–H3 headings, retains the section's subheadings, and excludes attribution, separators, and generated changelog content.

Fetched highlights are labeled as untrusted reference data, with an explicit instruction not to follow their contents, and enclosed in a text fence longer than any embedded backtick run. This is a presentation boundary and defense in depth, not a guarantee against prompt injection.

The complete candidate description is checked against GitHub's 65,536-character limit, including custom messages, rollout markers, and the version footer. If highlights would exceed the limit, they are omitted rather than truncating required content. Existing validation still rejects an oversized required description before PR side effects.

Lookup uses the running release version on github.com, including for GHES targets. Development, describe, and dirty versions without a release link skip the lookup. Fetch and decode failures warn and fall back to the release link. Org and enterprise rollouts resolve highlights once before launching workers and share the version-tagged result through the existing per-run cache directory. Empty and failed lookup results are shared too, avoiding repeated requests and lookup warnings. Existing rollout cleanup removes the cache.

Rollout discovery continues to prefer the command-specific body marker. Without a marker, it accepts either the exact legacy title or that title with a nonempty `(gh aw v...)` suffix, independent of the currently running version. The corresponding generated branch prefix remains required, preserving discovery of both older releases and `vdev` PRs without treating unrelated branches as rollouts.

### Alternatives Considered

#### Alternative 1: Keep the release link only

This was the existing behavior. It needs no network call at PR creation time and does not depend on release-note formatting, but reviewers still have to leave the PR to learn what changed. The link remains the fallback when highlights are unavailable or would exceed the description budget.

#### Alternative 2: Inline the full release body

Inlining the entire release body would include the generated `What's Changed` list, which is mostly commit subjects. Those lines bury the summary reviewers need and consume the description budget. Only the curated section is included, and it remains optional if even that section is too large.

#### Alternative 3: Fail PR creation when the release lookup fails

Failing closed would make the release summary a hard requirement for every update PR. A transient github.com outage or a GHES environment without github.com access would block an update that does not depend on the summary. The summary is informational, so lookup failures warn and preserve the release link instead.

#### Alternative 4: Fetch or memoize highlights inside each rollout worker

Per-worker fetching adds one request per repository; an in-memory memo cannot be shared across the worker processes. A version-tagged file in the existing run-scoped cache shares success and failure results without introducing a persistent cache or stale results between runs.

#### Alternative 5: Match only the new title or remove branch checks

Matching only the current versioned title would hide legacy and older-version marker-less PRs. Matching a broad title prefix without branch checks would also classify unrelated PRs as rollouts. Recognizing the legacy title and its version suffix while retaining generated branch prefixes preserves the existing fallback's scope.

### Consequences

#### Positive
- Reviewers see the gh-aw version and, when available within the description budget, the release summary without leaving the PR.
- Titles carry the version (for example `(gh aw v1.2.3)`), so update PRs can be sorted and searched by release.
- Development builds, unreleased describes, and dirty builds never request a release or link one that does not exist. The unit tests cover `dev`, `v1.2.3-4-gabcd`, and `v1.2.3-dirty`.
- Release lookup failures degrade to a warning and keep the release link; org rollouts share the result across workers.
- Legacy and versioned marker-less update and upgrade PRs remain discoverable for deduplication and replacement, while branch checks remain intact.

#### Negative
- A single-repository PR or an org rollout adds one release lookup to github.com for a release version. Run-scoped cache I/O adds another possible rollout setup failure; it is not treated as a release lookup failure.
- The extractor depends on the release workflow's `Release Highlights` heading and attribution markers. A missing section returns no highlights without a warning, so a change to the release format would silently drop the summary. Only fetch and decode errors warn.
- External automation that matches only the old exact title strings must adapt to the version suffix.
- Fenced highlights are less richly rendered than ordinary Markdown, and consumers must still treat the enclosed text as untrusted.

#### Neutral
- The release is fetched with `--hostname github.com` even when the target is on GHES, so GHES users depend on github.com reachability to get highlights.
- Optional highlights can be omitted for size without changing custom messages, rollout markers, required summaries, or footer validation.
- Development builds show `vdev` in titles, consistent with `normalizeDisplayVersion`.
