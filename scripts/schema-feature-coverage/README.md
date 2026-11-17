# Schema Feature Coverage measurement, version 1

This package proposes a reviewed freeze of the **textual schema-example
coverage** measurement for `github/gh-aw`'s
[`schema-feature-coverage`](../../.github/workflows/schema-feature-coverage.md)
workflow.
The deterministic evaluator, [contract](contract.v1.json), semantic tests and
[pinned replay](replay.v1.json) are reviewed together. Historical selection was
retrospective: this contract was **not** prospectively frozen in March 2026.
Review and merge establish the adoption revision, not the historical replay date.

## Measurement and authority

The primary native metric is **covered fields**, always accompanied by the
eligible-field denominator and sorted missing-field inventory. The population
is the schema's top-level `.properties` at each source commit. A field matches
an unindented `^FIELD:` line in a regular Markdown Git blob recursively under
either `.github/workflows` or `schema-demos`; field names are regex-escaped.
Both directories are searched throughout history, including fenced examples.
No frontmatter parsing, demo compilation, correctness, defect prevention, hours
saved or ROI is claimed. Actors are irrelevant to the repository outcome.

All source revisions are full Git commit IDs on the pinned first-parent chain.
Git blobs, not the current worktree or archive export filters, supply evidence.
Each observation emits its source hashes. Missing inventory is unavailable with
null counts, not zero or a previous result. Invalid schema, absent Git objects
or conflicting replay assertions fail explicitly.

Exact accepted-commit/first-parent witnesses separate new examples, lost
coverage and schema population changes. Retirement never counts as an example.
The six selected event windows have seven daily samples before the first merge
and seven starting at the last merge, zero maturation lag. The batch merge
interval belongs to neither period. The seven-day post-window endpoint is
separate. Ratios are not averaged, and shared overlapping windows cannot be
summed as exclusive effects.

## Reproduce offline

From a complete checkout containing the pinned historical objects, run:

```bash
python3 -B -m unittest discover -s scripts -p 'test_schema_feature_coverage.py'
SCHEMA_COVERAGE_REPLAY=1 python3 -B -m unittest discover -s scripts -p 'test_schema_feature_coverage.py'
python3 -B scripts/schema_feature_coverage.py --repository . > "$TMPDIR/schema-coverage-replay.json"
```

The default replay never resolves `origin/main`, reads the wall clock, calls
GitHub, fetches objects, edits files, dispatches workflows or publishes anything.
It preserves the last exploratory manifest's tip
`0114567b59e2a2333fcc7d6323503520e7e9b8ab`, observation as-of
`2026-11-17T17:27:00.610367+00:00`, and final source cutoff
`2026-11-17T12:00:00+00:00`. A single source snapshot can be inspected with
`--commit FULL_SHA`; it must belong to that same pinned chain. These are local
script options, not new `gh aw` CLI commands.

`replay.v1.json` pins exact contract/evaluator SHA-256 digests and asserts the
original 36 weekly count pairs, 17 accepted-merge count pairs and a digest over
all 84 daily samples (timestamps, commits, counts, missing fields and schema
fields). The evaluator verifies these before emitting a replay. Digests use
exact file bytes, except the daily projection which uses sorted-key, ASCII JSON
with separators `(',', ':')`. The JSON result includes the replay-input digest
and per-file source hashes, so repeated executions produce identical output.
The original exploratory JSON file hashes record provenance without requiring
private machine paths or shipping redundant API responses. The complete result
is regenerated from public Git evidence; those original files are not required.
PR numbers and GitHub merge timestamps are preserved metadata assertions from
that audit, independently inspectable at each returned PR URL; Git proves their
accepted source deltas, not human-decision or producer identity.

Do not refresh these pins to moving refs. Preserve adopted version-1 bytes and
results. A semantic correction requires a new version, evaluator, fixture and
separately labelled result; never reinterpret an old assessment in place.

## Retrospective evidence

The 36 snapshots from March 22 through November 17, 2026 move from **32/45** to
**75/75**, with initial worsening and later setbacks retained in the series.
The common retained 39-field cohort moves from **30/39** to **39/39**; six of
the original 45 fields retired. These are population-aware observations, not a
workflow-exclusive treatment effect.

| Selected event | Accepted PRs | Direct example additions | Other change |
| --- | ---: | ---: | --- |
| May 4 batch | 2 | 2 | None |
| May 18 batch | 8 | 7 | `dependencies` retired in [#32978](https://github.com/github/gh-aw/pull/32978), 48/58 to 48/57 |
| June 15 batch | 4 | 4 | None |
| [#63949](https://github.com/github/gh-aw/pull/63949) | 1 | 5 | 63/68 to 68/68 |
| [#72028](https://github.com/github/gh-aw/pull/72028) | 1 | 1 | 70/71 to 71/71 |
| [#74061](https://github.com/github/gh-aw/pull/74061) | 1 | 1 | 71/72 to 72/72 |

The early 13 additions are direct accepted-merge changes, not necessarily
permanently retained examples or exclusively attributable interval benefit.
This fixture is a selected demonstration, not a complete delivery inventory.
Merge timestamps are adoption proxies, not a reimplementation of native
lifecycle classification. The earlier native September audit found explicit
human-verified adoption for producer `36389625388`, but only snapshot survival;
later native queries did not validate identity. Those external lifecycle claims
are not reconstructed or upgraded by this source-only replay.

## Integration boundary

**Unregistered offline package.** gh-aw's
[`graders.operational-value.script`](../../.github/skills/operational-value-designer/SKILL.md)
is a restricted, run-scoped evaluator over recorded session evidence, not this
longitudinal measure. Nothing is added to the workflow, generated lock,
conclusion job or grader runtime. Workflow intent and activation remain unchanged.

CAO owns its reviewed `schemaVersion: 3` importable definition and
`collectBatch(requests, context)` / `scoreMetric(metricId, evidence)` compact
exchange, distinct from rich acquisition, registration and publication.
This local contract deliberately does **not** impersonate those schemas or
register a collector. A future adapter belongs at that supported consumer
boundary and must undergo its own schema/policy review. No CAO storage,
dashboard, portfolio reporting or scheduler is introduced here.

All `attributed_value` results remain **null**. Finding-to-implementation
lineage, authoritative native lifecycle identity and complete post-adoption
survival remain unestablished. An elapsed window is not survival. No qualified
attributed Value, causal effect, exclusive credit, collection activation,
deployment or publication is claimed.
