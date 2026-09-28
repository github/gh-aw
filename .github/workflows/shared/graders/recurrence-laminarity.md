---
graders:
  # Recurrence Quantification Analysis (RQA) laminarity (LAM): the fraction
  # of recurrent points that fall on vertical line structures (length >= 2)
  # in the state-recurrence matrix built from the canonical state/event
  # sequence, versus all recurrent points. A vertical line in column j means
  # the agent stayed on states that all match one earlier fixed state across
  # several consecutive steps — i.e. it stagnated around that state instead
  # of moving on. This is distinct from recurrence-determinism (ordered,
  # repeated multi-step subsequences) and from
  # state-revisit-probability-rep (raw revisit count). Lower is better:
  # less laminar structure means less stagnation.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  recurrence-laminarity: {}
---

<!--
recurrence-laminarity computes the RQA LAM metric over the canonical state
sequence: the fraction of recurrent (i, j) pairs in the state-recurrence
matrix that belong to vertical line structures of length >= 2, versus the
total number of recurrent pairs. A vertical line means several consecutive
steps all matched one earlier fixed state, which identifies stagnation
(the agent stopped making progress and kept re-entering the same state).
It complements recurrence-determinism, which measures ordered repeated
subsequences (diagonal structure), and state-revisit-probability-rep,
which only counts how many visits were revisits.
-->
