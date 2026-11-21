import "@typespec/http";

using TypeSpec.Http;

@service(#{ title: "AW Experiments Broker" })
@doc("The root route represents the complete deployment-configured HTTPS endpoint; no path is appended.")
namespace ExperimentsBroker {
  @minLength(1)
  scalar NonemptyString extends string;

  @pattern("^[A-Za-z0-9_-]{1,128}$")
  scalar InvocationId extends string;

  @minValue(0)
  @maxValue(9007199254740991)
  scalar Counter extends safeint;

  @minValue(0)
  @maxValue(9007199254740991)
  scalar Observation extends numeric;

  @minItems(2)
  model Variants is Array<NonemptyString>;

  @minValue(500)
  @maxValue(599)
  scalar ServerError extends int32;

  @doc("Retains all experiment metadata. Validate metadata according to the Experiments Specification.")
  model ExperimentConfiguration is Record<unknown> {
    variants: Variants;
  }

  alias Assignments = Record<string>;

  model RequestIdentity {
    oidc_token: NonemptyString;
    job_id: NonemptyString;
    workflow_id: NonemptyString;
    workflow_ref: NonemptyString;
    invocation_id: InvocationId;
    repository: NonemptyString;
    server_url: NonemptyString;
    run_id: NonemptyString;
    run_attempt: NonemptyString;
  }

  model AssignRequest {
    ...RequestIdentity;
    operation: "assign";
    @doc("Complete experiment map, excluding the reserved storage selector.")
    experiments: Record<Variants | ExperimentConfiguration>;
  }

  model ReportRequest {
    ...RequestIdentity;
    operation: "report";
    assignments: Assignments;
    results: Results;
  }

  model Results {
    agent_result: "success" | "failure" | "cancelled" | "skipped";
    firewall?: {
      total_requests?: Counter;
      allowed_requests?: Counter;
      blocked_requests?: Counter;
    };
    session?: {
      total_events?: Counter;
      session_starts?: Counter;
      session_shutdowns?: Counter;
      turns?: Counter;
      assistant_messages?: Counter;
      reasoning_events?: Counter;
      tool_execution_starts?: Counter;
      tool_execution_completes?: Counter;
      failed_tool_executions?: Counter;
    };
    gateway?: {
      total_calls?: Counter;
      failed_calls?: Counter;
      total_input_size?: Counter;
      total_output_size?: Counter;
      max_input_size?: Counter;
      max_output_size?: Counter;
      total_duration_ms?: Observation;
      max_duration_ms?: Observation;
    };
    integrity?: { total_filtered?: Counter };
    safe_outputs?: { total_items?: Counter };
    ledger?: { transactions_added?: Counter };
    friction?: Friction;
  }

  alias MeasurementState = "measured" | "causal" | "statistical" | "unavailable";
  alias DimensionState = MeasurementState | "unsupported";

  model Friction {
    counted_occurrences?: Counter;
    total_run_aic?: Observation;
    friction_ratio?: Observation;
    measurement_state?: MeasurementState;
    dimension_states?: {
      aic?: DimensionState;
      tokens?: DimensionState;
      turns?: DimensionState;
      tool_calls?: DimensionState;
      latency_ms?: DimensionState;
    };
    cost?: {
      aic?: Observation;
      turns?: Observation;
      tool_calls?: Observation;
      latency_ms?: Observation;
      tokens?: {
        input?: Counter;
        output?: Counter;
        cache_read?: Counter;
        cache_write?: Counter;
        reasoning?: Counter;
        total?: Counter;
      };
    };
  }

  model Assignment {
    assignments: Assignments;
    counts: Record<Record<Counter>>;
  }

  model ResponseHeaders {
    @header cacheControl: "no-store";
  }

  model Assigned {
    ...ResponseHeaders;
    @statusCode status: 200;
    @header contentType: "application/json";
    @body body: Assignment;
  }

  model Reported {
    ...ResponseHeaders;
    @statusCode status: 204;
  }

  @error
  model Failure {
    ...ResponseHeaders;
    @statusCode status: 400 | 401 | 403 | 409 | 413 | 415 | 429 | ServerError;
    @doc("No shared diagnostic schema; any error body is generic and sanitized.")
    @body body?: unknown;
  }

  @post
  @route("/")
  @doc("An assign request succeeds with 200; a report request succeeds with 204.")
  op submit(
    @header contentType: "application/json",
    @header accept: "application/json",
    @body request: AssignRequest | ReportRequest,
  ): Assigned | Reported | Failure;
}
