---
"gh-aw": minor
---

Add opt-in routing for explicit `request_continuation` requests. Set `safe-outputs.request-continuation.route` to:

- `workqueue` (assignment-bound queue workers): the worker's finish commit records the parent's `Completion` and one dependent successor Work in the same ledger commit. The successor inherits the parent's graph, pool, priority, fairness key, profile and payload, and receives the message as `assignment.claims[i].continuation`. `max-depth` (1–16, default 1) bounds automatic chains.
- `issue` (queue workers): the publisher opens one approval issue in the queue repository; the successor stays unschedulable until a user with write, maintain or admin permission closes that exact issue as completed. Each approval releases one successor.
- `dispatch` (ordinary workflows): `Process Safe Outputs` dispatches the named local workflow on the default branch with a bounded `gh_aw_continuation` input. The receiver must declare that string input; `max-depth` bounds the chain across hops.

Routes publish only for the producing run attempt: a new `continuation_evidence` agent-job output binds the agent-output artifact. Advisory `request-continuation` forms (no `route`) are unchanged.

**Compatibility:** queue ledgers gain an optional Work `continuation` member, `finish` commits that carry a successor Work, an `approved` Issue condition and Observation `approved_by`. Older v4 readers reject a finish that carries Work; upgrade workers, dispatchers, maintenance and the CLI before enabling a queue route against a shared ledger.
