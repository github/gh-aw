---
title: Ledger compaction
description: How Agentic Maintenance compacts standalone ledgers with an untrusted plan job and a trusted apply job
---

Standalone `tools.ledger` ledgers add one immutable shard on each run, so a ledger
used often builds up many small segments. Compaction merges them into fewer,
larger segments without losing history. Compaction belongs to the generated
Agentic Maintenance workflow (`agentics-maintenance.yml`). Agent jobs never
compact, rewrite, or delete ledger history.

## Configuration

Compaction is on by default and runs daily. Set it for each ledger:

```yaml
tools:
  ledger:
    findings:
      compaction:
        schedule: weekly     # daily (default), weekly, or manual
        min-segments: 32     # compact only after this many small segments exist (2-256, default 32)
        max-segments: 128    # maximum segments merged per plan (2-256, default 128)
    metrics:
      compaction: false      # never compact this ledger
```

Maintenance may run more often than a ledger's schedule. Each run checks whether
the ledger is due and does nothing if it is not. A `manual` ledger is compacted
only when compaction is requested.

An optional `compaction.script` chooses which segments to merge. The script
receives frozen `segments` (`id`, `bytes`, `records`) and `options`, and returns
`{ sources: [segmentId, ...] }`:

```yaml
tools:
  ledger:
    findings:
      compaction:
        script: |
          return { sources: segments.slice(0, options.maxSegments).map(segment => segment.id) }
```

## Trust boundary

Each compaction-enabled ledger gets two maintenance jobs that share a per-ledger
concurrency group:

| Job | Permissions | Responsibility |
| --- | --- | --- |
| `ledger_compaction_plan_<name>` | `contents: read` | Reads the `ledgers/<name>` branch, runs the optional selection script in an isolated Node.js process with no environment or credentials, and uploads a plan artifact |
| `ledger_compaction_apply_<name>` | `contents: write` | Downloads the plan, validates it, checks it against the latest ledger branch, and applies it in one commit |

The plan is only a proposal. The apply job runs no user JavaScript and treats
the plan as hostile input. Before it writes anything, it:

- rejects unknown keys, oversized plans, wrong ledger or branch names, and a
  `plan_id` that does not match the plan contents.
- reloads the latest ledger and checks every source segment against its SHA-256
  hash, size, and sorted record hashes.
- rebuilds the replacement segment from those verified records and confirms that
  no record is lost.
- publishes the change in one commit that applies only if the branch head is the
  one it validated against. Segments added after planning are kept.

## Plan format

Plans use version `gh-aw/ledger-compaction-plan/v1` and contain exactly these keys:

| Key | Content |
| --- | --- |
| `version`, `ledger`, `branch` | Plan version and target ledger |
| `trigger` | `scheduled` or `requested` |
| `created_at`, `base_commit` | When and from which branch commit the plan was made |
| `sources` | Sorted segments to retire: `segment`, `sha256`, `bytes`, `records` (sorted record SHAs) |
| `replacement` | The new segment, described with the same fields |
| `plan_id` | SHA-256 of the canonical ledger, branch, version, and source and replacement identities |

A given state transition always gets the same `plan_id`. After a successful
apply, `ledger/compaction/state.json` on the ledger branch records the plan ID
and the time it was applied.

## Results and retries

The apply job reports one of these results in its step summary, together with
the ledger name, trigger, plan ID, segment and record counts, and bytes before
and after. Ledger payloads are never logged.

| Result | Meaning |
| --- | --- |
| `applied` | The plan was committed |
| `already_applied` | The plan's effect is already present; nothing changed |
| `stale` | Some sources were already retired; the next run plans again |
| `conflict` | A referenced segment changed; the next run plans again |
| `rejected` | The plan failed validation; the job fails and nothing changed |

If a commit attempt fails, for example because the branch moved or GitHub is
unavailable, the apply job fetches the branch again and revalidates before
retrying. It never overwrites the branch. A planning, artifact, or validation
failure leaves the ledger unchanged.

## Requesting compaction

When at least one ledger has compaction enabled, agents get the
`ledger_request_compaction` safe output with an optional `ledger` and `reason`.
The `ledger` value is required only when the workflow has more than one
compaction-enabled ledger.
This safe output never compacts anything itself. It dispatches Agentic
Maintenance with `operation: compact_ledger` and the ledger name, and the
maintenance jobs above handle the request the same way as a scheduled run. To
allow the dispatch, the safe-outputs job gets `actions: write`.

You can also start compaction manually: run Agentic Maintenance with the
`compact_ledger` operation and an optional `ledger` input. Leave `ledger` empty
to consider every ledger.
