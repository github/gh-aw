---
graders:
  # Normalized first-order (bigram, previous-symbol-conditioned) Shannon
  # entropy rate H(X_t | X_{t-1}) of the ordered canonical event-symbol
  # sequence, projected purely from the Trajectory IR's events[]. tool_call
  # events are qualified by their ref (finer alphabet granularity); other
  # events use their kind. Normalized by log2(alphabetSize) to stay in
  # [0, 1] and remain comparable across traces with different event
  # alphabets. Measures conditional unpredictability of the event process:
  # a trace bouncing unpredictably between few event kinds can score higher
  # than one with many kinds arranged in a strict repeating cycle.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  event-entropy-rate: {}
---

<!--
event-entropy-rate computes the normalized first-order (bigram) Shannon
entropy rate H(X_t | X_{t-1}) of the ordered canonical event-symbol
sequence built purely from the Trajectory IR's events[] (no states,
provenance, or objectives required). It measures conditional
unpredictability of the event process rather than raw distinct-event-kind
count or revisit density: a trace bouncing unpredictably between few event
kinds scores higher than one with many kinds arranged in a strict
repeating cycle, distinguishing it from the recurrence-* family of graders
which measure revisit structure, not information-theoretic diversity.
-->
