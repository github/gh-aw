# Observed executions query pack v1

This read-only query gallery is a minimal dashboard surface for **observed
executions**, not fleet-complete activity. `dashboard.json` names the panels and
their shared prelude; it is a repository manifest, **not** an Azure Workbook or
deployment template. There is no service, inventory collector, pricing model, or
new telemetry instrumentation.

## Azure query gallery

Concatenate `azure/common.kql` and **one** panel file, then paste the result into
Azure Monitor Logs. For example, from the repository root:

```bash
cat queries/observed-executions/v1/azure/common.kql \
    queries/observed-executions/v1/azure/usage.kql
```

Set the prelude's `Start`, `End`, `Repository`, `RunId`, `AttemptMode`, and
`RunAttempt` literals before querying. Use fixed UTC datetimes and the same
parameters for every panel when comparing results. The window is `[Start, End)`
on the native table's `TimeGenerated`, **not** an ingestion-time or complete-run
selection. A span starting before the window is excluded even if it ends inside
it. Collector delays, query retention/time-picker limits, and missing exports can
leave any execution incomplete. A null/unavailable start cannot be assigned to
this window.

| Panel | Result contract |
| --- | --- |
| `coverage` (required) | Observed execution keys and span counts, physical copies, invalid identity, correlation gaps, conflicts, sampling and dropped fields/events. |
| `failures` | One deduplicated span per observed failure/cancellation/timeout or export diagnostic. Export errors are a separate signal, not functional workflow failures. |
| `phases` | One setup, dedicated agent execution, or job window per span; missing/invalid/conflicting timing is unknown. |
| `usage` | AIC and token **observed subtotals**, grouped by repository, workflow ref, owning job and engine, with separate known-owner denominators. |
| `models-routing` | One accounting owner with requested/effective/response model, effort, routing classification/status, deviations and diagnostic classifier AIC. |
| `graders` | One result per owning span, grader ID and source; physical copies and conflicting results qualified explicitly. |
| `compact-quality` (optional) | Per parent-span compact export quality; physical record counts, timing availability, omitted/oversized/source-scan limits, and absent exports. |

These are tables rather than a claimed final workflow success rate or a chart
that silently combines incomparable clocks. Keep `coverage` visible alongside
the other panels. A missing result is **unknown**, not an observed zero, passing
grader, disabled feature, or proof that no execution happened.
`WindowPhysicalSpans` and `WindowInvalidIdentity` are preselection window-wide
diagnostics; unlike selected-span counts, they are not narrowed to a requested
repository/run.

## Selection, identity and ownership

`AttemptMode` is explicit: `all` retains every observed attempt (including
unattributed records for diagnosis); `exact` requires a positive `RunAttempt`;
`latest-observed` selects the largest observed attempt for each repository/run
**within the window**, then retains all its jobs and workflow refs. It does not
assert that this is GitHub's latest attempt, or mix a successful old attempt into
a failed rerun. Invalid selection returns no activity and `SelectionValid=false`.

Physical spans are deduplicated by **trace ID and span ID**, before accounting.
No statistical sampling weights are applied. Invalid physical IDs appear only
in the window-level coverage count. Conflicting copies have a deterministic
lexicographic representative and an explicit flag; their usage is unavailable.
The signature excludes provider-specific workspace metadata so mirrors do not
count as new work. Differently ordered attribute bags can conservatively appear
as conflicts; v1 does not merge disagreeing measurements.

An observed execution key is `(repository, run, attempt, workflow ref, hop)`.
Workflow ref is required for usable accounting: current hop IDs can be shared by
reusable workflows inside a run. Job instances are **not** collapsed by job name:
each owning conclusion span is a separate instance. The dedicated
`gh-aw.agent.agent` span replaces only the conclusion whose span ID equals its
`ParentSpanId` in the same trace. This preserves matrix instances, other jobs,
and reusable workflow hops. Missing workflow/run/job/attempt correlation makes
accounting unavailable, while diagnostic rows remain visible.

