import "@typespec/http";

using TypeSpec.Http;

@service(#{ title: "AW Webhook Gateway Continuation" })
@useAuth(BearerAuth)
@doc("The root route represents the complete deployment-configured getToken HTTPS endpoint. Provider ingress is not a shared wire API.")
namespace WebhookGatewayContinuation {
  @pattern("^[1-9][0-9]*$")
  scalar PositiveDecimal extends string;

  @minLength(1)
  scalar NonemptyString extends string;

  model ContinuationRequest {
    schema_version: 1;
    repository: NonemptyString;
    repository_id: PositiveDecimal;
    repository_owner_id: PositiveDecimal;
    actor_id: PositiveDecimal;
    workflow_path: NonemptyString;
    workflow_ref: NonemptyString;
    @pattern("^[a-f0-9]{40}$")
    workflow_sha: string;
    @pattern("^[a-f0-9]{64}$")
    workflow_hash: string;
    run_id: PositiveDecimal;
    run_attempt: PositiveDecimal;
    check_run_id: PositiveDecimal;
    @doc("Required when the runner provides a job ID.")
    job_id?: NonemptyString;
    ref: NonemptyString;
    event_name: "repository_dispatch";
    @pattern("^gh-aw-webhook-[a-f0-9]{24}$")
    event_type: string;
  }

  @doc("Echoes every request field unchanged, including job_id only when present in the request.")
  model ContinuationApproval {
    ...ContinuationRequest;
    allowed: true;
  }

  @error
  model RefreshUnavailable {
    @statusCode status: 503;
    @header retryAfter?: string;
    @body body?: unknown;
  }

  @error
  model ContinuationDenied {
    @statusCode status: 403 | 409;
    @body body?: unknown;
  }

  @error
  @doc("Other responses do not allow continuation.")
  model ContinuationRejected {
    @body body?: unknown;
  }

  @post
  @route("/")
  op getToken(
    @header contentType: "application/json",
    @header accept: "application/json",
    @header traceparent?: string,
    @header baggage?: string,
    @body request: ContinuationRequest,
  ): {
    @statusCode status: 200;
    @header contentType: "application/json";
    @body body: ContinuationApproval;
  } | ContinuationDenied | RefreshUnavailable | ContinuationRejected;
}
