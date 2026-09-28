---
graders:
  # For runs that left one or more declared objectives unsatisfied, measures
  # whether the failure was due to insufficient search: 1 - (observations /
  # distinctStatesVisited), clamped to [0, 1]. distinctStatesVisited comes
  # from distinct state_change event refs, falling back to the declared
  # states[] count when no state_change events are recorded. Runs with all
  # objectives satisfied score 0 (no exploration error to attribute). This is
  # the complement of exploitation-error, which covers runs that had
  # enough evidence but failed anyway. Lower is better: fewer
  # unmet objectives attributable to insufficient search.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  exploration-error: {}
---

<!--
exploration-error attributes objective failure to insufficient search: for
runs that left one or more declared objectives unsatisfied
(satisfiedAtEventIndex null/undefined), it computes
1 - (observations / distinctStatesVisited), clamped to [0, 1]. A low
observation-to-state ratio (few observations gathered across the states the
run visited) yields a score near 1 -- the run likely failed because it never
gathered enough evidence. distinctStatesVisited is the count of distinct
refs across events[] of kind "state_change"; when no such events are
recorded it falls back to the declared states[] count. Runs with all
objectives satisfied score 0 -- there is no exploration error to attribute,
since exploration failures only apply to failed runs. This is the
complement of exploitation-error, which covers runs that had enough
evidence but misused it. Reports not-applicable
(passed: null) when no objectives are declared, or when neither
state_change events nor declared states are present in the trace.
-->
