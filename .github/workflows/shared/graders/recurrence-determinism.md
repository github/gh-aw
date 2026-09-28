---
graders:
  # Recurrence Quantification Analysis (RQA) determinism (DET): the fraction
  # of recurrent points that fall on diagonal line structures (length >= 2)
  # in the state-recurrence matrix built from the canonical state/event
  # sequence, versus all recurrent points. High DET means the agent tends to
  # repeat the *same ordered subsequence* of states more than once (e.g.
  # re-running an identical multi-step probe), not just visiting the same
  # state in isolation. Lower is better: less repeated deterministic
  # structure means more novel, non-repetitive behavior.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  recurrence-determinism: {}
---

<!--
recurrence-determinism computes the RQA DET metric over the canonical state
sequence: the fraction of recurrent (i, j) pairs in the state-recurrence
matrix that belong to diagonal line structures of length >= 2, versus the
total number of recurrent pairs. It detects when an agent re-executes the
same ordered multi-step subsequence more than once (structural repetition),
which is distinct from state-revisit-probability-rep's simple revisit count
because DET specifically rewards/penalizes *ordered, repeated* structure
rather than isolated repeated states.
-->
