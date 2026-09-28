---
graders:
  # Normalized dynamic time warping (nDTW) similarity between the observed
  # canonical state path and reference.states:
  # exp(-DTW(reference, observed) / (|reference| * threshold)), using a
  # discrete state distance (0 when state ids match, 1 otherwise) and a
  # success threshold of 1. Higher is better; 1.0 is an exact path match.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  trajectory-ndtw: {}
---

<!--
trajectory-ndtw computes normalized dynamic time warping between
reference.states[] and the ordered events[] whose kind is state_change (each
event ref is a canonical states[].id). The discrete state distance is 0 for
matching canonical ids and 1 otherwise, with a success threshold of 1, so
nDTW = exp(-DTW / |reference|). Runs without a reference state trajectory are
not applicable; missing state_change events or malformed state ids are
unavailable.
-->
