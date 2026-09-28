---
graders:
  # Measures the fraction of tool-originated observations that were actually
  # referenced by a later action, per observations[].consumedByActionIds in
  # the canonical Trajectory IR. A low rate indicates the agent frequently
  # called tools and then ignored their outputs (wasted or speculative tool
  # calls); a high rate indicates most tool outputs fed into subsequent
  # decisions. Higher is better.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  tool-output-consumption-rate: {}
---

<!--
tool-output-consumption-rate computes the fraction of tool-originated
observations whose consumedByActionIds array is non-empty in the canonical
Trajectory IR, i.e. the fraction of tool outputs that a later action
actually referenced. Depends on observations[].sourceToolCallId,
observations[].consumedByActionIds, and toolCalls[].id from the IR. Reports
not-applicable (passed: null) when the trace has no observations, or no
observations that originate from a matching tool call, rather than fabricating
a value. Complements the built-in tool-success-rate grader (which measures
whether tool calls succeeded, not whether their outputs were used).
-->
