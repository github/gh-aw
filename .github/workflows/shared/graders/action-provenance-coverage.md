---
graders:
  # Fraction of consequential actions with a provenance path to a tool call or
  # observation in the canonical Trajectory IR. Higher is better.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  action-provenance-coverage: {}
---

<!--
action-provenance-coverage selects consequential actions using the canonical
actions[].consequential flag (not a substring guess on actions[].type) and
follows provenanceEdges backward from each actions[].id to a toolCalls[].id or
observations[].id evidence root. Read-only actions are outside the
denominator. Missing graph fields, a missing consequential flag, or missing
action IDs make the result unavailable.
-->
