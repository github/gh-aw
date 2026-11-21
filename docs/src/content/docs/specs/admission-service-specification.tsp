import "@typespec/http";

using TypeSpec.Http;

@service(#{ title: "AW Admission Service" })
@server("https://{authority}", "Configured admission authority origin", {
  authority: string = "admission.example.org";
})
@useAuth(BearerAuth)
namespace AdmissionService {
  @minLength(1)
  scalar NonemptyString extends string;

  @pattern("^[1-9][0-9]*$")
  scalar PositiveDecimal extends string;

  @pattern("^[a-f0-9]{64}:[a-f0-9]{64}$")
  scalar WorkflowHash extends string;

  @pattern("^[1-9][0-9]{0,19}$")
  scalar ArtifactId extends string;

  @minValue(1)
  @maxValue(2147483647)
  scalar CreditAllowance extends int32;

  @minValue(0)
  scalar EventCount extends safeint;

  @minValue(500)
  @maxValue(599)
  scalar ServerError extends int32;

  model Invocation {
    repo: NonemptyString;
    workflow_id: NonemptyString;
    workflow_hash: WorkflowHash;
    @doc("Exactly <run_id>-<run_attempt>:<workflow_id>.")
    workflow_call_id: NonemptyString;
    run_id: PositiveDecimal;
    run_attempt: PositiveDecimal;
    @doc("Required when the runner provides a job ID.")
    job_id?: NonemptyString;
    ref: NonemptyString;
    event_type: NonemptyString;
    actor: NonemptyString;
    triggering_actor: NonemptyString;
  }

  model RequestHeaders {
    @header contentType: "application/json";
    @header cacheControl: "no-store";
    @header traceparent: string;
    @header baggage?: string;
  }

  model Allowances {
    max_ai_credits: CreditAllowance;
    max_ai_credits_threat_detection?: CreditAllowance;
    max_ai_credits_evals?: CreditAllowance;
  }

  model Admitted {
    @statusCode status: 200;
    @header contentType: "application/json";
    @header cacheControl: "no-store";
    @body body: Allowances;
  }

  @error
  model AdmissionDenied {
    @statusCode status: 403;
    @header cacheControl: "no-store";
  }

  @error
  model AdmissionFailure {
    @statusCode status: 408 | 429 | ServerError;
    @header cacheControl: "no-store";
    @header retryAfter?: string;
  }

  model SessionCompaction {
    version: 1;
    included_events: EventCount;
    omitted_events: EventCount;
    oversized_lines: EventCount;
    scan_truncated: boolean;
    truncated: boolean;
  }

  model ConclusionNotification {
    ...Invocation;
    @doc("Compact profile v1 JSONL, encoded as a JSON string.")
    session: NonemptyString;
    session_compaction: SessionCompaction;
    @doc("Unique IDs, with available retry IDs preceding initial IDs.")
    @maxItems(2)
    usage_artifact_ids: ArtifactId[];
  }

  model ConclusionAccepted {
    @statusCode status: 202;
    @doc("The client discards any response body.")
    @body body?: unknown;
  }

  @error
  model ConclusionTransientFailure {
    @statusCode status: 408 | 429 | ServerError;
    @header retryAfter?: string;
    @body body?: unknown;
  }

  @error
  @doc("Other non-success statuses stop delivery without retry.")
  model ConclusionRejected {
    @body body?: unknown;
  }

  @post
  @route("/api/admission")
  op admit(...RequestHeaders, @body request: Invocation):
    Admitted | AdmissionDenied | AdmissionFailure;

  @post
  @route("/api/conclusion")
  op conclude(...RequestHeaders, @body request: ConclusionNotification):
    ConclusionAccepted | NoContentResponse | ConclusionTransientFailure | ConclusionRejected;
}
