package workflow

// Formal predicates for specs/otel-observability-spec.md Sections 13-16:
// Outcome Evaluation, Local Mirrors and Artifacts, Security and Privacy, and
// Reliability and Failure Handling.
//
// No outcome-evaluation span emitter, mirror writer, or fan-out/retry
// orchestrator currently exists in pkg/workflow (the mirror writer and OTLP
// fan-out/retry behavior are implemented in JavaScript under
// actions/setup/js/send_otlp_span.cjs, and outcome-status taxonomy is
// implemented in pkg/cli, which pkg/workflow cannot import without creating
// an import cycle). Consistent with the precedent set for pending predicates
// P20/P21 in otel_observability_formal_test.go (see ADR-49809), each
// not-yet-implemented predicate below is expressed as a skipped test with an
// explicit pending rationale rather than a stub type validated against its
// own test-only oracle, to avoid a tautological test that always passes.
//
// TestFormal_MirrorPathIsStable is the sole predicate with a real production
// call site (pkg/constants) and is asserted directly.

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
)

// P4_MirrorPathStable
// The local OTLP mirror default path MUST be exactly /tmp/gh-aw/otel.jsonl.
// Specification reference: specs/otel-observability-spec.md §14.1.
func TestFormal_MirrorPathIsStable(t *testing.T) {
	assert.Equal(t, "/tmp/gh-aw/otel.jsonl", constants.TmpGhAwDirSlash+constants.OtelJsonlFilename.String())
}

// P1_OutcomeSeparateTrace
// An outcome-evaluation span emitted long after (e.g. >24h) the source
// workflow completed MUST NOT extend the original workflow trace.
// Specification reference: specs/otel-observability-spec.md §13.1.
//
// Pending: no outcome-evaluation span emitter exists in pkg/workflow yet.
func TestFormal_OutcomeEvaluationUsesSeparateTrace(t *testing.T) {
	t.Skip("pending: no production outcome-evaluation span emitter in pkg/workflow; " +
		"replace t.Skip with assertions against the real emitter once it lands")
}

// P2_OutcomeSourceCorrelation
// Each outcome-evaluation span SHOULD carry either a span link to the
// originating workflow root context, or the full attribute triple
// (gh-aw.adoption.source_run_id, gh-aw.adoption.source_workflow,
// gh-aw.adoption.repo) when a persisted span context is unavailable.
// Specification reference: specs/otel-observability-spec.md §13.2.
//
// Pending: no outcome-evaluation span emitter exists in pkg/workflow yet.
func TestFormal_OutcomeSourceCorrelation(t *testing.T) {
	t.Skip("pending: no production outcome-evaluation span emitter in pkg/workflow; " +
		"replace t.Skip with assertions against the real emitter once it lands")
}

// P3_OutcomeResultTaxonomy
// gh-aw.adoption.result MUST be restricted to the canonical outcome taxonomy
// (accepted, rejected, ignored, pending, lifecycle, lifecycle_close) defined
// in specs/safe-output-lifecycle.md.
// Specification reference: specs/otel-observability-spec.md §13.3.
//
// Pending: the outcome-status taxonomy is implemented as pkg/cli.OutcomeStatus
// (see pkg/cli/outcome_eval_formal_test.go), which pkg/workflow cannot import
// without an import cycle; no OTel span attribute emitter reads that taxonomy
// from pkg/workflow yet.
func TestFormal_OutcomeResultTaxonomy(t *testing.T) {
	t.Skip("pending: outcome taxonomy lives in pkg/cli.OutcomeStatus and no " +
		"pkg/workflow span emitter reads it yet; see pkg/cli/outcome_eval_formal_test.go " +
		"for the taxonomy invariant that already covers this predicate")
}

// P9_MetricDimensionBound
// URLs, item identifiers, and run IDs MUST NOT be used as metric dimensions.
// Specification reference: specs/otel-observability-spec.md §13.3, §15.4.
//
// Pending: no production metric-cardinality filter exists in pkg/workflow yet
// (tracked jointly with P20 TestFormal_MetricResourceCardinalityBound in
// otel_observability_formal_test.go).
func TestFormal_MetricDimensionCardinalityBound(t *testing.T) {
	t.Skip("pending: no production metric-cardinality filter in pkg/workflow; " +
		"replace t.Skip with assertions against the real filter once it lands")
}

// P5_MirrorWriteBeforeExport
// A mirror record MUST be written before any remote export attempt is
// assumed successful.
// Specification reference: specs/otel-observability-spec.md §14.3.
//
// Pending: the mirror writer is implemented in JavaScript
// (actions/setup/js/send_otlp_span.cjs), not in pkg/workflow.
func TestFormal_MirrorWriteOccursBeforeExportSuccess(t *testing.T) {
	t.Skip("pending: mirror writer lives in actions/setup/js/send_otlp_span.cjs, not pkg/workflow; " +
		"replace t.Skip with assertions against a real pkg/workflow mirror writer once it lands")
}

