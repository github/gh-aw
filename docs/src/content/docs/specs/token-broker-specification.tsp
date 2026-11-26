import "@typespec/http";

using TypeSpec.Http;

@service(#{ title: "AW Token Broker" })
@doc("The root route represents the complete deployment-configured HTTPS endpoint; no path is appended.")
namespace TokenBroker {
  @minLength(1)
  scalar NonemptyString extends string;

  @pattern("^[!-~]+$")
  scalar Credential extends string;

  @doc("RFC 6750 Section 2.1 b64token grammar for OTLP only.")
  @pattern("^[A-Za-z0-9._~+/-]+={0,}$")
  scalar OtlpCredential extends string;

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

  @doc("Proposed independent OTLP exchange selected by observability.otlp.token-broker; provider, endpoint, and caller headers are forbidden.")
  model OtlpRequest {
    ...RequestIdentity;
    token_type: "otlp";
  }

  model InvalidateRequest {
    ...RequestIdentity;
    operation: "invalidate";
  }

  model IssuedCredential {
    token: Credential;
    expires_at: Expiration;
  }

  @doc("An approved absolute HTTPS OTLP/HTTP base URL. Format/pattern checks are partial; URL parsing, userinfo rejection, destination policy, and SSRF checks are normative in Section 3.1.")
  @format("uri")
  @pattern("^[Hh][Tt][Tt][Pp][Ss]://[^/@\\s\\x00-\\x1F\\x7F\\\\#]+(?:[/?][^\\s\\x00-\\x1F\\x7F\\\\#]*)?$")
  scalar CollectorEndpoint extends string;

  @doc("Only an otlp exchange returns this exact three-member body; endpoint/token/expiry are one job-local pair. The token uses OtlpCredential, and expiration covers the trusted job completion bound plus the safety margin in Section 6.4.")
  model IssuedOtlpCredential {
    endpoint: CollectorEndpoint;
    token: OtlpCredential;
    expires_at: Expiration;
  }

  model ResponseHeaders {
    @header cacheControl: "no-store";
  }

  model Exchanged {
    ...ResponseHeaders;
    @statusCode status: 200;
    @header contentType: "application/json";
    @doc("The schema union cannot encode the request-dependent response shape. A conforming client MUST select the response model using the request token_type and reject any member set that does not match Section 4.3; T-TBR-021 verifies this for both mismatch directions.")
    @body body: IssuedCredential | IssuedOtlpCredential;
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
  @doc("An exchange request succeeds with 200; an invalidation request succeeds with 204. OTLP is proposed, not implemented. Request-dependent response validation follows Section 4.3.")
  op submit(
    @header contentType: "application/json",
    @header accept: "application/json",
    @header traceparent?: string,
    @header baggage?: string,
    @body request: CopilotRequest | TaskOrAppRequest | OtlpRequest | InvalidateRequest,
  ): Exchanged | Invalidated | Failure;
}
