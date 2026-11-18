# ADR-79058: Inline gh-aw Release Highlights and Version in Update and Upgrade PRs

**Date**: 2026-11-18
**Status**: Draft
**Deciders**: pelikhan (PR author); pending maintainer review

---

### Context

`gh aw update` and `gh aw upgrade` open pull requests that change workflow pins and generated files, either in one repository or across many with `--org`. Their titles were generic (`Update workflows from source`, `Upgrade agentic workflows`), and the description linked to the matching gh-aw release without summarizing it, so a reviewer had to leave the PR to judge whether the change was safe to merge. The CLI runs on user machines, must find the release for the version it is running, must not block PR creation when that lookup fails, and must not request a release for development builds that have none. This extends [ADR-69378](69378-centralized-generated-pr-descriptions-with-custom-message-append.md), which centralized how these descriptions are built.

### Decision

We will add the running gh-aw version to the title of generated update and upgrade PRs and append the curated `Release Highlights` section of the matching GitHub release to their descriptions, keeping the release link. The primary driver is reviewer comprehension: a reviewer can judge a pin change from the PR itself. Extraction stops at the release workflow's attribution, the `---` separator, and the generated changelog. Lookup failures warn without blocking PR creation, and development builds skip the lookup.

### Alternatives Considered

#### Alternative 1: Keep the release link only

This was the existing behavior. It needs no network call at PR creation time and does not depend on how release notes are formatted. It was not chosen because reviewers still had to open the release to learn what changed, which is the problem this change addresses. It was a close call: the link alone remains a defensible fallback if the lookup cost becomes a problem for org-wide runs.

#### Alternative 2: Inline the full release body

Inlining the entire release body would include the generated `What's Changed` list, which is mostly commit subjects. Those lines bury the summary reviewers need and push bodies toward GitHub's 65,536-character limit, which [ADR-69378](69378-centralized-generated-pr-descriptions-with-custom-message-append.md) treats as a hard error. The extractor keeps only the curated section, so this option was rejected.

#### Alternative 3: Fail PR creation when the release lookup fails

Failing closed would make the release summary a hard requirement for every update PR. The lookup goes to github.com, so a transient outage, or a GHES environment without github.com access, would block a dependency update that does not depend on the summary. The summary is informational, so the implementation warns and keeps the release link instead.

### Consequences

#### Positive
- Reviewers see the release summary and the gh-aw version in the PR title and body, so they can triage pin changes without leaving the PR.
- Titles carry the version (for example `(gh aw v1.2.3)`), so update PRs can be sorted and searched by release.
- Development builds, unreleased describes, and dirty builds never request a release or link one that does not exist. The unit tests cover `dev`, `v1.2.3-4-gabcd`, and `v1.2.3-dirty`.
- Lookup failures degrade to a warning and keep the release link, so PR creation is unaffected.

#### Negative
- Each generated PR makes one more API request to github.com. In `--org` runs the request count grows with the number of target repositories.
- The extractor depends on the release workflow's `Release Highlights` heading and attribution markers. A missing section returns no highlights without a warning, so a change to the release format would silently drop the summary. Only fetch and decode errors warn.
- Rollout discovery matches titles exactly (`isRolloutPR` in `pkg/cli/org_rollout_discovery.go`). Titles now include the version. PRs without the body marker, which the single-repository `update` and `upgrade` paths create, are matched only by title and will no longer be recognized. This PR does not update that comparison, so it must be resolved before this ADR is accepted.
- Any external automation that matches the old title strings stops matching.

#### Neutral
- The release is fetched with `--hostname github.com` even when the target is on GHES, so GHES users depend on github.com reachability to get highlights.
- In the `--org` path, highlights are appended before `validatePRBody` runs, so a long curated section counts toward GitHub's 65,536-character body limit.
- Development builds show `vdev` in titles, consistent with `normalizeDisplayVersion`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
