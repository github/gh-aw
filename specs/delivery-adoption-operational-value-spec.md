---
title: Safe Output Delivery, Adoption, and Operational Value Specification
description: W3C-style normative contract for delivery, adoption, and operational value in GitHub Agentic Workflows
---

# Safe Output Delivery, Adoption, and Operational Value Specification

**Version**: 1.0.0  
**Status**: Working Draft  
**Latest Version**: https://github.com/github/gh-aw/blob/main/specs/delivery-adoption-operational-value-spec.md  
**Editors**: GitHub Next (GitHub, Inc.)  
**Publication Date**: 2026-11-16

---

## Abstract

This specification defines the relationship between safe-output delivery,
adoption, and operational value in GitHub Agentic Workflows
(gh-aw). It defines a versioned registry for all built-in and compiler-managed
safe-output types, normalized delivery and adoption states, processing
algorithms, unified-session and CLI projections, and the authority boundary
between gh-aw producers and downstream lifecycle or Value systems.

The central invariant is:

> Delivery proves an applied effect. Adoption classifies its acceptance.
> Operational value measures the intended repository outcome.

No stage may be inferred solely from the existence of an earlier stage.

## Status of This Document

This is a Working Draft implemented by gh-aw's safe-output runtime and live
adoption evaluator. The registry was migrated from the Central Agentic Ops
adoption model so gh-aw owns the semantics of the evidence it produces.
Downstream systems remain responsible for retention, collection scheduling,
historical reconstruction, and presentation.

Feedback and errata should be filed in the `github/gh-aw` repository.

## Table of Contents

