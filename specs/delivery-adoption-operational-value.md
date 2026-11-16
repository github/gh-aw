---
title: Delivery, Adoption, and Operational Value
description: Human-readable model for safe-output delivery, later adoption, and operational value in gh-aw
---

# Delivery, Adoption, and Operational Value

This document explains three related but different facts:

1. **Delivery** asks whether a safe output applied its declared effect.
2. **Adoption** asks whether the delivered effect counts as accepted, rejected,
   pending, lifecycle activity, or not applicable after observing repository state.
3. **Operational value** asks whether the workflow's intended repository outcome
   was attained under a frozen metric contract.

Adoption is the new name and contract for the former safe-output outcome layer.
The distinctions prevent successful execution from being mislabeled as value.

## The lifecycle

```text
safe-output request
  -> handler execution
  -> delivery evidence
  -> later repository observation
  -> adoption classification
  -> operational-value evaluation
```

A request is not delivery. Delivery may establish initial adoption under the
registry, but adoption is not automatically operational value.

### Delivery

Delivery is established by trusted execution evidence such as
`safe_output.result` or `safe_output.delivery`. A created issue with a confirmed
canonical URL is delivered. A staged preview, deferred operation, failed
handler, or skipped action is not delivered.

The normalized delivery states are:

| State | Meaning |
| --- | --- |
| `delivered` | The handler confirmed the declared effect. |
| `failed` | The handler failed or was cancelled. |
| `skipped` | The operation intentionally did not run. |
| `staged` | The operation was previewed but not applied. |
| `deferred` | Another job or process still owns completion. |
| `unknown` | The operation may have applied, or the status is unsupported. |

An `operation_outcome: indeterminate` result is `unknown`, never delivered.

### Adoption

Adoption combines the safe-output type's registry rule with delivery and, when
available, a later repository observation. `gh aw logs --adoptions` publishes
the `adoption_status`, `evidence_strength`, and `signal` triple.

Adoption evaluation remains action-specific. A pull request is accepted when it
is merged. A created issue is accepted when it is closed as completed. Missing
evidence remains unknown, and a generic existence check does not establish
acceptance.

The registry has five effect classes:

| Class | Initial adoption after delivery |
| --- | --- |
| `proposal` | Pending until an authoritative decision, unless the rule says otherwise. |
| `publication` | Accepted at delivery when no stronger human-decision channel is required. |
| `mutation` | Accepted at delivery, subject to later reversal evidence. |
| `delegation` | Accepted at delivery, subject to downstream failure or reversal evidence. |
| `infra` | Not applicable to repository value. |

The adoption states are `accepted`, `rejected`, `pending`, `lifecycle`,
`not_applicable`, and `unknown`.

Later repository evidence has explicit precedence:

- `accepted` becomes adopted.
- `rejected` or `ignored` becomes rejected adoption.
- `lifecycle` and `lifecycle_bot_close` become lifecycle adoption.
- `pending` refines a proposal that was already pending.
- `unknown` or evaluator failure does not erase established delivery adoption.

This means a delivered comment may remain accepted under the publication
fallback even when no later engagement is observed. A created issue stays
pending until its repository lifecycle supplies an authoritative decision.

### Operational value

Operational value measures the workflow's intended repository outcome, not the
number of requests, deliveries, or adopted outputs.

Adoption can be evidence for a value function. For example, a merged pull
request may locate the revision at which a repository-native condition is
measured. The merge does not by itself prove that the condition improved.

Two scopes remain distinct:

- A **run-scoped operational-value grader** evaluates the strongest effect
  attributable to one gh-aw run from its recorded unified session.
- A **repository-scoped value measure** evaluates a frozen native metric over
  eligible opportunities and observation windows, regardless of actor.

Neither scope may silently convert missing evidence to zero or normalize unlike
native units into one score.

Run-scoped operational value uses `graders.<distinct-id>.script`. The synchronous
JavaScript receives immutable `session`, `config`, and `helpers` snapshots in
the separate restricted graders job. It cannot read files, spawn processes, use
credentials, access the network, or fetch later repository state. Shell-backed
graders and `run` are unsupported.

