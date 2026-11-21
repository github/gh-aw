import "@typespec/http";

using TypeSpec.Http;

@service(#{ title: "AW Token Broker" })
@doc("The root route represents the complete deployment-configured HTTPS endpoint; no path is appended.")
namespace TokenBroker {
  @minLength(1)
  scalar NonemptyString extends string;

  @pattern("^[!-~]+$")
  scalar Credential extends string;

  @doc("Restricted RFC 3339 profile: valid Gregorian date, no leap seconds or hour 24.")
  @pattern("^[0-9]{4}-[0-9]{2}-[0-9]{2}[Tt](?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\\.[0-9]+)?(?:[Zz]|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])$")
  scalar Expiration extends string;

  model RequestIdentity {
    oidc_token: NonemptyString;
    @doc("Required when the runner provides a job ID.")
    job_id?: NonemptyString;
  }

  model CopilotRequest {
    ...RequestIdentity;
    token_type: "copilot-request";
    provider: NonemptyString;
  }

  model TaskOrAppRequest {
    ...RequestIdentity;
    token_type: "copilot-task" | "github-app";
  }

  model InvalidateRequest {
    ...RequestIdentity;
    operation: "invalidate";
  }

  model IssuedCredential {
    token: Credential;
    expires_at: Expiration;
  }

  model ResponseHeaders {
    @header cacheControl: "no-store";
  }

  model Exchanged {
    ...ResponseHeaders;
    @statusCode status: 200;
    @header contentType: "application/json";
    @body body: IssuedCredential;
  }

  model Invalidated {
    ...ResponseHeaders;
    @statusCode status: 204;
  }

  @error
  model Failure {
    ...ResponseHeaders;
    @statusCode status: 400 | 401 | 403 | 404 | 413 | 415 | 422 | 429 | 500 | 501 | 502 | 503 | 504;
    @doc("Empty bodies are recommended; any diagnostic must be fixed and sanitized.")
    @body body?: unknown;
  }

  @post
  @route("/")
  @doc("An exchange request succeeds with 200; an invalidation request succeeds with 204.")
  op submit(
    @header contentType: "application/json",
    @header accept: "application/json",
    @header traceparent?: string,
    @header baggage?: string,
    @body request: CopilotRequest | TaskOrAppRequest | InvalidateRequest,
  ): Exchanged | Invalidated | Failure;
}
