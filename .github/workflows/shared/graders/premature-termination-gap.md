---
graders:
  # Number of declared completion conditions still unsatisfied when the trace
  # ended. Lower is better; zero means every declared objective was completed.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  premature-termination-gap: {}
---

<!--
premature-termination-gap counts objectives[] entries whose
satisfiedAtEventIndex is null or absent at the end of the trace. It preserves
the native count rather than normalizing it. Runs without declared objectives
are not applicable; malformed objective metadata is unavailable.
-->
