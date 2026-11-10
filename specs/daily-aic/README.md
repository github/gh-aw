# Cancelled daily AIC reruns: bounded TLA+ model

`CancelledRerunAccounting.tla` checks that carried-forward usage is accepted
only for a cancelled component attempt with no verified current producer, one
verified prior producer, and complete accounting. It also checks that missing
accounting and ambiguous prior jobs fail closed, that other outcomes require a
verified current producer, and that a verified current producer takes
precedence.

The finite input domains include four outcomes, three current-producer states,
four prior-producer states, and two accounting states. `"verified"` prior
producer represents a unique earlier executed job whose producer artifact is
unexpired and timestamp-bound to that job. This bounded model checks the
decision rules, not GitHub artifact APIs, timestamp parsing, or the JavaScript
implementation.

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

Only a uniquely identified, timestamp-bound prior producer may supply usage for
a cancelled rerun. Other outcomes require a verified current producer, as
specified by `CancelledRerunAccounting.tla`.
