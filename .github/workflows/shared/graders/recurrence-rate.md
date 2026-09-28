---
graders:
  # Recurrence Quantification Analysis (RQA) recurrence rate (RR): the overall
  # density of recurrent points in the state-recurrence matrix built from the
  # canonical state/event sequence, excluding the line of identity (i != j).
  # RR captures how much of the run revisits previously-seen states in any
  # form. Lower is better: less time spent in previously-visited states.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  recurrence-rate: {}
---

<!--
recurrence-rate computes the RQA RR metric over the canonical state sequence:
the density of recurrent (i, j) pairs in the state-recurrence matrix where
i != j and state_i == state_j. Unlike recurrence-determinism (diagonal
structure) and recurrence-laminarity / recurrence-trapping-time (vertical
structure), RR is the base recurrence density itself: the aggregate share of
the run spent revisiting any previously-seen canonical state.
-->
