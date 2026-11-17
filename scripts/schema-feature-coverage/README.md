# Schema Feature Coverage measurement contract, version 1

This proposal defines the **textual schema-example coverage** measurement for
[`schema-feature-coverage`](../../.github/workflows/schema-feature-coverage.md).
Review the [versioned contract](contract.v1.json) with the compact
[historical evidence](evidence.v1.json). No evaluator or runtime tests are
included. Review and merge establish the adoption revision: the contract was
selected retrospectively, **not prospectively frozen in March 2026**.

## Measurement rationale

The primary native metric is **covered fields**, accompanied by the eligible
denominator and sorted missing-field inventory. The population is every
top-level `.properties` key at each source revision. Search regular Markdown
Git blobs recursively under both `.github/workflows` and `schema-demos` for
an unindented, regex-escaped `^FIELD:` line. Both directories apply throughout
history, avoiding a relocation discontinuity; fenced examples count.

This intentionally follows the workflow's textual check, not frontmatter
parsing, demo compilation, runtime correctness, defect prevention, hours saved
or ROI. Repository outcomes count regardless of actor. Missing evidence is
unknown, not zero. Retain changing denominators, regressions and retired-field
inventories; retirement never counts as a newly covered example.

Selected merges use exact accepted-commit/first-parent witnesses. Seven daily
samples cover `[first merge - 7 days, first merge)` and
`[last merge, last merge + 7 days)`, with zero maturation lag. The batch merge
interval belongs to neither period; the post-window endpoint is separate.
Never average daily ratios or independently sum overlapping windows as
exclusive effects.

## Historical evidence and provenance

The evidence preserves source tip
`0114567b59e2a2333fcc7d6323503520e7e9b8ab`, final source cutoff
`2026-11-17T12:00:00+00:00`, observation as-of
`2026-11-17T17:27:00.610367+00:00`, 36 weekly count pairs and 17 selected merge
witnesses. The original exploratory evidence hashes are retained for provenance;
the original audit files and its Python research runner are not dependencies
or shipped runtime components.

Weekly observations move from **32/45 to 75/75**, retaining initial worsening
and later setbacks. The common retained 39-field cohort moves from **30/39 to
39/39**; six original fields retired. These are population-aware observations,
not a workflow-exclusive treatment effect.

| Selected event | Accepted PRs | Direct example additions | Other change |
| --- | ---: | ---: | --- |
| May 4 batch | 2 | 2 | None |
| May 18 batch | 8 | 7 | `dependencies` retired in [#32978](https://github.com/github/gh-aw/pull/32978), 48/58 to 48/57 |
| June 15 batch | 4 | 4 | None |
| [#63949](https://github.com/github/gh-aw/pull/63949) | 1 | 5 | 63/68 to 68/68 |
| [#72028](https://github.com/github/gh-aw/pull/72028) | 1 | 1 | 70/71 to 71/71 |
| [#74061](https://github.com/github/gh-aw/pull/74061) | 1 | 1 | 71/72 to 72/72 |

The early 13 additions are direct accepted-merge changes, not necessarily
permanently retained examples. This is a selected demonstration, not a complete
delivery inventory. GitHub merge timestamps are preserved adoption proxies,
not native lifecycle classifications. The audit inspected 84 daily samples;
the compact evidence does not ship those full inventories or a replay harness.

The immutable witnesses can be inspected without an evaluator:

```bash
# Example: inspect #63949's accepted change and exact first parent.
git show --format=fuller --stat 62524032a5d44809fd0cf558bb8110fc1c127d70
git diff 5fe849d5e82e66889a40c892fc0f1757e0f808c0 \
  62524032a5d44809fd0cf558bb8110fc1c127d70 -- \
  pkg/parser/schemas/main_workflow_schema.json .github/workflows schema-demos
```

Preserve adopted version-1 contract bytes and historical observations.
Semantic corrections require a new version and separately labelled evaluation;
do not refresh historical pins or reinterpret prior results in place.

## Integration boundary

**Definition only; unregistered and inactive.** Nothing changes in the workflow,
generated lock, conclusion job or grader runtime. gh-aw's restricted
[`graders.operational-value.script`](../../.github/skills/operational-value-designer/SKILL.md)
evaluates recorded run-scoped evidence, not this longitudinal source measure.

A future implementation and its tests belong at CAO's supported, separately
reviewed value-module boundary. This local contract is not a CAO
`schemaVersion: 3` module, registration or publication envelope. No collector,
dashboard, reporting platform, scheduler or deployment is introduced.

All historical `attributed_value` remains **null**: finding-to-implementation
lineage, authoritative native lifecycle identity and complete post-adoption
survival remain unestablished. An elapsed window does not prove survival,
exclusive credit or causality.
