---
"gh-aw": major
---

Run graders as pure computation over the redacted in-memory unified session in
conclusion, with ten fast built-ins enabled by default.

Custom JavaScript runs only in a separate restricted job after conclusion,
receiving immutable in-memory session data and lookup helpers. Conclusion never
receives executable custom definitions. Audit conditionally downloads the
combined `graders` results artifact when custom IDs are registered, including
when refreshing cached reports. The logs `graders` artifact set includes both jobs'
results, including workflow-call-prefixed names and cached usage-only downloads.

Reserved `graders.operational-value.script` runs through the restricted
JavaScript lane and preserves its ordered metric-array contract: the first
metric is primary, remaining metrics are diagnostics, and schema-v2
artifacts/events retain the full array and implementation digest. CLI replay,
offline regrading, experiments, audit/logs, telemetry, and adapter consumers
retain the existing top-level source/value shape.

**Breaking change:** `graders.operational-value.run` Bash has been removed.
Migrate manually to restricted `graders.operational-value.script`. Invalid
legacy declarations fail explicitly and are never ignored or translated.

Legacy evaluator stdin requests and built-in overrides are rejected. Other
custom IDs use pure JavaScript and the `session, config, helpers` contract.
User-provided conclusion steps are rejected; move them to a separate job.
Expensive trajectory analyses now require explicit opt-in.

Grader artifacts use schema version 2 and live in `usage/graders/`, not the
agent artifact; custom results use the separate `graders` artifact. Recompile
workflows to adopt the split jobs. For
`gh aw graders run`, replace legacy trace payloads with unified-session JSONL
or a JSON event array; run-ID replay downloads the `usage` session.
