---
graders:
  # Longest correct execution prefix against reference.toolCalls, with
  # parameter correctness credit, normalized by reference length.
  # Higher is better; 1.0 reproduces the full reference with exact arguments.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  tool-wise-score: {}
---

<!--
tool-wise-score compares the ordered toolCalls[] (sorted by eventIndex when
every call has one, otherwise in array order) with
reference.toolCalls[]. The execution prefix extends while tool names match the
reference step-for-step. Each prefix step earns 0.5 for the correct tool plus
0.5 times the fraction of reference argument keys whose values match exactly
(key-order independent). The score is total credit divided by the reference
length. Runs without a reference trajectory are not applicable; malformed
reference or observed tool calls, or a mix of indexed and unindexed calls, are
unavailable.
-->