Accounting uses the dedicated agent span, even when its usage is absent.
It never fills its missing counters from a copied conclusion. Without a
dedicated span, the `agent`, `detection`, or engine-named executing job's own
conclusion is the fallback. `safe_outputs`, `conclusion`, model/tool spans, compact
`usage.report` records, and downloaded downstream copies do not own usage.
Detection and engine jobs remain separate accounting owners.

Numeric zero is measured. Absent, null, negative, nonfinite or nonnumeric counters
are unavailable. Each usage subtotal is null when its known-owner count is zero;
otherwise it sums **only measured owners**. Compare each denominator to `Owners`
before interpreting a subtotal as complete for the observed owners.
`UnusableOwners` additionally identifies correlation, conflict, sampling, or
dropped-attribute problems. A total token measurement requires both input and
output; a partial pair and any copied `total_tokens` do not establish a total.
Cache read/write counters are separate and are not added to that total.

Model fields describe observations, not semantic equivalence or pricing.
`ClassifierAicDiagnosticOnly` is **never** added to/subtracted from `gh-aw.aic`.
Routing selection, failure, degradation and deviations are not counterfactual
savings; missing routing fields mean unavailable/not observed, not necessarily
“routing disabled.”

## Time, graders and compact evidence

Setup spans measure setup. The dedicated agent span measures the recorded agent
execution window (which can use emitter fallbacks on older locks/failures).
Conclusion spans cover their emitting **job window**, not just conclusion-step
overhead. These windows overlap. Do not sum them into workflow duration or treat
`gh-aw.run.status`, `gh-aw.workflow_run.conclusion`, or a job's observed failure
as an authoritative final GitHub workflow status.

Actual grader ownership comes from `selectGraderOutputForJob` in
`actions/setup/js/send_otlp_span.cjs`: built-ins emit on
`gh-aw.conclusion.conclusion`; `inline` and `operational-value` results emit on
`gh-aw.graders.conclusion`. Downloaded built-in results in the graders job and
copies on agent spans are not additional results. Compact `grader.result` bodies
are lossy run summaries, not substitutes for these per-result span events.

The emitter sets each grader event's `timeUnixNano` to its conclusion emission
time. However, the documented Azure **`OTelEvents.TimeGenerated` is the parent
span's start**, and the native schema has no documented event-clock column.
Consequently `EventTime` is null with `ClockState=unavailable-native-event-clock`.
`ParentEndTime` is parent context, not a recovered event clock. Do not time-bucket
grader results by either field. `PartialEvidence` qualifies result ownership,
conflicts and dropped/sampled attributes separately from this clock limitation.
Status `unavailable`, missing values and missing pass/fail booleans remain
unavailable; values with different IDs/units are not averaged together.

Events join their **exact trace/span parent**, never trace alone. Results with
no observed parent are retained as `Ownership=unknown`, with blank run/workflow
identity, rather than assigned to whichever run happens to share a trace.
They are unassigned window diagnostics, not selected-run results. Known parents
outside the requested selection are excluded. Orphan events with a repository
filter require that repository on their resource. Orphan logs carry no repository
identity and remain unassigned diagnostics even under a repository/run filter;
never count them toward that selection.

Compact logs are optional and unnecessary for every span-based panel.
The current exporter deliberately strips run/repository/body identity and
correlates via the emitting conclusion job's **setup** span ID. The quality panel
joins that exact trace/span. A selected `gh-aw.conclusion.setup` without logs
returns `not-observed`: opt-out, failed export, retention, filtering, and a
zero-admission byte cap cannot be distinguished. If even that span is absent,
there is no export row to infer. Missing log tables should leave this optional
panel disabled, not block span queries.

