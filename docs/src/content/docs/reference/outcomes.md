---
title: Outcomes
description: Reference for outcome concepts in GitHub Agentic Workflows, including accepted outcomes, outcome states, and how outcome data relates to cost and observability.
sidebar:
  order: 297
---

Outcomes describe what happened after a [safe output](/gh-aw/reference/safe-outputs/) landed in a repository. Safe outputs record what a workflow did. Outcomes record the repository state that can be observed afterward.

For example, a pull request can be merged or closed, an issue can remain relevant or be dismissed, and a comment can lead to follow-up activity or be ignored. Outcome data is based on repository state, not on the workflow's self-assessment.

This page defines the common outcome states, summarizes what `accepted` means across safe output types, and lists the telemetry and cost rollups built from that data.

## Outcome Efficiency

Token and cost data are necessary, but they are not enough. A workflow can become cheaper because it became more efficient, or because it simply did less useful work. Outcomes make that difference visible by relating AI Credits (AIC) to accepted results.

Outcome efficiency is measured as AI Credits (AIC) divided by accepted outcomes. Lower is better: a lower value means the workflow consumed fewer AI Credits per accepted result.

## Outcome States

To support that measurement, every evaluated output is classified into an outcome state. These states provide the base vocabulary for the rest of the page.

| Outcome | Meaning |
| --- | --- |
| `accepted` | The result was kept, merged, completed, or otherwise accepted by the repository state. |
| `rejected` | The result was explicitly undone, closed, removed, or not accepted. |
| `pending` | The result exists, but has not reached a terminal state yet. |
| `ignored` | The action reached a state without its expected effect, such as a dispatched run skipped without success or failure. |
| `lifecycle` | Closed or removed by the workflow itself as part of its normal operation (for example, a `close-older-issues` workflow) — not a rejection. |
| `lifecycle_close` | A `close_issue` or `close_pull_request` output where the close actor was a lifecycle bot (for example, a stale bot) and no visible non-bot actor has since reopened it. |
| `unknown` | The action cannot be verified: required execution evidence is missing, the evaluator is unsupported, or only target existence is known. |
| `error` | An API or evaluation failure prevented checking the action. |

The CLI and scheduled collector use the same action-specific classifications,
evidence strengths, and signals. An open issue or PR remains `pending`; approval,
bot comments, and unrelated target state do not turn a creation into acceptance.
Generic existence checks return weak `unknown`, never `accepted`. Unsupported
evaluators return `unknown` with no evidence. Summary counts include unknown,
error, and lifecycle outcomes so all evaluated items reconcile.

`zero_touch` is only true for an accepted PR when comments, submitted reviews,
and commits are available and show no visible non-bot follow-up after the action,
including commits by the PR author.
Missing actor or commit evidence cannot establish zero-touch acceptance.

## Accepted Outcomes

An accepted outcome is the simplest useful unit for measuring workflow effectiveness. Typical examples include merged pull requests, issues that remained relevant and were completed, and labels or comments that stuck and were acted on.

Accepted outcomes are intentionally simpler than a full value model. They do not try to rank one accepted result as inherently more important than another.

> [!NOTE]
> Different output types can have different practical importance. The outcomes model keeps the base measurement simple first. If needed, compare workflows within the same output class before introducing more complex weighting.

The table below is the quick lookup for what `accepted` currently means for each safe output type and whether that meaning comes from a dedicated rule, a fallback rule, a limited check, or no implemented rule yet.

