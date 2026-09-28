---
graders:
  # Number of trace events after the event where the final declared objective
  # became satisfied. Lower is better: stop promptly once evidence is complete.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  evidence-saturation-stopping-lag: {}
---

<!--
evidence-saturation-stopping-lag finds the latest
objectives[].satisfiedAtEventIndex and counts events[] entries after it. It is
not applicable until every declared objective is satisfied. Invalid indexes or
any satisfaction index absent from the trace make the result unavailable.
-->
