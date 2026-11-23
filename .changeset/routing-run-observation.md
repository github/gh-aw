---
"gh-aw": minor
---

Record whether a routed run's result can teach a router anything. For each run that AWF routed and whose agent made requests, the conclusion job's unified session now records a `routing.observation` verdict: `success` or `failure` when the result counts as evidence about the model and effort AWF selected, or `skip` with the reason it does not (`cancelled`, `infrastructure`, `rate_limited`, `pair_mismatch`, or `unknown_evidence`). The verdict uses only AWF's request records, the job results and the validated safe outputs, so it works for every engine. The selected pair and its cost stay in the AWF records the session already holds. `gh aw audit` and `gh aw logs --json` show the verdict.
