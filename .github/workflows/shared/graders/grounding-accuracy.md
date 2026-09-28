---
graders:
  # Fraction of issued actions that were structurally valid in the state in
  # which they were issued (actions[].validAtIssueTime). Higher is better.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  grounding-accuracy: {}
---

<!--
grounding-accuracy divides the number of actions[] entries whose
validAtIssueTime is true by the number of issued actions. The IR builder decides
validity against the valid-action schema of the state in which the action was
issued (for example, the tool existed, required arguments were present, and the
target resource existed). Runs without actions are not applicable; actions
missing a boolean validAtIssueTime make the result unavailable rather than
being guessed.
-->
