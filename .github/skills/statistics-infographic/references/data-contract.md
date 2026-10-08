# Data contract and interpretation

Use [the synthetic example](../examples/example.json) as the complete minimal input
template. One JSON object has five sections. Alternatively, `--data-dir` maps them
to these filenames:

| Section | Filename | Required evidence |
|---|---|---|
| `origins` | `pr-initiation-assumption-summary.json` | Repository, census date, authorized assumption, three disjoint origin buckets, seven components, outcome counts |
| `histogram` | `pr-time-to-merge-histogram.json` | Same repository/date, merged population, contiguous duration bands, median and P90 in hours |
| `community` | `community-statistics-summary.json` | Repository, timezone-aware collection timestamp, labelled/eligible issues, accounts, issue outcomes, deduplicated delivery PRs |
| `structure` | `repo-structure-statistics.json` | Repository, full commit ID, file/nonblank-line totals, four Go/JavaScript implementation/test groups |
| `history` | `weekly-code-history.json` | Same repository/commit, contiguous Monday-start weeks, additions/deletions and totals |

Counts must be nonnegative integers, not booleans, strings, or floats. All
repositories must match. PR and histogram census dates must match; community has
its own collection time because collection is not atomic. Code and history share
one full 40- or 64-character hexadecimal Git object ID. Optional precomputed
percentages and ratios consumed by the model must agree with the derived rounded
values. Unused metadata is not independently validated.

## PR populations

Use these exact origin names:

- `AW-started`
- `Human-started, including assumed direct sessions`
- `Other / unattributed`

Each has merged, closed-without-merging, and open counts. Their sum must equal the
census. Origin breakdowns and component sums must reconcile. An optional `All PRs`
outcome record must also match.

The source auditor follows this documented **inference policy**, not observed
session-initiation telemetry:

- Direct AW PR: `github-actions` author and recognized workflow provenance marker.
- CCA: `copilot-swe-agent` author. Explicit issue references in the CCA suffix take
  precedence; otherwise combine closing-issue links and supported closing-keyword
  references from the body, restricted to the repository and authorized aliases.
- AW issue: Bot author plus recognized workflow provenance marker.
- Human-account issue: User author without recognized bot/machine-user naming.
- No identified source issue: assume human initiation **only with user
  authorization**, recorded in `assumption`.
- Missing/deleted authors, other automation, and mixed sources remain unattributed.

Account type is not proof of a person, and issue provenance is not proof of the
session initiator. Do not infer phone use or causality. The seven component fields
are shown in the example; the auditor verifies the four named AW/human components
and aggregate outcomes, not a finer subdivision of the residual other bucket.

Origin shares divide by all PRs. Completed merge rate divides merged by
merged + closed-unmerged, excluding open PRs. The displayed ratio divides merged
by closed-unmerged, not by all closed states including merged PRs.

## Time to merge

Measure `mergedAt - createdAt` in UTC for merged PRs only. Boundaries are
lower-inclusive and upper-exclusive; `null` is the final unbounded upper limit.
Bands must cover `[0, infinity)` without gaps/overlap and include boundaries at
1, 6, and 24 hours. Counts sum to the merged population.

Median and P90 use sorted elapsed times with linear interpolation at
`(n - 1) * q`. The model checks ordering and histogram-implied feasible ranges;
only raw-record auditing independently verifies the exact quantiles. Empty
merged populations require `null` quantiles. Percentages under each threshold use
merged PRs as denominator, never all PRs.

Count-derived percentages use exact fractions rounded half up to one decimal;
merge ratios use two decimals. Undefined zero denominators show `n/a`, not 0%.
Histogram/time geometry uses floating-point coordinates, not displayed count
arithmetic.

## Community outcomes

The labelled-issue population must be collected explicitly and completely.
Eligible third-party issues have a User author and association `NONE`,
`CONTRIBUTOR`, `FIRST_TIMER`, or `FIRST_TIME_CONTRIBUTOR`. Distinct author logins are
accounts, not verified people. This differs from CCA source classification and
does not independently exclude every machine-user account.

A landed issue is closed and has a linked merged PR targeting `main` in the same
repository. Landed, closed-without-such-a-link, and open issues form a disjoint
partition. Delivery PRs are deduplicated by number, independently of the issue
count; one PR can link multiple issues. CCA delivery share divides by distinct
delivery PRs. Links do not prove released impact, work causality, or sole credit.

## Code and history

The code scope is Go and JavaScript (`.go`, `.js`, `.cjs`, `.mjs`, `.jsx`), not all
repository languages. Inventory excludes dependency/build/fixture paths,
generated filenames/headers, and the auditor's explicit bundle exclusions.
Classification follows the auditor's `excluded`, `group_key`, and generated-header
rules. The four groups contain distinct language/role pairs; file and nonblank
line totals must reconcile. Test-line share is code volume, **not test coverage**.

History uses complete first-parent traversal at the pinned commit, UTC committer
weeks, root diffs, and merge diffs against the first parent. Renames count as
deletions/additions. Weekly diff totals include blank lines, unlike the code map.
Empty weeks between the first and last selected-code changes are retained.
Historical files whose generated status cannot be identified by the known
path/current-header filters may remain in churn. The auditor verifies each weekly
pair and that net additions minus deletions equals the current physical-line
inventory. It does not equate physical lines with nonblank lines.

## Cached raw records

`--raw-dir` contains flat UTF-8 JSONL records, not GraphQL response envelopes:

- `pr-statistics-data.jsonl`: every census PR's number, state, author login, body,
  createdAt, and mergedAt.
- `cca-issue-links.jsonl`: each CCA PR number and `closingIssuesReferences` with
  `totalCount` and `nodes`; issue nodes include number, body, repository
  `nameWithOwner`, and author login/`__typename`.
- Optional `cca-extra-issues.json`: issue-number keys mapped to cached issue
  metadata or `null` for unavailable issues. `not_an_issue: true` identifies a
  reference that is actually a PR and is excluded from source issues.
- `community-issues-data.jsonl`: every labelled issue's number, state,
  author login/`__typename`, authorAssociation, and
  `closedByPullRequestsReferences`. PR nodes include number, mergedAt,
  baseRefName, repository `nameWithOwner`, and author login.

Every consumed connection's `totalCount` must equal its node count; paginate
before saving. Duplicate PR/issues, missing CCA inventories/issue metadata,
unknown states, negative durations, and conflicting delivery metadata fail.
The auditor reconciles saved evidence; it does not establish that an upstream
collection was exhaustive or independently recollect the live repository.
