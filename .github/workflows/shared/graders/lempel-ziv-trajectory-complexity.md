---
graders:
  # LZ76 (Kaspar & Schuster) incremental-parsing complexity of the ordered
  # canonical event-symbol sequence, projected purely from the Trajectory
  # IR's events[] (no states, provenance, or objectives required).
  # tool_call events are qualified by their ref (finer alphabet
  # granularity, matching the same convention used by event-entropy-rate);
  # other events use their kind. The raw phrase count produced by the
  # incremental copy/insert parsing rule is normalized by the theoretical
  # asymptotic upper bound n / log_b(n) (b = alphabet size) to stay in
  # [0, 1] and remain comparable across traces of different
  # lengths/alphabets. Unlike event-entropy-rate (first-order,
  # previous-symbol-conditioned predictability), LZ76 complexity captures
  # compressibility from arbitrary-length repeated substrings/motifs
  # anywhere earlier in the sequence, without assuming a Markov model.
  # Lower values indicate a highly compressible trace dominated by a few
  # repeated motifs (likely stuck in a loop); values near 1 indicate a
  # near-incompressible, structurally diverse trace.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  lempel-ziv-trajectory-complexity: {}
---

<!--
lempel-ziv-trajectory-complexity computes the normalized LZ76 (Kaspar &
Schuster) incremental-parsing complexity of the ordered canonical
event-symbol sequence built purely from the Trajectory IR's events[] (no
states, provenance, or objectives required). It measures whole-sequence
compressibility from arbitrary-length repeated substrings/motifs anywhere
earlier in the sequence, without assuming a Markov model — distinct from
event-entropy-rate, which only captures first-order (previous-symbol-
conditioned) statistical predictability, and from the recurrence-* family,
which requires canonical states and measures revisit density/structure
rather than whole-sequence compressibility.
-->