1. [Conformance](#1-conformance)
2. [Terminology](#2-terminology)
3. [Authority Model](#3-authority-model)
4. [Registry](#4-registry)
5. [Delivery Processing](#5-delivery-processing)
6. [Adoption Processing](#6-adoption-processing)
7. [Adoption Observation Processing](#7-adoption-observation-processing)
8. [Operational Value](#8-operational-value)
9. [Transport Contracts](#9-transport-contracts)
10. [Security and Privacy](#10-security-and-privacy)
11. [Versioning and Compatibility](#11-versioning-and-compatibility)
12. [Conformance Testing](#12-conformance-testing)
13. [References](#13-references)
14. [Change Log](#14-change-log)

---

## 1. Conformance

### 1.1 Conformance classes

This specification defines three conformance classes:

1. A **producer** records safe-output application evidence and projects
   normalized delivery and initial adoption.
2. An **adoption evaluator** observes later repository state and refines
   adoption without reinterpreting delivery.
3. A **Value consumer** uses delivery or adoption only as evidence
   under an independently frozen operational-value contract.

A conforming implementation MUST satisfy every applicable requirement for its
class.

### 1.2 Requirements notation

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** are interpreted as described by RFC 2119 and RFC 8174 when they
appear in all capitals.

Normative requirements have stable identifiers of the form **DAV-NNN**.

### 1.3 Determinism

**DAV-001** A producer or evaluator MUST return the same delivery and adoption
classification for identical registry, event, and repository-observation inputs.

**DAV-002** A classifier MUST NOT perform network, filesystem, model, clock, or
random operations as part of classification.

## 2. Terminology

**Safe-output request**: An agent-authored request for a constrained effect.

**Delivery**: Trusted evidence that the safe-output handler applied its declared
effect.

**Repository observation**: Later evidence about the affected repository object
or workflow execution.

**Adoption**: The normalized decision state produced from the safe-output rule,
delivery evidence, and any authoritative later repository observation.

**Operational value**: The degree to which a workflow's intended repository
outcome is attained under a frozen measurement contract.

**Fallback delivery adoption**: A rule under which successful delivery is the
accepted adoption event because no stronger mandatory human-decision channel is
available.

**Proposal**: An effect that requests or creates an object requiring a later
decision before adoption.

**Publication**: A delivered information object whose successful publication
may be the strongest available adoption fact.

**Mutation**: A delivered change to existing repository or service state.

**Delegation**: A delivered request for another workflow, agent, or execution
system to act.

**Infrastructure effect**: An internal or diagnostic effect that is not
eligible for repository-value attribution.

## 3. Authority Model

**DAV-010** gh-aw MUST be authoritative for safe-output type identity,
application status, registry classification, and the normalized live adoption
triple.

**DAV-011** A downstream system MAY retain, merge, query, or present gh-aw
evidence, but MUST NOT silently assign different registry semantics to the same
registry version.

**DAV-012** A safe-output request MUST NOT establish delivery.

**DAV-013** Delivery MUST NOT by itself establish operational value.

**DAV-014** Adoption MUST NOT by itself establish operational value.

**DAV-015** Repository-scoped collection cadence, historical reconstruction,
storage retention, and dashboard presentation are outside the producer
conformance class.

## 4. Registry

### 4.1 Registry document

The canonical registry is `pkg/safeoutput/adoption_rules.json`. Its Actions
runtime mirror is `actions/setup/js/safe_output_adoption_rules.json`.

The registry version is:

```text
gh-aw-v1.2.3-adoption-v1
```

Each rule contains:

| Field | Type | Meaning |
| --- | --- | --- |
| `type` | string | Canonical snake-case safe-output type. |
| `effectClass` | enum | `proposal`, `publication`, `mutation`, `delegation`, or `infra`. |
| `fallbackToDelivery` | boolean | Whether delivery establishes accepted adoption. |
| `attributionEligible` | boolean | Whether the effect may participate in operational-value attribution. |

**DAV-020** The registry MUST contain one unique rule for every built-in
safe-output tool and compiler-managed dynamic output.

**DAV-021** Registry entries MUST be sorted by `type`.

**DAV-022** An `infra` rule MUST set `attributionEligible` to `false`.

**DAV-023** A non-`infra` built-in rule MUST set `attributionEligible` to
`true`.

**DAV-024** A conforming runtime MUST expose the exact registry version used to
classify an event.

### 4.2 Effect classes

The registry covers 51 built-in tools plus `call_workflow`,
`comment_memory`, `dispatch_repository`, and `dispatch_workflow`.

Infrastructure types include diagnostic reports, memory and ledger operations,
`noop`, and coverage publication. Proposal types include created issues, pull
requests, discussions, security advisories, and code-scanning alerts. The
machine registry is authoritative for the complete classification.

## 5. Delivery Processing

### 5.1 Delivery states

The delivery state vocabulary is:

```text
delivered | failed | skipped | staged | deferred | unknown
```

### 5.2 Normalization algorithm

Given event type `E`, application status `S`, and operation outcome `O`, a
producer MUST apply these rules in order:

1. If `O` is `indeterminate`, delivery is `unknown`.
2. If `E` is `safe_output.result` and `S` is absent, delivery is `delivered`.
3. If `S` is `success` or `warning`, delivery is `delivered`.
4. If `S` is `failed`, `failure`, or `cancelled`, delivery is `failed`.
5. If `S` is `skipped`, delivery is `skipped`.
6. If `S` is `staged`, delivery is `staged`.
7. If `S` is `deferred` or `delegated`, delivery is `deferred`.
8. Otherwise, delivery is `unknown`.

**DAV-030** A staged, skipped, deferred, failed, cancelled, or indeterminate
operation MUST NOT be classified as delivered.

**DAV-031** A canonical resource URL MUST come from trusted handler output, not
from the agent request.

**DAV-032** Missing delivery evidence MUST remain unknown and MUST NOT become
zero, rejection, or success.

## 6. Adoption Processing

### 6.1 Adoption states

The adoption state vocabulary is:

```text
accepted | rejected | pending | lifecycle | not_applicable | unknown
```

### 6.2 Initial adoption algorithm

For a delivered effect, the producer MUST apply these rules:

1. An unknown safe-output type is `unknown` with signal
   `unsupported_safe_output`.
2. A rule that is not attribution-eligible is `not_applicable` with signal
   `no_repository_value_effect`.
3. A `proposal` without fallback delivery is `pending` with signal
   `human_decision_pending`.
4. A rule with fallback delivery is `accepted` with signal `delivered`,
   source `delivery-rule`, and acceptance kind `fallback_delivery`.
5. Any other rule is `unknown` with signal `missing_acceptance_rule`.

**DAV-040** Initial adoption for an attribution-eligible effect MUST be
computed only after delivery is established. Infrastructure effects remain
`not_applicable`.

**DAV-041** Fallback delivery adoption MUST preserve the delivery clock when it
is available.

**DAV-042** Infrastructure effects MUST be `not_applicable`, not accepted,
rejected, or zero-valued.

**DAV-043** Attribution eligibility MUST NOT grant write permission, execution
authority, or collection authority.

### 6.3 Survival

An attribution-eligible effect has `survivalRequired: true`. This flag states
that later reversal evidence matters. It does not claim that survival has been
observed.

**DAV-044** A producer MUST NOT report survival merely because adoption was
accepted.

## 7. Adoption Observation Processing

The normalized gh-aw adoption triple is:

```text
adoption_status + evidence_strength + signal
```

The action-specific adoption specification is
`specs/safe-output-adoption-evaluation.md`.

### 7.1 Adoption refinement

An adoption evaluator MUST refine initial adoption as follows:

| Observed adoption status | Final adoption effect |
| --- | --- |
| `accepted` | `accepted`, source `adoption-evaluator` |
| `rejected` | `rejected`, source `adoption-evaluator` |
| `ignored` | `rejected`, source `adoption-evaluator` |
| `lifecycle`, `lifecycle_bot_close` | `lifecycle`, source `adoption-evaluator` |
| `pending` | Refines adoption only when initial adoption is already pending |
| `unknown`, `error`, `skipped` | Does not erase initial adoption |

**DAV-050** A generic existence check MUST NOT establish accepted adoption for
a proposal.

**DAV-051** A failed or unavailable later observation MUST NOT erase a
previously established delivery fact.

**DAV-052** A pending engagement observation for a fallback-delivery publication
MUST NOT replace its accepted delivery adoption.

**DAV-053** A delegation observed to have no effect MUST be represented as
rejected adoption.

**DAV-054** A lifecycle close MUST remain distinct from rejection.

### 7.2 Observation clocks

**DAV-055** An implementation MUST preserve delivery time, adoption observation
time, and authoritative decision time as distinct clocks when available.

**DAV-056** An implementation MUST NOT invent a decision time from the
collection time.

## 8. Operational Value

### 8.1 General requirements

**DAV-060** Operational value MUST measure an intended repository or operational
outcome, not agent activity, output count, run success, delivery count, or
adoption count.

**DAV-061** A Value measure MUST define its eligible opportunities, accepted
evidence, metric identifier, native unit, direction, formula, applicability,
and missing-data behavior before inspecting scored outcomes.

**DAV-062** A Value measure MUST count equivalent accepted evidence identically
regardless of whether the effect was produced by an agent, a person, or another
system.

**DAV-063** Missing evidence MUST produce unavailable or `null`, not an
invented zero.

**DAV-064** Exactly one metric SHOULD be primary. Diagnostics MUST remain
separate and MUST NOT be averaged into an undeclared composite.

### 8.2 Run-scoped grader

A run-scoped operational-value grader evaluates one gh-aw run from the redacted
unified session. It MAY use delivery and adoption evidence already
recorded in that session.

**DAV-065** A run-scoped grader MUST NOT fetch later repository state during
grading.

**DAV-066** A run-scoped grader MUST name its metric after the effect its
evidence proves, not a stronger downstream claim.

**DAV-067** A custom run-scoped operational-value grader MUST use
`graders.<distinct-id>.script` and execute only in the separate restricted
graders job. Shell-backed graders and `run` MUST be rejected.

**DAV-068** The JavaScript grader MUST receive immutable `session`, `config`, and
`helpers` snapshots, MUST NOT receive file, module, process, credential, network,
clock, random, or timer capabilities, and MUST return a finite number, `null`,
or an object with a finite or null `value`.

### 8.3 Repository-scoped Value

A repository-scoped Value measure evaluates a frozen native metric over
observation windows and eligible opportunities. It MAY use accepted adoption
as lineage or a measurement anchor, but MUST independently evaluate the
repository-native condition.

**DAV-069** Run-scoped grader results and repository-scoped Value records MUST
remain distinct populations. A downstream Value system MUST NOT relabel a gh-aw
operational grader result as repository-scoped Value.

## 9. Transport Contracts

### 9.1 Unified session

`safe_output.result` and `safe_output.delivery` data MAY contain:

```text
deliveryState
deliverySignal
deliverySource
adoptionState
adoptionSignal
adoptionSource
adoptionRuleVersion
effectClass
fallbackToDelivery
attributionEligible
survivalRequired
adoptionAt
acceptanceKind
```

The field names use camel case in the unified-session transport.

**DAV-070** Projection MUST derive these fields from allowlisted structured
evidence and the registry. It MUST NOT copy arbitrary source fields.

**DAV-071** Reprojecting an already normalized event MUST be idempotent.

### 9.2 Logs adoption reports

`gh aw logs --adoptions --json` publishes:

```json
{
  "delivery": {
    "state": "delivered",
    "signal": "safe_output.result",
    "at": "2026-11-16T12:00:00Z",
    "source": "safe-output-manifest"
  },
  "adoption_status": "pending",
  "evidence_strength": "medium",
  "signal": "open",
  "source": "adoption-evaluator",
  "rule_version": "gh-aw-v1.2.3-adoption-v1",
  "effect_class": "proposal",
  "fallback_to_delivery": false,
  "attribution_eligible": true,
  "survival_required": true
}
```

New live adoption evaluations MUST publish the adoption fields and SHOULD
publish the nested delivery object.

**DAV-072** JSONL adoption export MUST preserve the same delivery and adoption
fields as the in-memory report.

**DAV-073** Episode deduplication MUST compare delivery and adoption fields when
they are present.

## 10. Security and Privacy

**DAV-080** Adoption classification MUST NOT execute session text, request
content, handler output content, or error text as code.

**DAV-081** Credentials, signed URL query parameters, free-form request bodies,
and raw handler errors MUST NOT be copied into lifecycle fields.

**DAV-082** A producer MUST fail closed when an operation is indeterminate.

**DAV-083** Cross-repository identity MUST come from validated execution or
canonical resource evidence, not from untrusted display text.

**DAV-084** Registry classification MUST NOT broaden workflow permissions or
repository scope.

## 11. Versioning and Compatibility

**DAV-090** Changing the meaning of an existing registry rule requires a new
registry version.

**DAV-091** Adding a safe-output type requires adding one registry rule and
updating inventory parity tests in the same change.

**DAV-092** A registry version change MUST be explicit in every newly produced
adoption record.

**DAV-093** Downstream consumers MUST tolerate unknown future registry versions
without reclassifying them under an older version.

## 12. Conformance Testing

A conforming implementation MUST cover:

| Requirement area | Executable coverage |
| --- | --- |
| Registry inventory and mirror parity | `pkg/safeoutput/adoption_test.go`, `actions/setup/js/safe_output_adoption.test.cjs` |
| Initial delivery adoption | `TestEvaluateDelivery`, JavaScript delivery tables |
| Adoption precedence | `TestEvaluateAdoptionPrecedence`, `finalizeAdoptionEvaluation` tests |
| Unified-session projection and idempotence | `actions/setup/js/unified_session_payload.test.cjs` |
| Application status projection | `actions/setup/js/safe_output_deliveries.test.cjs` |
| Restricted operational value | `actions/setup/js/custom_graders.test.cjs`, `TestOperationalValueJavaScriptGraderCompileBoundary` |
| JSONL preservation | `pkg/cli/adoption_eval_jsonl_test.go` |
| Schema conformance | `pkg/cli/json_schema_adoption_compatibility_test.go` |
| Direct adoption semantics | `pkg/cli/testdata/adoption_conformance.json`, `specs/adoptions/` |

**DAV-100** The Go and JavaScript registry documents MUST be byte-equivalent
JSON values.

**DAV-101** The registry MUST cover exactly the built-in tool inventory plus
the compiler-managed dynamic output set.

**DAV-102** Tests MUST distinguish missing evidence, failed delivery, pending
adoption, accepted fallback delivery, and not-applicable infrastructure.

## 13. References

- [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119)
- [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174)
- [Safe Output Adoption Evaluation Specification](safe-output-adoption-evaluation.md)
- [Graders Specification](../docs/src/content/docs/specs/graders-specification.md)
- [Human-readable lifecycle model](delivery-adoption-operational-value.md)

## 14. Change Log

### 1.0.0 — 2026-11-16

- Migrates the safe-output adoption registry into gh-aw.
- Defines delivery, adoption, and operational-value boundaries.
- Adds unified-session and logs adoption transport contracts.
- Adds registry, projection, precedence, JSONL, and schema conformance coverage.
