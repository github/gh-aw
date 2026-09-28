---
graders:
  # Fraction of final safe outputs that can be traced backward through the
  # canonical Trajectory IR provenance graph to a tool call or observation.
  # Higher is better: every delivered output should have recorded evidence.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  end-to-end-lineage-completeness: {}
---

<!--
end-to-end-lineage-completeness follows provenanceEdges backward from each
events[].ref whose kind is safe_output and counts the fraction that reaches a
toolCalls[].id or observations[].id evidence root. It reports unavailable when
the trace lacks the graph contract or a safe output lacks its required ref, and
not-applicable when the run emitted no safe output.
-->