Quality resource counters describe a whole compact export and repeat on every
record: the panel takes maxima, **not sums**. Record and untimed counts are
explicitly *physical* counts (including mirrored log copies); there is no emitted
log-record ID that would justify exact logical record deduplication. Included and
omitted counts are not fleet/session completeness denominators. Oversized lines
were not decoded; `scan_truncated=true` means records beyond the source-scan
boundary were not inspected, so `omitted_events` cannot count them. Even a
bounded export with no known omission is only `compact-projection`, because its
allowlist excludes event types and nested details. Missing quality attributes,
unparseable bodies, untimed records, truncation or missing correlation are
explicitly unknown/partial evidence. None feeds AIC, tokens or grader totals.

This pack's baseline is `fdfc5d5a0de`. Accounting missingness `212d6e8643c`
and partial rejection diagnostics `adc174687b7` are ancestors. Compact-clock
commit `0c46b32a1cad758d1d7cd37a3d5e2559d8862000` is **not** an ancestor.
The baseline exports `observedTimeUnixNano`, not log `timeUnixNano`, and does not
emit `gh-aw.session.untimed_events`. This pack therefore never treats native log
`TimeGenerated` as the original event clock. It recognizes only valid ISO
`Body.timestamp` values; numeric, absent and unsupported clocks are untimed
(no guessed epoch units). The optional future `untimed_events` resource field
is displayed when present, stays null when absent, and is not required for
correctness. No action upgrade or cherry-pick is required.

## Schema sources and validation limits

Native Azure schema references checked for this version:
[OTelSpans](https://learn.microsoft.com/en-us/azure/azure-monitor/reference/tables/otelspans),
[OTelEvents](https://learn.microsoft.com/en-us/azure/azure-monitor/reference/tables/otelevents),
[OTelLogs](https://learn.microsoft.com/en-us/azure/azure-monitor/reference/tables/otellogs)
(Microsoft Learn table revision 2026-07-27). In particular, spans use `Name`,
`DurationMs`, `EndTime`, `StatusCode`, `Attributes`, `ResourceAttributes`,
`ScopeName`, `TraceId`, `SpanId` and `ParentSpanId`; events use `Name`, `Attributes`,
`TraceId` and `SpanId`; logs expose **string** `Body` plus resource attributes
and trace/span correlation. These are native tables, not Application Insights
`AppRequests`, `AppDependencies` or `AppTraces`.

Emitted attributes/ownership are checked against the JavaScript span and compact
exporters, not invented from provider names. The Node built-in test runner needs
no new packages:

```bash
node --test queries/observed-executions/v1/query-pack.test.cjs
```

`make test-observed-executions` runs all versioned packs. It is included in
`make test-scripts` and in the final change-scoped `make agent-report-progress`
gate when this query surface changes.

`fixtures.cjs` builds deterministic Azure-shaped rows. `expected.cjs` contains
literal expected tables. Tests compare **every supported panel's exact reference
results**, exercise all/exact/latest attempt selection, duplicate/mirrored and
conflicting spans, multiple workflow refs and same-named jobs, zero/absent/partial
usage, downstream copies, orphan correlation, unknown clocks and partial compact
evidence. Additional tests check the static KQL/dashboard contracts.

**These tests execute `reference.cjs`, not KQL.** Static checks are not a parser,
query-plan validation, or provider conformance. No live Azure access or KQL
execution was used. Still unverified are actual workspace table availability and
query-tier/join support, ingestion's numeric attribute/bag representation,
structured OTLP log-body serialization into `Body`, and preservation of IDs,
times and quality fields through DCR transformations. In particular, a body not
mapped to a JSON object returns unknown body evidence rather than success.
Operator behavior, the composed queries, cross-workspace mirroring, and result
parity with these fixtures require a provider integration check before treating
this pack as production-validated. Query/export limits and provider partial
result diagnostics must be surfaced by the caller; a returned table alone
cannot establish full acceptance or complete evidence.

Incompatible selection, ownership, schema or result-shape changes belong in a
new major pack directory. v1 does not perform output-lifecycle tracking, richer
Value evidence, independent fleet inventory, deployment or workflow dispatch.
