import "@typespec/http";

using TypeSpec.Http;

@service(#{ title: "AW Safe-Output Authorization" })
@useAuth(BearerAuth)
@doc("The root route represents the complete deployment-configured HTTPS endpoint; no path is appended.")
namespace SafeOutputAuthorization {
  @minLength(1)
  scalar NonemptyString extends string;

  @pattern("^[a-f0-9]{64}$")
  scalar Digest extends string;

  @pattern("^[A-Za-z0-9][A-Za-z0-9_.:@/-]{0,255}$")
  scalar Code extends string;

  @pattern("^[1-9][0-9]{0,9}$")
  scalar Attempt extends string;

  @pattern("^[1-9][0-9]{0,39}$")
  scalar GitHubId extends string;

  @pattern("^[A-Za-z0-9][A-Za-z0-9_.:@/#-]{0,511}$")
  scalar Reference extends string;

  @pattern("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\\.[0-9]{3}Z$")
  scalar UtcTimestamp extends string;

  alias Version = "gh-aw.safe-output-authorization.v1";

  model Caller {
    @doc("Canonical HTTPS origin, with no path, query, or fragment.")
    host: NonemptyString;
    repository_id: GitHubId;
    workflow_ref: NonemptyString;
    @pattern("^(?:[a-f0-9]{40}|[a-f0-9]{64})$")
    workflow_sha: string;
    @pattern("^[a-f0-9]{64}(?::[a-f0-9]{64})?$")
    workflow_hash: string;
    run_id: GitHubId;
    run_attempt: Attempt;
  }

  model Output {
    type: Code;
    name: Code;
    executor: "manager" | "native" | "script" | "action" | "job" | "standalone" | "workflow" | "queue";
    implementation_digest: Digest;
  }

  model Invocation {
    version: Version;
    @format("uuid")
    request_id: string;
    invocation_id: Digest;
    caller: Caller;
    output: Output;
    batch: {
      digest: Digest;
      @minValue(0)
      record_index: safeint;
    };
    input_digest: Digest;
    scope: {
      binding: "invocation" | "resolved_effect";
      @maxItems(64)
      refs: Reference[];
      limits_digest?: Digest;
    };
    context: {
      policy_digest?: Digest;
      @maxItems(64)
      evidence_refs?: Reference[];
      claim_id?: Code;
      delivery_id?: Code;
    };
  }

  model DecisionRequest {
    version: Version;
    request_digest: Digest;
    invocation: Invocation;
    @doc("Required when a valid runner job ID is available; excluded from the invocation digest.")
    job_id?: NonemptyString;
  }

  model Decision {
    version: Version;
    decision: "allow" | "deny";
    request_digest: Digest;
    invocation_id: Digest;
    run_attempt: Attempt;
    policy_revision: Code;
    decision_id: Code;
    issued_at: UtcTimestamp;
    expires_at: UtcTimestamp;
  }

  @error
  @doc("Non-200 responses never authorize execution; the client discards their bodies.")
  model NotAuthorized {
    @body body?: unknown;
  }

  @post
  @route("/")
  op decide(
    @header contentType: "application/json",
    @header cacheControl: "no-store",
    @header traceparent?: string,
    @header baggage?: string,
    @body request: DecisionRequest,
  ): {
    @statusCode status: 200;
    @header contentType: "application/json";
    @body body: Decision;
  } | NotAuthorized;
}
