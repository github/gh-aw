---
graders:
  # Measures the fraction of visited canonical states that are redundant revisits:
  # (visited states - distinct states) / visited states. Lower is better.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  state-revisit-probability-rep: {}
---

<!--
state-revisit-probability-rep measures structural exploration redundancy as
(visited states - distinct states) / visited states over canonical state visits.
It catches wasted exploration where an agent returns to known behavioral states
anywhere in the trajectory, not just adjacent loops.
-->
