---
graders:
  # Fraction of explicitly declared objectives that were completed during the
  # trace. Higher is better; 1.0 means every declared objective was satisfied.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  objective-coverage: {}
---

<!--
objective-coverage divides the number of objectives[] entries with a valid
satisfiedAtEventIndex by the number of declared objectives. It is the
normalized complement of premature-termination-gap. Runs without declared
objectives are not applicable; malformed objective metadata or invalid
satisfaction indexes are unavailable.
-->
