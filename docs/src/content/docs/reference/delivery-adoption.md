---
title: Delivery and adoption
description: Execution evidence, adoption decisions, reversal observations, and their relationship to attributed value.
sidebar:
  order: 297
---

Delivery records a successfully applied safe-output effect. Adoption records the
decision defined by that output type's versioned rule. These facts apply to every
workflow, independently of campaigns or operational-value graders.

## CLI reports

```bash
gh aw logs weekly-research --delivery --json
gh aw logs weekly-research --adoption --json
gh aw audit https://github.com/owner/repo/actions/runs/123 --adoption --json
gh aw logs episode https://github.com/owner/repo/actions/runs/123 --adoption --json
```

`--delivery` reads recorded execution evidence without querying later resource
disposition. `--adoption` includes delivery and refreshes supported GitHub
observations. Artifact downloads and run discovery can still make API requests
with either flag. No workflow configuration change or workflow dispatch is needed.

Logs JSON exposes `.delivery[]`, `.adoption[]`, and, for selected episode members,
`.episode_adoption`. A run whose execution evidence is malformed or unreadable
stays in `.runs[]` with `unavailable` coverage and is listed in
`.adoption_errors[]` with its `run_id`, `reason` (`lifecycle_identity_missing`
or `lifecycle_evidence_invalid`), and `error`; the remaining runs are still
evaluated. Single-run audit JSON exposes `.delivery`, `.adoption[]`, and
`.adoption_summary`. Text reports show the same projections. The `delivery`
artifact set selects compact execution evidence rather than full workflow logs.
The MCP `logs` and `audit` tools expose matching `delivery` and `adoption`
boolean arguments; `logs` also supports them with `episode_run`.

## Delivery evidence

Successful handler results establish delivery even when another job fails.
Requests, staged previews, deferred placeholders, skipped actions, and failed
handlers do not establish delivery.

Each delivery binds the source host, repository, workflow, run, attempt,
invocation, evidence identifier, type, and available exact target. Delivery and
observation timestamps are separate. Missing or malformed execution evidence is
unavailable or partial coverage, not a verified count of zero. Custom safe-job
results without a recorded delivery timestamp cannot establish dated delivery;
collection time is not a substitute.

A recorded skipped safe-output job can establish zero deliveries. Missing optional
artifacts do not discard execution evidence already present in the download cache.

## Adoption rules

The shared registry in `pkg/lifecycle` versions the rules for built-in output
types. Unknown or custom types remain unsupported until a rule is registered.

| State | Meaning |
| --- | --- |
| `accepted` | A verified decision or the type's explicit delivery-time fallback established adoption. |
| `rejected` | A supported provider observation established rejection or reversal. |
| `pending` | The proposal is awaiting a qualifying decision. |
| `lifecycle` | An observed automated decision is not a verified human decision. |
| `not_applicable` | Infrastructure bookkeeping does not represent repository value. |
| `unknown` | Evidence or a supported rule is missing. |

Created issues and pull requests require the exact delivered resource and an
authoritative decision timestamp. The refresh checks the visible decision actor
and current repository authorization, excluding known bots and the publisher.
Current authorization does not prove historical authorization or reveal hidden
machine-user ownership. Unverifiable decisions remain unknown.

Other registered publications, mutations, and delegations can explicitly adopt
at delivery when their rule permits it. Such records carry
`acceptance_kind: "fallback_delivery"` and `human_verified: false`; they are not
human approvals. Supported effect adapters can subsequently observe retention or
reversal. Other proposal types retain pending/unobserved coverage until their
provider adapter is implemented.

Summaries separate `accepted_human` from `accepted_delivery_fallback`.
Provider errors remain evidence diagnostics, not fabricated rejection or deletion.
A 404 can mean lost visibility and does not prove that an object was deleted.

## Retention and refresh

Commands retain compact identities and observations in `lifecycle.jsonl` beside
the downloaded run directories, or inside an explicitly selected audit directory.
The file uses the existing logs JSONL record
format (`kind: "output_lifecycle"`), sensitive file permissions, atomic replacement,
and an exclusive collector lock. Run-folder pruning does not remove it.
Retention is 365 days from delivery, capped at 20,000 identities and 16 MiB;
capacity errors are explicit. Use a separate output directory when capacity is
exceeded. A leftover `.lifecycle-cache-lock` after an interrupted command must be
removed only after confirming that no collector is using that directory.

Repeated collection merges by delivery identity. Replayed delivery cannot
overwrite a later provider observation. Retained issue/PR identities allow refresh
after source artifacts expire; missing mutation payloads can still prevent an
effect-specific survival check. `--cached-jsonl` also exports and imports typed
lifecycle records.