## Examples

| Safe output | Delivery | Repository observation | Adoption | Value |
| --- | --- | --- | --- | --- |
| `create_issue` | Issue created | Issue remains open | `pending` | Not yet established |
| `create_issue` | Issue created | Human closes as completed | `accepted` | May provide accepted lineage for a separate repository measure |
| `add_comment` | Comment created | No reply yet | `accepted` by delivery fallback | Comment existence alone is not repository value |
| `dispatch_workflow` | Dispatch confirmed | Downstream run has no effect | `rejected` | No attained downstream outcome |
| `noop` | No repository effect | Not evaluated | `not_applicable` | May still be correct restraint for a separately defined grader |

## gh-aw data surfaces

Unified-session `safe_output.result` and `safe_output.delivery` events carry:

- `deliveryState`, `deliverySignal`, and `deliverySource`;
- `adoptionState`, `adoptionSignal`, and `adoptionSource`;
- `adoptionRuleVersion` and `effectClass`;
- `fallbackToDelivery`, `attributionEligible`, and `survivalRequired`;
- `adoptionAt` and `acceptanceKind` when delivery itself establishes adoption.

`gh aw logs --adoptions --json` publishes adoption fields and a nested
`delivery` object. Optional JSONL export preserves the same contract.

The machine registry is
`pkg/safeoutput/adoption_rules.json`. The Actions runtime mirror is
`actions/setup/js/safe_output_adoption_rules.json`. Both use registry version
`gh-aw-v1.2.3-adoption-v1`.

## Ownership boundary with Central Agentic Ops

gh-aw owns:

- safe-output request and application evidence;
- the safe-output adoption registry;
- live adoption evaluation;
- run-scoped delivery, adoption, and grader records;
- episode lineage recorded by `aw_context`.

Central Agentic Ops may retain, merge, query, and present those facts. It may
collect later provider observations and repository-scoped value measurements.
It must not redefine the gh-aw safe-output registry or reinterpret gh-aw's
normalized adoption triple.

Repository-scoped Value, collection scheduling, retention, dashboard queries,
and historical evidence packets remain Central Agentic Ops responsibilities.

## Missing evidence

These values are not interchangeable:

- zero means a complete observation measured zero;
- pending means a decision has not reached a terminal state;
- unknown means the required evidence is absent or unsupported;
- failed delivery means no effect was confirmed;
- not applicable means the safe-output type is not a repository-value effect.

An unavailable later observation does not roll back a previously established
delivery fact. A stronger authoritative observation may refine adoption, but it
must preserve source and observation time.

## Security and privacy

Only allowlisted structured fields enter the unified session. Free-form request
content, handler errors, signed URL query parameters, and credentials are not
adoption evidence. Canonical resource identity comes from the handler response,
not from the agent's requested target.

The adoption registry performs deterministic classification only. It grants no
permission, performs no network request, and cannot turn a staged, deferred,
failed, or indeterminate operation into delivery.

Operational-value JavaScript executes after conclusion without inherited
credentials or host capabilities. The compiler rejects shell-backed grader
fields, and the runtime rejects module, filesystem, process, network, clock,
random, and timer access.

## Implementation and tests

The implementation is split by responsibility:

- `pkg/safeoutput/adoption.go` evaluates delivery and adoption for CLI reports.
- `actions/setup/js/safe_output_adoption.cjs` projects runtime evidence.
- `pkg/cli/adoption_lifecycle.go` combines delivery with live adoption evidence.
- `actions/setup/js/custom_graders.cjs` runs operational-value JavaScript in
  restricted workers.
- `specs/delivery-adoption-operational-value-spec.md` is the normative contract.

Conformance coverage includes registry inventory parity, delivery fallback,
adoption precedence, unified-session projection, JSONL round trips, generated
schema validation, shell-surface rejection, and restricted operational-value
JavaScript execution.
