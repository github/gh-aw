---
graders:
  # Recurrence Quantification Analysis (RQA) trapping time (TT): the mean
  # length of vertical line structures (length >= 2) in the state-recurrence
  # matrix built from the canonical state/event sequence. A vertical line means
  # consecutive steps all match one fixed previously visited state (stagnation).
  # TT complements recurrence-laminarity: LAM measures how much recurrent
  # structure is vertical, while TT measures how long each vertical episode
  # lasts on average once it starts. Lower is better: shorter stagnation runs.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  recurrence-trapping-time: {}
---

<!--
recurrence-trapping-time computes the RQA TT metric over the canonical state
sequence: the mean length of vertical line structures of length >= 2 in the
state-recurrence matrix. A vertical line means consecutive steps all matched
one previously visited state, i.e. a stagnation episode. This complements
recurrence-laminarity (how much recurrent structure is vertical) by measuring
how long each vertical stagnation episode lasts once it starts.
-->
