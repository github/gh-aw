# ADR-71893: Centralize Public GitHub API GET Transport in githubapi Package

**Date**: 2026-05-14
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent

---

### Context

When authenticated `gh` calls are unavailable, gh-aw falls back to the unauthenticated public GitHub REST API in three places: `fetchPublicGitHubAPI` in `pkg/cli/update_workflows.go`, `fetchPublicGitHubContentsAPI` in `pkg/parser/remote_client.go`, and `resolveRefToSHAViaPublicAPI` in `pkg/parser/remote_resolve_sha.go`. Each one built a context-aware GET request, set the `Accept: application/vnd.github+json` header, read and closed the body, and checked for HTTP 200 on its own. Because the code was copied three times, response handling, cancellation, and body closing could drift apart between callers (tracked as a duplicate-code finding in #71713). Each caller also has its own needs: error message wording, debug logging, operation timing via `ctxutil.StartOperation`, URL escaping, and its own `*http.Client` with a specific timeout.

### Decision

We will add one shared helper, `githubapi.GetPublic(ctx, client, endpointURL) ([]byte, error)`, in `pkg/githubapi`. It owns only the transport mechanics: building the request with the context, setting the GitHub JSON Accept header, reading and always closing the body, and checking for HTTP 200. Non-200 responses come back as a typed `*githubapi.HTTPStatusError` with `StatusCode` and a trimmed `Body`. Callers can then use `errors.As` to keep their existing error messages and logging. Endpoint construction, URL escaping, fallback eligibility, client and timeout choice, operation timing, and response parsing stay in the callers.

### Alternatives Considered

#### Alternative 1: Keep the per-caller implementations

Leave the three hand-written request/read/status blocks as they are. This needs no new API and each caller stays self-contained. It was rejected because the duplication is the problem the PR addresses: a fix to cancellation or body-closing behavior would have to be applied in three places, and the copies had already started to differ in logging and error wording.

#### Alternative 2: A full public-API client abstraction that owns the base URL, timeouts, and parsing

Build a richer client type, for example with a fixed `https://api.github.com` base, a built-in timeout, and JSON decoding into typed results. This would remove more code from callers. It was rejected because the callers differ in URL escaping (`url.PathEscape` vs per-segment encoding plus `url.QueryEscape`), timeouts (separate injected `*http.Client` values, which tests replace), timing labels, and how they parse responses. A bigger abstraction would need many options or would change behavior that callers and tests depend on.

#### Alternative 3: Return a formatted `error` string instead of a typed error

`GetPublic` could keep returning `fmt.Errorf("GitHub API returned %d: %s", ...)`. That is simpler, but `resolveRefToSHAViaPublicAPI` uses a different message format and `fetchPublicGitHubContentsAPI` logs the status code. Both would have to parse strings to stay compatible, so a typed `HTTPStatusError` was chosen.

### Consequences

#### Positive
- One implementation of the unauthenticated GET mechanics, so cancellation, header, body-close, and status-check behavior can't drift between the CLI and the parser.
- The typed `HTTPStatusError` lets callers branch on the status code (`errors.As`) without parsing strings, and existing user-facing messages are kept.
- The helper takes an injected `*http.Client`, so callers can still swap clients in tests, and it is covered by its own unit tests (`public_get_test.go`).
- Response body close errors are now logged in one place through the `githubapi:public_get` debug namespace.

#### Negative
- `pkg/githubapi` grows beyond its previous single job (go-gh client options) and now also handles raw `net/http` transport, which widens the package's scope.
- `pkg/cli` and `pkg/parser` now depend on `pkg/githubapi` for this path, which adds coupling between packages.
- The helper always reads the whole body into memory and only accepts HTTP 200. A future caller that needs streaming, other 2xx codes, or authentication will have to extend it or skip it.

#### Neutral
- Callers still decide whether to fall back and still own operation timing (`ctxutil.StartOperation`), so the public-API fallback policy is not centralized by this change.
- The default `HTTPStatusError.Error()` text matches the old `fetchPublicGitHubAPI` message format (`GitHub API returned %d: %s`), so existing error output stays the same for that caller.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
