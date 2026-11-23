# ADR-80468: Record a Routing Observation Verdict for Routed Runs

**Date**: 2026-11-23
**Status**: Draft
**Deciders**: Not stated in PR evidence (PR author: rabeckett)

## Context

When AWF routes a run, it selects a model and effort for it. Three existing signals answer different questions: the routing selection, the harness outcome, and the safe-output result. None of them says whether the run's result is evidence about the pair AWF actually selected. Issue [#79103](https://github.com/github/gh-aw/issues/79103) names two failure modes. A setup timeout or a 429 counted as a model failure teaches a router the wrong lesson. So does a result credited to a model or effort the harness did not use.

Forces:

- The unified session already holds, for every routed run, the selected pair, whether each request ran as selected (`routed`), and each request's priced cost.
- Job results and validated safe outputs are known only in the conclusion job, after the other jobs finish.
- The verdict must mean the same thing for Copilot, Claude, Codex, and Pi, so it cannot depend on engine-specific logs.
- Cost must come from the firewall's priced requests, not a second accounting path. Missing cost is not zero.

## Decision

For each routed run whose agent made requests, the conclusion job adds one `routing.observation` event to the unified session. The event is either:

- `success` or `failure`, when the result counts as evidence about the selected model and effort; or
- `skip`, with one reason: `cancelled`, `infrastructure`, `rate_limited`, `pair_mismatch`, or `unknown_evidence`.

The verdict uses only:

- AWF's request records (`model_routing`, `token_usage`), attributed by the rule routing analytics already applies (`attributeRequest` in `routing_analytics.cjs`);
- the job results;
- the validated safe outputs in `agent_output.json`; and
- threat detection's result.

The rules apply in this order: cancellation; platform causes, for runs that did not succeed; rate limits; whether the selected pair ran; then the result. A run that succeeded is not re-examined for platform errors it recovered from. A success needs a successful agent job, a terminal safe output (a valid `noop` counts), and a clean threat detection result. Output that threat detection flagged is skipped as `unknown_evidence`.

Only the conclusion job computes the verdict, because only it has the final job results. The collector's entry point enables it, so the compiler does not change and no lock file is recompiled. The collector drops `routing.observation` events from any other source. It also records `collectorInfrastructureCause: "safeoutputs_gateway_empty"` in `agent_output.json`, so a gateway that registered no tools is not mistaken for the agent's invalid outputs.

The verdict appears in `gh aw audit` and as `model_routing.observation` in `audit --json` and `logs --json`. Its vocabulary is defined in version 2.14.0 of the unified agent session specification (T-UAS-094) and in the audit reference. Whether a run may update a routing history is left to the history writer, not the verdict.

## Alternatives Considered

1. **Use the agent job's or the conclusion job's success as the verdict.** Rejected. A setup timeout or a 429 would count as a model failure, and a run that used a model other than the one selected would be credited to the selection.
2. **Parse each engine's logs for outcome evidence.** Rejected. Issue #79103 requires evidence that every engine produces, so the meaning is the same for Copilot, Claude, Codex, and Pi.
3. **Compute the verdict in the agent job.** Rejected. The agent job does not have the final job results that the platform-cause and output rules depend on.
4. **Compute cost in a separate accounting path.** Rejected. The firewall's priced requests are the single source of cost, and a second path could disagree with them.

## Consequences

**Positive**

- Platform causes (outages, timeouts, 429s, cancellations) no longer count as model failures.
- A result counts only when the selected pair actually ran, so the verdict is attributable.
- The verdict means the same thing across engines and needs no compiler or lock-file change.
- Old artifacts should remain readable, as issue #79103 requires. The PR evidence reviewed here does not show a specific test for this; the author should confirm it.

**Negative**

- The precedence and classification tables are large (platform, rate-limit, and behavioral sets, plus the wrapper and guard exceptions). They must be kept in step with AWF's category names.
- An agent-filed `infrastructure_error` report counts neither as success nor failure, so a real infrastructure failure that the agent reports yields no signal.
- Many routed runs will produce `skip` verdicts, which give no learning signal for routing history.
- The vocabulary is closed. A new reason or outcome requires coordinated changes to the session schema, the specification, and the audit output.
- Consumers must read two separate decisions: the verdict, and the history writer's choice about whether to record it.
