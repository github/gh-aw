---
title: Exact-effect authorization model
description: Experimental bounded TLA+ model of resolved-effect safe-output authorization.
---

# Exact-effect authorization model

> [!WARNING]
> **Experimental.** This model is informative and bounded. It is not a
> conformance artifact, does not replace the runtime tests in
> `actions/setup/js/safe_output_effect_plan.test.cjs`, and may change without
> deprecation.

`ExactEffectAuthorization.tla` models the resolved-effect protocol in the
[safe-output authorization contract](../../docs/src/content/docs/specs/safe-outputs-specification.md#safe-output-invocation-authorization).
TLC explores two logical records with identical inputs: record `a` uses a
trusted adapter route and record `c` is custom code. A hostile environment can
change observed preconditions, move the leased ref, expire the decision, tamper
with SDK middleware, lose responses and redeliver a record.

Each adapter plan has two ordered effects. Effect 1 is guarded by an atomic
target precondition (a merge head SHA, a `--force-with-lease` value or a
`createCommitOnBranch` `expectedHeadOid`). Effect 2 addresses the resource
created by effect 1 through a typed receipt, as follow-up labels, reviewers
and sub-issue links do.

| Property | Meaning |
| --- | --- |
| `SingleDecision` | At most one allow per logical invocation, across redelivery. |
| `NoDowngrade` | Adapter routes never request, or retry at, `invocation` binding. |
| `HonestBinding` | Custom code is never relabeled as `resolved_effect`. |
| `SentEqualsPlan` | Every transmitted request equals its approved plan entry; derived values come only from earlier receipts. |
| `FreshPermission` | Every send happens under a live, unexpired permission. |
| `SendOnce` | No effect is replayed, including SDK retries after a lost response. |
| `AtomicLease` | The leased effect never applies over a moved ref. |
| `RevalidatedSend` | Every send follows a revalidation that observed preconditions intact. |
| `HonestOutcome` | Partial or indeterminate execution is never reported as success. |
| `StagedSilent` | Staged previews make no authority request and no effect. |

Witnesses are invariants TLC must violate to show reachable behavior:
`NoCompletePlan` (a plan completes), `NoPartialFailure` (an earlier effect is
kept when a later one fails) and `NoResidualRace`. The last one shows the
documented limitation: observed preconditions are not atomic, so drift between
the final revalidation and the send can still reach the target.

Each `Broken*` configuration removes one protection: binding downgrade, forged
promotion, wire verification, freshness, the no-retry transport, authority
claims, receipt-bound derived slots, revalidation, honest outcomes or the
atomic lease. TLC must find a counterexample for the corresponding property.

## Running

```bash
bash .github/scripts/install_tla_runtime.sh "$TMPDIR/gh-aw-tla" --jar-only
TLA2TOOLS_JAR="$TMPDIR/gh-aw-tla/tla2tools.jar" specs/safe-output-authorization/check.sh
```

Set `TLC_CONFIG_FILTER=Hardened` to run one configuration. The hardened
configuration explores about 666,000 distinct states.
