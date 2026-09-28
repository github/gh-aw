---
graders:
  # Fraction of precompiled, workflow-supplied behavioral constraints
  # (config.constraints: { id, description, pattern, requireSuccess }) that
  # were both exercised (regex pattern matched at least one toolCalls/actions
  # entry) and passed (all matches succeeded/were valid at issue time, unless
  # requireSuccess: false) during the run. Constraints are stable across runs
  # of the same harness/skill, unlike per-run inferred objectives. Higher is
  # better: more declared requirements exercised and satisfied.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  skill-constraint-coverage: {}
---

<!--
skill-constraint-coverage converts a precompiled, workflow-supplied list of
behavioral constraints (config.constraints: { id, description, pattern,
requireSuccess }) into a harness-improvement signal: the fraction that were
both exercised (regex pattern matched at least one toolCalls/actions entry,
matched case-insensitively against "name arguments" for tool calls and "type
target" for actions) and passed (all matches succeeded/were valid at issue
time, unless requireSuccess: false) during the run. A tool call that started
but never completed (completed: false) counts as unsuccessful. Unlike policy-near-miss
(keyword-matched guard objectives already declared as IR objectives) or the
not-yet-implemented objective-coverage (inferred per-run objectives),
constraints here are stable across runs of the same harness/skill, making the
metric trackable over time. The denominator is every supplied constraint,
including ones with an empty/invalid regex pattern -- malformed constraints
count as unmet rather than being silently dropped from the fraction, which
would otherwise inflate the score. A single constraint pattern may match
multiple entries; passing requires all of them to have succeeded/been valid,
unless requireSuccess: false. Reports not-applicable (passed: null) when no
constraints are configured, none have a valid pattern, or the trace lacks
toolCalls/actions.
-->
