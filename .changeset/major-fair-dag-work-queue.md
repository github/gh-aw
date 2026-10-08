---
"gh-aw": major
---

Replace the experimental work queue with the current-only fair DAG protocol.

**Breaking change:** the operator and workflow runtime use one versioned
`QueueCommit` contract on `work-queue.jsonl`; old fact records, scalar
`work_queue_claim` assignments, automatic upgrades, Issues storage and explicit
Work-selection Claim bypasses are unsupported. Native Go and JavaScript engines
use the same schemas, scheduling rules and conformance fixtures.

Version-3 payload numbers use canonical plain safe integers only; fractional,
exponent, negative-zero and unsafe-integer tokens are rejected explicitly.
Represent other exact quantities as strings. No automatic payload conversion is
performed.

This restriction applies recursively to immutable Work payloads, not only
scheduler metadata. It avoids cross-engine rounding and ambiguous request
fingerprints. Typed producer inputs also reject negative zero instead of
normalizing it to zero. Accepted payload fields and decoded values are preserved
losslessly; canonical key ordering and JSON escaping are not source-format
preservation. This is a restricted JSON profile, not arbitrary numeric JSON
support.

Quiesce old writers and workflows before deploying the new protocol. Preserve
old history and initialize the new protocol explicitly; readers do not migrate,
reset or rename an existing queue. Recompile workers to receive immutable
`work_queue_assignment` arrays. Single-Claim workers may omit output selectors;
multi-Claim workers must identify the Claim on every safe output and finish intent.

Queue launches require the pinned run-details API contract and verified run
binding. Uncertain launches retain reservations rather than being retried or
force-released. DAG successors require verified Results, not worker completion
alone. Branch writer-restriction verification/provisioning remains explicitly
deferred; deployments must independently enforce the documented writer boundary.