`--adoption-dir DIR` or `GH_AW_ADOPTION_DIR` additionally writes
`adoption-RUN_ID.jsonl` item projections. Ordinary logs and audit refresh provider
state; there is no built-in scheduling service. Canonical episode retrieval can
reuse its recorded snapshot; use `--refresh` (MCP `refresh: true`) to collect again.
Identical successful API reads are shared within a collection, not treated as a
live snapshot by later collections.
Episode refresh respects the allowed repositories, wall-clock deadline, and
remaining API budget, including paginated reads.

### Refresh retained deliveries

```bash
gh aw logs --adoption --retained --repo owner/repo \
  --output ./lifecycle-state --count 200 --timeout 2 --json
```

This mode reads `lifecycle-state/lifecycle.jsonl` directly. It does not discover
runs, fetch run metadata, or download source artifacts. Restore that file from
durable storage before invoking it on a new runner. `--repo` scopes the source
host/repository; recorded cross-repository targets keep their exact identity.
Workflow arguments and unrelated run-selection/output modes are rejected.

`--count` bounds delivery checks in this mode. Supported records are due after
24 hours, or immediately if no provider check has been attempted. Oldest checks
go first; deferred records keep their clocks and remain eligible on the next
invocation. The ordinary timeout and API-quota protections also apply. JSON
returns `.retained_adoption` with all scoped retained records, `refreshed`,
`deferred`, and coverage. A failed provider check preserves the previous decision
with unavailable coverage; it does not fabricate rejection.

New retained records include execution selectors needed by effect adapters,
such as review IDs and label deltas. Conflicting replayed selectors fail
explicitly. Records without required execution selectors can still have
unsupported effect-survival checks.

### Repository collection

This repository's Metrics Collector runs deterministic collection with its
source-built CLI before the agent starts. A separate repo-memory branch,
`memory/output-lifecycle`, retains the per-item ledger and discovery checkpoint.
The existing shared metrics branch
receives `metrics/lifecycle.json`, a compact fleet/per-workflow snapshot keyed by
source workflow path. Performance reports read that snapshot without maintaining
another adoption classifier.

Initial discovery covers the previous day. Subsequent executions resume saved
windows and pagination, then catch up from the completed window with a one-hour
overlap instead of repeatedly selecting only yesterday's runs. Unavailable
execution evidence stays in a rotating retry backlog. Discovery and new retained
bytes are bounded per execution: five batches of at most 200 runs and 768 KiB of
new delivery records. Each discovery batch has a four-minute network budget.
Incomplete windows remain checkpointed rather than being reported as complete.
Provider refresh checks at most 1,000 due deliveries per execution, subject to
its two-minute network and API-quota limits. Due does not guarantee a daily check;
monitor deferred counts as the retained population grows.
The first deployment does not automatically backfill deliveries older than the
initial day; seed the ledger with `logs --delivery` when historical coverage is
required.
Discovery follows run creation windows; late reruns outside those windows also
require a delivery backfill.

Honor the snapshot's coverage, deferred work, unavailable runs, and discovery
watermark. It describes the retained population, not all decisions occurring
that day, and must not be added to recent-run cohort counts. Repo-memory's
persistence job remains separate from the read-only agent job. Cache-memory is
not the authoritative ledger, and webhooks are not required.

## Survival, attributed value, and cost

`survival.state: "observed"` means a supported snapshot check succeeded.
It does **not** establish continuous survival across a measurement window.
Reversal event time is omitted when only the observation time is known.

Attributed value additionally requires a defined measurement, attribution evidence,
and survival across its complete post-adoption window. Delivery counts, acceptance
counts, label-derived weights, and an agent's claims are not attributed value.
Run-scoped `graders.operational-value.script` results remain separate recorded
measurements; lifecycle refresh never rewrites those results or the unified session.
CAO can schedule refresh and consume gh-aw records for longitudinal measurement;
it is not required to inspect delivery or adoption.

For the slides' schema-coverage replay, use
[`gh aw value`](/gh-aw/reference/value/). It joins native adoption
records with Git measurements and a complete-window file-survival check,
without changing the original records or grader scores.

`cost_per_adopted_output` divides observed run or selected-episode AIC by adopted
items. It is an efficiency ratio, not a value score. Missing cost, incomplete
episode evidence, or zero adopted items suppress the episode ratio.

See [Measuring Impact](/gh-aw/guides/measuring-impact/), the
[CLI reference](/gh-aw/setup/cli/), and the
[lifecycle specification](https://github.com/github/gh-aw/blob/main/specs/safe-output-lifecycle.md).
