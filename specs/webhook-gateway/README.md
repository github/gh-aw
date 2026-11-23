# Webhook gateway: bounded TLA+ security specification

[`WebhookGateway.tla`](WebhookGateway.tla) models the
[webhook gateway contract](../../docs/src/content/docs/specs/webhook-gateway-specification.md):
source admission, discovery fencing, durable dispatch/replay state, worker races,
App provenance, hash-bound admission and destination continuation.
The generated [threat model](threat-model.md) records TLC results, attack controls,
reachability witnesses, trust assumptions and residual risks.

The receiver and admission server are external integration requirements, not
services implemented here. The four gateway JavaScript components map to these
normative requirements and safeguards; the model abstracts the checks as
validity predicates and does not prove implementation refinement.

| Component | Norm | Safeguard |
| --- | --- | --- |
| [`check_gateway_provenance.cjs`](../../actions/setup/js/check_gateway_provenance.cjs) | [Mandatory destination gate](../../docs/src/content/docs/specs/webhook-gateway-specification.md#mandatory-destination-gate) | Binds the original event, sender, actor and destination to the centrally approved App identity. |
| [`check_gateway_admission.cjs`](../../actions/setup/js/check_gateway_admission.cjs) | [Workflow-hash-bound admission](../../docs/src/content/docs/specs/webhook-gateway-specification.md#workflow-hash-bound-gettoken-admission) | Checks bounded OIDC claims and an exact closed approval response; signature and artifact verification remain server responsibilities. |
| [`validate_webhook.cjs`](../../actions/setup/js/validate_webhook.cjs) | [Dispatch envelope](../../docs/src/content/docs/specs/webhook-gateway-specification.md#dispatch-envelope) | Enforces the versioned closed envelope, configured selectors and filters, and payload size limits. |
| [`webhook_context.cjs`](../../actions/setup/js/webhook_context.cjs) | [Derived context and responsibility boundary](../../docs/src/content/docs/specs/webhook-gateway-specification.md#derived-context-and-responsibility-boundary) | Derives and deduplicates resources from inline deliveries and provides webhook-payload redaction. |

## Reproduce and regenerate

Use Java 21 and the repository's checksum-pinned TLA+ Tools v1.7.4
(TLC 2.19), following the
[formal-methods runtime instructions](../../.github/aw/formal-methods.md).
On macOS, the installer supports `--jar-only` with separately installed Java.
The checker verifies the pinned jar digest before executing it.

From the repository root, with a new results directory:

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s specs/webhook-gateway -p '*_test.py'
JAVA_BIN=/path/to/java TLA2TOOLS_JAR=/path/to/tla2tools.jar \
  PYTHONDONTWRITEBYTECODE=1 python3 specs/webhook-gateway/check.py \
  --results "$TMPDIR/webhook-gateway-tlc" \
  --report specs/webhook-gateway/threat-model.md
```

The report and adjacent `threat-model.json` are generated only after every case
has the expected result and measured inputs remain unchanged. Preserve the full
results directory when archiving evidence: each case includes a self-contained
model/config, full TLC log (including counterexample states), and state data.
The JSON records exact commands, input/config/log hashes, Java identity and
verdicts. Negative-control and witness state counts are explored prefixes,
**not** exhausted state spaces.

`WebhookGateway.cfg` enables all 18 invariants. The checked-in secure report
records 81,295 generated states (23,620 distinct; zero queued), with all 18
invariants satisfied. The runner also checks 18 single-fault mutations against
exact expected invariant failures, then checks six reachability witnesses with
security invariants still enabled. Parse errors, timeouts, deadlocks, wrong
invariant failures, missing traces, wrong tooling and incomplete baseline
searches are failures.

## Abstraction and ordering

There are 13 finite request profiles, one scoped source/destination/subscription
receipt, two workers, two attempts, one revocation and one restart. Receipt
admission atomically persists the original identity/content binding, immutable
projection and pending dispatch. Conflicting duplicates cannot overwrite it.
Worker tickets represent durable lease fences, not process-local locks.

Discovery may race revocation. A complete scan publishes only if its captured
generation still matches current approval. Each send rechecks approval and
records the attempt at one local ordering point. Authorization snapshots remain
historical facts after revocation; no invariant incorrectly claims that a later
revocation recalls a request. Failed responses permit retries only when
nonacceptance is known; lost responses and crashes around a send quarantine it.
A recorded acceptance means GitHub accepted the dispatch, not that an agent ran.

An unrelated principal can start a run outside the receiver, exercising the App
gate independently. Server admission abstracts signature/claim, configuration
hash and exact-artifact verification separately; client continuation requires a
correlated closed response, then activation validates the envelope. Revocation
blocks a later server grant, not continuation already issued before revocation.
No fairness, eventual execution or exactly-once guarantee is asserted.

## Maintenance

Update the contract and source mapping when changing modeled gates. Add a
single-fault control for each new boundary and a witness for new positive
behavior, then regenerate the report. Do not interpret deliberate control
mutations as current-code vulnerabilities. This suite is explicitly invoked;
it is not wired into the Go unit-test suite or a live workflow dispatch.
