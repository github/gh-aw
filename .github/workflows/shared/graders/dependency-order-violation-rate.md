---
graders:
  # Fraction of completed objectives with declared dependencies that completed
  # before at least one prerequisite. Lower is better.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  dependency-order-violation-rate: {}
---

<!--
dependency-order-violation-rate evaluates completed objectives that declare
objectives[].dependsOn. A completed dependent objective is a violation when any
prerequisite is incomplete, invalid, or completed at a later event index.
Unknown dependency IDs or a non-array dependsOn value make the graph
unavailable; traces without dependencies or without a completed dependent
objective are not applicable.
-->