// P6_MirrorNotTruncatedOnFail
// A remote export failure MUST NOT delete or truncate previously written
// mirror records.
// Specification reference: specs/otel-observability-spec.md §14.3.
//
// Pending: the mirror writer is implemented in JavaScript, not in pkg/workflow.
func TestFormal_MirrorNotTruncatedOnExportFailure(t *testing.T) {
	t.Skip("pending: mirror writer lives in actions/setup/js/send_otlp_span.cjs, not pkg/workflow; " +
		"replace t.Skip with assertions against a real pkg/workflow mirror writer once it lands")
}

// P7_HeaderRedactionInMirror
// Exporter header values MUST be masked before any diagnostic, mirror, or
// artifact output, and raw credentials MUST NOT leak into redacted output.
// Specification reference: specs/otel-observability-spec.md §15.1.
//
// Pending: header redaction for the local mirror is implemented in
// JavaScript (actions/setup/js/send_otlp_span.cjs); the compile-time header
// normalization in pkg/workflow (normalizeOTLPHeadersForEndpoint, covered by
// TestFormal_SentryAuthHeaderRewrite) only rewrites the Sentry auth header
// name and does not implement the general redaction/masking contract.
func TestFormal_HeaderRedactionBeforeMirrorOrDiagnostic(t *testing.T) {
	t.Skip("pending: general header redaction for mirror/diagnostic output lives in " +
		"actions/setup/js/send_otlp_span.cjs, not pkg/workflow; replace t.Skip with assertions " +
		"against a real pkg/workflow redaction stage once it lands")
}

// P8_ContentDefaultNone
// Raw prompts, model responses, and other sensitive content MUST NOT be
// captured by default; full-content capture requires explicit opt-in.
// Specification reference: specs/otel-observability-spec.md §15.2.
//
// Pending: no content-capture configuration or default exists in pkg/workflow yet.
func TestFormal_ContentCaptureDefaultsToNone(t *testing.T) {
	t.Skip("pending: no production content-capture configuration in pkg/workflow; " +
		"replace t.Skip with assertions against the real configuration once it lands")
}

// P11_PartialFanOutIndependent
// For N configured endpoints, one endpoint's export failure MUST NOT
// suppress attempts or successes at sibling endpoints.
// Specification reference: specs/otel-observability-spec.md §16.3.
//
// Pending: fan-out export/retry orchestration is implemented in JavaScript
// (actions/setup/js/send_otlp_span.cjs), not in pkg/workflow. pkg/workflow
// only resolves the configured endpoint list at compile time
// (collectAllOTLPEndpoints, covered by TestFormal_FanOutPreservesDeclarationOrder).
func TestFormal_PartialFanOutFailureDoesNotSuppressOthers(t *testing.T) {
	t.Skip("pending: fan-out export/retry orchestration lives in " +
		"actions/setup/js/send_otlp_span.cjs, not pkg/workflow; replace t.Skip with assertions " +
		"against a real pkg/workflow orchestrator once it lands")
}

// INV1_RetryBounded
// Retry permission MUST be bounded by maximum attempts, maximum elapsed
// time, and short-circuited on permanent failure.
// Specification reference: specs/otel-observability-spec.md §16.2.
//
// Pending: retry/backoff orchestration is implemented in JavaScript
// (actions/setup/js/send_otlp_span.cjs), not in pkg/workflow.
func TestFormal_RetryIsBoundedByAttemptsAndElapsedTime(t *testing.T) {
	t.Skip("pending: retry/backoff orchestration lives in actions/setup/js/send_otlp_span.cjs, " +
		"not pkg/workflow; replace t.Skip with assertions against a real pkg/workflow retry " +
		"policy once it lands")
}

// P10_NonFatalExportFailure
// The reported workflow result MUST depend only on functional success, never
// on telemetry export outcome.
// Specification reference: specs/otel-observability-spec.md §16.1.
//
// Pending: no production code path in pkg/workflow couples workflow result
// to telemetry export outcome, but there is also no explicit guard/emitter to
// assert against yet.
func TestFormal_TelemetryFailureNeverFlipsFunctionalSuccess(t *testing.T) {
	t.Skip("pending: no production workflow-result/telemetry-outcome coupling point exists in " +
		"pkg/workflow to assert against; replace t.Skip with assertions once a concrete call site lands")
}

// SAFETY_FailClosedSecrets (edge case)
// A shutdown flush timeout MUST NOT delete previously written mirror records.
// Specification reference: specs/otel-observability-spec.md §16.5, Safeguards.
//
// Pending: shutdown/finalization ordering is implemented in JavaScript
// (actions/setup/js/send_otlp_span.cjs and actions/setup/post.js), not in pkg/workflow.
func TestFormal_SafeguardsShutdownDoesNotDeleteMirrorOnTimeout(t *testing.T) {
	t.Skip("pending: shutdown/finalization ordering lives in actions/setup/js/send_otlp_span.cjs " +
		"and actions/setup/post.js, not pkg/workflow; replace t.Skip with assertions against a real " +
		"pkg/workflow finalization path once it lands")
}
