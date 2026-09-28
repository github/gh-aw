---
graders:
  # Detects "successful" traces (traces that emitted at least one safe_output
  # event) which nonetheless left one or more guard/policy-shaped objectives
  # unsatisfied -- i.e. traces that reached the correct outcome without
  # performing required checks. Guard-shaped objectives are matched by
  # keyword against objectives[].description. Lower is better: fewer
  # near-misses.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  policy-near-miss: {}
---

<!--
policy-near-miss flags "successful" traces (at least one safe_output event
emitted) that nonetheless left one or more guard/policy-shaped objectives
unsatisfied -- runs that reached the correct outcome without performing a
required check. Guard-shaped objectives are identified by keyword match
against objectives[].description (e.g. "check", "verify", "policy",
"approval"); it does not evaluate objectives that aren't guard-shaped
(see objective-coverage, not yet implemented, for that). Reports
not-applicable (passed: null) rather than a fabricated value when no
objectives are declared, no outcome was reached, or no guard-shaped
objectives exist in the trace.
-->
