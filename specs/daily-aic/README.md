# Daily AI Credits TLA+ models

[`DailyAICGuardrail.tla`](DailyAICGuardrail.tla) models guardrail admission
from §§9.8–9.15 of the [AI Credits specification](https://github.github.com/gh-aw/specs/ai-credits-specification/#9-daily-ai-credits-guardrail).
It checks that artifact decisions use a complete fresh listing and one
contribution per member, with separate whole-run estimates. Reuse requires
trusted recorded evidence matching the current attempt identity. Quota deferral
restarts the phase, discards partial results, and cannot itself admit a run.
An explicit fail-open policy may allow startup without publishing a daily total
or changing the outcome to `under_budget`. Repo-memory backend selection
distinguishes trusted coverage, artifact bootstrap, clone failure, and the
explicit insecure override; a permissive ledger total is not an artifact
completeness claim.

The model uses two historical run IDs, two pages, and one deferral per phase.
It abstracts artifact validation, the timestamps in cache identities, retry
deadline eligibility, and ledger shard contents. It does not model provider
billing, concurrent budget reservations, or the ledger writer. Termination
assumes weak fairness of the modeled progress actions.

Run the guardrail checks from this directory:

```sh
java -cp "$TLA2TOOLS_JAR" tlc2.TLC \
  -config DailyAICGuardrail.cfg DailyAICGuardrail.tla
```

## Cancelled rerun accounting

`CancelledRerunAccounting.tla` checks that carried-forward usage is accepted
only for a cancelled component attempt with no verified current producer, one
verified prior producer, and complete accounting. It also checks that missing
accounting and ambiguous prior jobs fail closed, that other outcomes require a
verified current producer, and that a verified current producer takes
precedence.

The finite input domains include four outcomes, three current-producer states,
four prior-producer states, and two accounting states. `"verified"` prior
producer represents a unique earlier executed job. This bounded model checks
the decision rules, not GitHub artifact APIs, timestamp parsing, or the
JavaScript implementation.

With Java and TLA+ Tools installed:

```sh
java -cp /path/to/tla2tools.jar tlc2.TLC \
  -config CancelledRerunAccounting.cfg \
  -metadir /tmp/gh-aw-daily-aic-tlc \
  CancelledRerunAccounting.tla
```

The concrete boundary tests are in
`actions/setup/js/daily_aic_component_coverage.test.cjs` and include the
T-AIC-DG-026 verified-carry-forward case.

Configuration alignment (2026-11-15): `CHECK_DEADLOCK FALSE` matches the
workflow-security TLA+ configurations. TLC was not run for this update because
the pinned `TLA2TOOLS_JAR` is unavailable in the environment; the invocation
above remains the reproducible model-check command.

## Requirements

`CancelledRerunAccounting.tla` must never accept carried-forward usage unless
the component attempt was cancelled, no current producer is verified, exactly
one prior producer is verified, and accounting is complete. A verified current
producer takes precedence over any prior result.

## Safeguards

Missing accounting and ambiguous prior jobs fail closed; neither may be treated
as evidence for carried-forward usage. These rejection paths are modeled in
`CancelledRerunAccounting.tla`.

## Norms

A uniquely identified prior producer may supply usage for a cancelled rerun.
Other outcomes require a verified current producer, as specified by
`CancelledRerunAccounting.tla`.