Rows marked `fallback rule` use a generic existence check, not a type-specific rule. For exact rules, edge cases, and conformance details, see [Safe Output Outcome Evaluation Specification](https://github.com/github/gh-aw/blob/main/specs/safe-output-outcome-evaluation.md).

Outcome evaluation is based on visible repository state and visible actor identity. A non-bot actor may still be AI-assisted; the lookup reflects what the system can observe, not hidden authoring provenance.

| Safe output type | `accepted` at a glance | Current rule source |
| --- | --- | --- |
| `create_pull_request` | merged | dedicated rule |
| `create_issue` | closed as completed | dedicated rule |
| `add_comment` | reacted to or replied to | dedicated rule |
| `add_labels` | recorded label addition retained | dedicated rule |
| `add_reviewer` | recorded requested reviewer submitted a review | dedicated rule |
| `update_issue` | intended edit still matches current issue state | dedicated rule |
| `update_pull_request` | intended edit still matches current PR state | dedicated rule |
| `close_issue` | still closed by a visible non-bot actor | dedicated rule |
| `close_pull_request` | still closed unmerged by a visible non-bot actor | dedicated rule |
| `close_discussion` | none yet | no implemented rule yet |
| `create_discussion` | none yet | no implemented rule yet |
| `update_discussion` | none yet | no implemented rule yet |
| `create_pull_request_review_comment` | none yet | no implemented rule yet |
| `submit_pull_request_review` | review affected PR lifecycle | dedicated rule |
| `reply_to_pull_request_review_comment` | none; target existence is unknown | fallback rule |
| `resolve_pull_request_review_thread` | none yet | no implemented rule yet |
| `push_to_pull_request_branch` | recorded pushed commits verified in merged history | dedicated rule |
| `mark_pull_request_as_ready_for_review` | reviewed | dedicated rule |
| `assign_to_agent` | attributable post-assignment agent PR merged | dedicated rule |
| `dispatch_workflow` | recorded dispatched run completed successfully | dedicated rule |
| `autofix_code_scanning_alert` | none; target existence is unknown | fallback rule |
| `create_code_scanning_alert` | none; target existence is unknown | fallback rule |
| `link_sub_issue` | none; target existence is unknown | fallback rule |
| `hide_comment` | none yet | no implemented rule yet |
| `assign_milestone` | recorded milestone identity still assigned | dedicated rule |
| `replace_label` | recorded label replacement delta retained | dedicated rule |
| `update_project` | none; target existence is unknown | fallback rule |
| `update_release` | none; target existence is unknown | fallback rule |
| `noop` | skipped | skipped |
| `missing_tool` | skipped | skipped |

## Evaluating Outcomes in Practice

Outcome evaluation does not require an `outcomes:` field in workflow
frontmatter. Configure the actions the workflow may take under
[`safe-outputs:`](/gh-aw/reference/safe-outputs/), then evaluate those actions
after a run:

```bash wrap
gh aw outcomes 1234567890
```

The command downloads the run's safe-output artifacts, queries the current
state of each affected GitHub object, and prints the item-level classifications
and summary. Use a run ID from the repository's GitHub Actions page. Specify
`--repo owner/repo` when running outside the target repository.

Evaluation is a snapshot, not a permanent verdict. Run it after the workflow
has produced safe outputs, allow an observation period appropriate to the
output type, and evaluate it again while results remain `pending`. A pull
request may remain pending until review and become accepted when merged; a
comment may need time to receive a reply or reaction.

Use JSON output to feed a report or inspect the summary:

```bash wrap
gh aw outcomes 1234567890 --json |
  jq '.summary | {total, accepted, rejected, ignored, pending, acceptance_rate}'
```

Repeat this for comparable runs over a stable time window rather than drawing
conclusions from one run. Compare workflows that produce the same kind of safe
output, and examine item-level `object_url`, `outcome_status`, and
`evidence_strength` fields when a summary changes unexpectedly. See
[Measuring Impact](/gh-aw/practices/measuring-impact/) for combining these
results with run volume and AIC.

## Telemetry

Outcome data is derived from safe outputs and later checked against repository state. The system records the safe output produced by the workflow, looks up the affected repository object later, and classifies the observed state into an outcome.

This makes outcome evaluation external and observable. The workflow does not decide whether it succeeded; the repository state does.

Outcome information appears in OpenTelemetry spans and related artifacts. Workflow-level rollups such as accepted counts and acceptance rate are emitted on outcome summary or conclusion spans, and per-item spans can carry more detailed fields such as object type, URL, comments, review activity, and zero-touch acceptance.

For the span-level attribute inventory, see [OpenTelemetry attribute reference](/gh-aw/reference/open-telemetry-attributes/).

To write per-item outcome JSONL for an OTLP reporting pipeline, use:

```bash wrap
gh aw outcomes 1234567890 --outcomes-dir ./outcomes
```

For repository-wide or organization-wide reporting, collect these records on a
schedule and aggregate the outcome attributes by workflow, repository, output
type, and observation window.

## Cost and Rollups

Outcomes are most useful when read together with cost data. At the workflow level, the basic questions are how much AIC a workflow spent, how many accepted outcomes it produced, and how much AIC each accepted outcome cost.

The basic dashboard for outcomes is therefore intentionally small: total AIC, total accepted outcomes, AIC per accepted outcome, a trend over time, and a workflow ranking by AIC per accepted outcome.

For simple workflows, a single run is usually the right unit for outcome measurement.

For orchestrated workflows, multiple runs can belong to one logical execution. In that case, the more meaningful unit is the episode. Outcome and cost totals can be rolled up from runs into episodes using simple sums, and then from episodes into workflow totals and repository totals.

The outcomes model is deliberately narrow. It does not try to estimate the full business value of a workflow, replace human judgment for nuanced quality questions, combine deterministic compute cost and inference cost into one synthetic score, or solve overlap and duplicate-work analysis in the first version.

Those questions may matter later, but they are separate from the base outcomes model described here.

## Learn More

- [Cost Management](/gh-aw/reference/cost-management/) explains how workflow cost is measured and reduced.
- [OpenTelemetry attribute reference](/gh-aw/reference/open-telemetry-attributes/) describes the span attributes and artifacts that carry workflow telemetry.
- [Safe Outputs](/gh-aw/reference/safe-outputs/) explains how workflows produce constrained actions.
- [Safe Output Outcome Evaluation Specification](https://github.com/github/gh-aw/blob/main/specs/safe-output-outcome-evaluation.md) defines the detailed evaluation logic for each safe output type.
