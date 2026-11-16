---
title: Adoptions
description: Reference for adoption concepts in GitHub Agentic Workflows, including accepted adoptions, adoption states, and how adoption data relates to cost and observability.
sidebar:
  order: 297
---

Adoption describes whether a delivered [safe output](/gh-aw/reference/safe-outputs/) is accepted, rejected, pending, lifecycle activity, not applicable, or unknown. It combines the versioned safe-output rule with trusted delivery evidence and later repository observations.

Delivery, adoption, and operational value are separate. Delivery records that
the safe-output handler applied an effect. Adoption combines that delivery with
the safe-output rule and later repository evidence. Operational value measures the
workflow's intended repository outcome under a separate metric contract. See
the [human-readable lifecycle model](https://github.com/github/gh-aw/blob/main/specs/delivery-adoption-operational-value.md)
and the [normative specification](https://github.com/github/gh-aw/blob/main/specs/delivery-adoption-operational-value-spec.md).

For example, a pull request can be merged or closed, an issue can remain relevant or be dismissed, and a comment can lead to follow-up activity or be ignored. Adoption data is based on repository state, not on the workflow's self-assessment.

This page defines the common adoption states, summarizes what `accepted` means across safe output types, and lists the telemetry and cost rollups built from that data.

## Adoption Efficiency

Token and cost data are necessary, but they are not enough. A workflow can become cheaper because it became more efficient, or because it simply did less useful work. Adoptions make that difference visible by relating AI Credits (AIC) to accepted results.

Adoption efficiency is measured as AI Credits (AIC) divided by accepted adoptions. Lower is better: a lower value means the workflow consumed fewer AI Credits per accepted result.

## Adoption States

To support that measurement, every evaluated output is classified into an adoption state. These states provide the base vocabulary for the rest of the page.

| Adoption | Meaning |
| --- | --- |
| `accepted` | The result was kept, merged, completed, or otherwise accepted by the repository state. |
| `rejected` | The result was explicitly undone, closed, removed, or not accepted. |
| `pending` | The result exists, but has not reached a terminal state yet. |
| `lifecycle` | Closed or removed by the workflow itself as part of its normal operation (for example, a `close-older-issues` workflow) — not a rejection. |
| `not_applicable` | The safe output is infrastructure or diagnostics, not a repository-value effect. |
| `unknown` | The action cannot be verified: required execution evidence is missing, the evaluator is unsupported, or only target existence is known. |

The CLI uses action-specific classifications,
evidence strengths, and signals. An open issue or PR remains `pending`; approval,
bot comments, and unrelated target state do not turn a creation into acceptance.
Generic existence checks return weak `unknown`, never `accepted`. Unsupported
evaluators return `unknown` with no evidence. API failures remain `unknown` with
an `eval_error` diagnostic. Summary state counts include unknown, lifecycle, and
not-applicable adoptions so all evaluated items reconcile.

`zero_touch` is only true for an accepted PR when comments, submitted reviews,
and commits are available and show no visible non-bot follow-up after the action,
including commits by the PR author.
Missing actor or commit evidence cannot establish zero-touch acceptance.

## Accepted Adoptions

An accepted adoption is the simplest useful unit for measuring workflow effectiveness. Typical examples include merged pull requests, issues that remained relevant and were completed, and labels or comments that stuck and were acted on.

Accepted adoptions are intentionally simpler than a full value model. They do not try to rank one accepted result as inherently more important than another.

> [!NOTE]
> Different output types can have different practical importance. The adoptions model keeps the base measurement simple first. If needed, compare workflows within the same output class before introducing more complex weighting.

The table below is the quick lookup for what `accepted` currently means for each safe output type and whether that meaning comes from a dedicated rule, a fallback rule, a limited check, or no implemented rule yet.

Rows marked `delivery fallback` are accepted when trusted delivery is recorded;
later authoritative evidence may reject or classify them as lifecycle activity.
For exact rules, edge cases, and conformance details, see
[Safe Output Adoption Evaluation Specification](https://github.com/github/gh-aw/blob/main/specs/safe-output-adoption-evaluation.md).

Adoption evaluation is based on visible repository state and visible actor identity. A non-bot actor may still be AI-assisted; the lookup reflects what the system can observe, not hidden authoring provenance.

| Safe output type | `accepted` at a glance | Current rule source |
| --- | --- | --- |
| `create_pull_request` | merged | proposal decision |
| `create_issue` | closed as completed | proposal decision |
| `add_comment` | delivered; deletion or minimization may reject later | delivery fallback |
| `add_labels` | delivered; removal may reject later | delivery fallback |
| `add_reviewer` | delivered; PR lifecycle may refine later | delivery fallback |
| `update_issue` | delivered; reverted state may reject later | delivery fallback |
| `update_pull_request` | delivered; PR lifecycle may refine later | delivery fallback |
| `close_issue` | delivered; reopening rejects later | delivery fallback |
| `close_pull_request` | delivered; reopening or merge rejects later | delivery fallback |
| `close_discussion` | delivered | delivery fallback |
| `create_discussion` | answered or resolved | proposal decision |
| `update_discussion` | delivered | delivery fallback |
| `create_pull_request_review_comment` | delivered; thread lifecycle may refine later | delivery fallback |
| `submit_pull_request_review` | delivered; PR lifecycle may refine later | delivery fallback |
| `reply_to_pull_request_review_comment` | delivered | delivery fallback |
| `resolve_pull_request_review_thread` | delivered | delivery fallback |
| `push_to_pull_request_branch` | delivered; merge ancestry may refine later | delivery fallback |
| `mark_pull_request_as_ready_for_review` | delivered; PR lifecycle may refine later | delivery fallback |
| `assign_to_agent` | delivered; downstream failure may reject later | delivery fallback |
| `dispatch_workflow` | delivered; downstream failure may reject later | delivery fallback |
| `autofix_code_scanning_alert` | delivered | delivery fallback |
| `create_code_scanning_alert` | fixed by an accepted change | proposal decision |
| `link_sub_issue` | delivered | delivery fallback |
| `hide_comment` | delivered | delivery fallback |
| `assign_milestone` | delivered; removal may reject later | delivery fallback |
| `replace_label` | delivered; reversal may reject later | delivery fallback |
| `update_project` | delivered | delivery fallback |
| `update_release` | delivered | delivery fallback |
| `noop` | not applicable | infrastructure |
| `report_missing_tool` | not applicable | infrastructure |

## Evaluating Adoptions in Practice

Adoption evaluation does not require an `adoptions:` field in workflow
frontmatter. Configure the actions the workflow may take under
[`safe-outputs:`](/gh-aw/reference/safe-outputs/), then evaluate those actions
after a run:

```bash wrap
printf '%s\n' 1234567890 | gh aw logs --stdin --repo owner/repo --adoptions
```

The command downloads executed safe-output manifests and compact agent-output
fallback evidence, without downloading workflow logs by default. It queries the
current state of each affected GitHub object and includes per-run adoption summaries in
the logs output. Use `--json` for item-level classifications. Use a run ID from
the repository's GitHub Actions page. Specify
`--repo owner/repo` when running outside the target repository.

Evaluation is a snapshot, not a permanent verdict. Run it after the workflow
has produced safe outputs, allow an observation period appropriate to the
output type, and evaluate it again while results remain `pending`. A pull
request may remain pending until review and become accepted when merged; a
comment may need time to receive a reply or reaction.

`gh aw logs --adoptions` is the live adoption-evaluation entry point, including
`gh aw logs episode <run-id-or-url> --adoptions`. Workflow execution
records safe-output evidence; it does not decide whether those outputs were
accepted. `gh aw audit` reports execution evidence without checking live adoption
state. There is no built-in scheduled collector or automatic reevaluation.
Rerun logs with `--adoptions` later for unresolved items; each invocation checks current
GitHub state rather than reusing a cached classification.

Within one collection, identical successful REST and GraphQL reads are shared
across actions and runs. Labels already present in an issue or pull request
response do not require a separate label request. Paginated REST reads request
100 items per page and still fetch all pages. Each action retains its own
timestamp-dependent classification.
This response cache is in memory only: later invocations check GitHub again,
and failed or partial responses are not retained.

Use JSON output to feed a report or inspect the summary:

```bash wrap
gh aw logs weekly-research --adoptions --json |
  jq '.adoptions[] | {run_id, summary}'
```

Each new item-level result also includes `delivery` and `adoption` objects.
Existing `adoption_status`, `evidence_strength`, and `signal` fields remain the
authoritative live adoption classification. Adoption may remain accepted from
delivery when a publication has no mandatory later human-decision channel, while
a proposal such as `create_issue` remains pending until its lifecycle supplies
an authoritative decision.

Repeat this for comparable runs over a stable time window rather than drawing
conclusions from one run. Compare workflows that produce the same kind of safe
output, and examine item-level `object_url`, `adoption_status`, and
`evidence_strength` fields when a summary changes unexpectedly. See
[Measuring Impact](/gh-aw/guides/measuring-impact/) for combining these
results with run volume and AIC.

## Telemetry

Adoption data is derived from safe outputs and later checked against repository state. The system records the safe output produced by the workflow, looks up the affected repository object later, and classifies the observed state into an adoption.

This makes adoption evaluation external and observable. The workflow does not decide whether it succeeded; the repository state does.

Adoption information appears in OpenTelemetry spans and related artifacts. Workflow-level rollups such as accepted counts and acceptance rate are emitted on adoption summary or conclusion spans, and per-item spans can carry more detailed fields such as object type, URL, comments, review activity, and zero-touch acceptance.

For the span-level attribute inventory, see [OpenTelemetry attribute reference](/gh-aw/reference/open-telemetry-attributes/).

To write per-item adoption JSONL for an OTLP reporting pipeline, use:

```bash wrap
gh aw logs weekly-research --adoptions --adoptions-dir ./adoptions
```

For repository-wide or organization-wide reporting, collect these records on a
schedule and aggregate the adoption attributes by workflow, repository, output
type, and observation window.

## Cost and Rollups

Adoptions are most useful when read together with cost data. At the workflow level, the basic questions are how much AIC a workflow spent, how many accepted adoptions it produced, and how much AIC each accepted adoption cost.

The basic dashboard for adoptions is therefore intentionally small: total AIC, total accepted adoptions, AIC per accepted adoption, a trend over time, and a workflow ranking by AIC per accepted adoption.

For simple workflows, a single run is usually the right unit for adoption measurement.

`gh aw logs --adoptions` populates `.adoptions[].summary.cost_per_accepted_adoption` when positive AIC
usage and at least one accepted adoption are available. This is the full
observed run AIC, including detection usage, divided by accepted output items,
not a per-item allocation. Cost is computed from usage artifacts independently
of the agent-only token usage cache.
Unavailable cost and zero accepted adoptions leave this field omitted. If usage
cannot be read or parsed, the command warns and still returns evaluated adoptions
and writes any requested adoption JSONL.

For orchestrated workflows, multiple runs can belong to one logical execution. In that case, the more meaningful unit is the episode. Adoption and cost totals can be rolled up from runs into episodes using simple sums, and then from episodes into workflow totals and repository totals.

### Episode Adoption Summaries

`gh aw logs --adoptions` includes per-run observations in `.adoptions[]` and joins
them with selected episode membership and cost in `.episode_adoptions`.
Use `gh aw logs --episodes --adoptions` to include recorded episode membership
and hops in text output alongside these summaries. The combined flags perform
the episode-adoption join in memory, without a second command, JSONL export,
or additional GitHub requests for the join itself. Standard logs JSON membership
and adoption summaries are preserved without the explicit `--episodes` flag.
Manifests are collected alongside usage artifacts; full agent artifacts and
workflow logs are not required. Episode membership remains bounded to selected
runs. `gh aw logs episode <seed> --adoptions` also checks adoptions live when
membership comes from a cached snapshot; its retrieval API cap covers artifacts
and discovery, not the additional live adoption queries.

```bash wrap
gh aw logs --episodes --adoptions --json > logs.json
```

The join reads each selected run's downloaded usage artifacts directly,
including detection usage, rather than summing agent-only cached cost.
Accounting sources are selected separately for each execution phase before
summing independent stream totals. Agent JSON remains a fallback when agent
JSONL has no usable cost; an executed phase without accounting qualifies cost
coverage as partial.
Both compact and verbose logs JSON retain all canonical episodes, including standalone episodes.
A successfully evaluated empty or all-skipped manifest establishes adoption coverage
for that selected run. A missing manifest does not establish zero outputs and
keeps adoption coverage partial.

Membership can also come from [`gh aw logs episode`](/gh-aw/setup/cli/#logs-episode-run-id-or-url).
Its bounded retrieval report uses the same episode contract. The adoption join
uses the collected run directories, including isolated repository/attempt
directories. Collection limits remain membership gaps and suppress efficiency
claims; the join does not perform additional discovery.

Exported per-item JSONL records include `item_id` (a SHA-256 digest of the source action record,
with an occurrence number for indistinguishable records) and
`run_item_count` (the number of evaluated actions). The join matches `run_id`
against episode `run_ids`, selects the latest collected `checked_at` for each source-run
item, and reuses CLI adoption classifications. Repeated snapshots do not add
accepted adoptions, and a later rejection replaces a previous acceptance.
Conflicting evaluation fields at the latest observation time, including
evidence strength, objective value, and item count, produce an error rather
than an arbitrary verdict. A newer observation supersedes older conflicts.
Reordering or reserializing unchanged source records does not change action
identities; distinct actions against the same object remain distinct items.

The `.episode_adoptions` JSON field contains `episodes[]` and
`unmatched_adoption_run_ids`. Each episode contains
the selected `run_ids`, latest `items`, the existing adoption `summary`, and:

| Field | Meaning |
| --- | --- |
| `membership_scope` | Always `selected_runs`; the logs selection does not prove full-episode membership. |
| `membership_coverage` | Optional lineage/collection scope, status, reasons, and truncation evidence copied from the logs, separate from adoption/cost coverage. |
| `adoption_coverage` | `complete` or `partial` for selected runs only, based on available observations, item identity, and recorded item counts. |
| `adoption_run_ids`, `missing_adoption_run_ids` | Selected runs with and without recorded adoptions. No observations do not prove that a run produced no outputs. |
| `observed_aic` | Sum of available selected-run usage; `null` when unavailable. An explicit recorded zero remains `0`, not unknown. |
| `cost_coverage` | `available`, `partial`, or `unavailable` for selected runs. Usage warnings qualify cost as partial. |
| `cost_run_ids`, `missing_cost_run_ids` | Selected runs with and without usable AIC evidence. |
| `missing_run_ids` | Episode member IDs absent from the supplied run records. |
| `cost_per_accepted_adoption` | Observed AIC divided by accepted items, or `null` when there are no accepted items, incomplete membership/evidence/cost, or ambiguous legacy identities. A valid zero cost yields `0`. |
| `qualifications` | Scope and evidence limitations, including unresolved adoptions. |

Pending, unknown, and error adoptions remain unresolved rather than becoming
accepted. A ratio with unresolved items is an observed snapshot, not a final
efficiency verdict. Partial AIC is never divided by accepted items to imply a
complete cost result. Adoptions whose source runs are outside selected
membership are listed separately and excluded from all episode counts.

The join does not infer missing episode members or automatically schedule
observations. Adoption evaluation happens through `--adoptions` during collection.
Generate the schema, including `.episode_adoptions`, with `gh aw json-schema logs`.

The adoptions model is deliberately narrow. It does not try to estimate the full business value of a workflow, replace human judgment for nuanced quality questions, combine deterministic compute cost and inference cost into one synthetic score, or solve overlap and duplicate-work analysis in the first version.

Those questions may matter later, but they are separate from the base adoptions model described here.

## Learn More

- [Cost Management](/gh-aw/reference/cost-management/) explains how workflow cost is measured and reduced.
- [OpenTelemetry attribute reference](/gh-aw/reference/open-telemetry-attributes/) describes the span attributes and artifacts that carry workflow telemetry.
- [Safe Outputs](/gh-aw/reference/safe-outputs/) explains how workflows produce constrained actions.
- [Safe Output Adoption Evaluation Specification](https://github.com/github/gh-aw/blob/main/specs/safe-output-adoption-evaluation.md) defines the detailed evaluation logic for each safe output type.
