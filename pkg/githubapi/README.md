# githubapi Package

> Provides go-gh REST client options with repository-standard defaults and an unauthenticated GitHub GET helper.

## Overview

The `githubapi` package produces an `api.ClientOptions` value pre-populated with the repository's default HTTP timeout (`constants.DefaultHTTPClientTimeout`) and centralizes public GitHub GET transport. Callers retain endpoint construction, fallback eligibility, operation timing, and response parsing.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ClientOptions` | `func(host, authToken string) api.ClientOptions` | Returns go-gh REST client options with the repository default timeout applied |
| `GetPublic` | `func(ctx context.Context, client *http.Client, endpointURL string) ([]byte, error)` | Fetches a public GitHub JSON response using the supplied client |

#### `GetPublic`

Sends a context-aware GET with `Accept: application/vnd.github+json` without adding authorization. The supplied client controls transport and timeouts. Returns the unmodified body for HTTP 200; other statuses return `*HTTPStatusError` with `StatusCode` and a whitespace-trimmed `Body`. Request and body-read errors pass through, and the response body is closed on both success and read or status errors.

#### `ClientOptions`

```go
func ClientOptions(host, authToken string) api.ClientOptions
```

Returns an `api.ClientOptions` struct suitable for passing to `api.NewRESTClient` or `api.NewGraphQLClient`.

| Parameter | Type | Description |
|-----------|------|-------------|
| `host` | `string` | GitHub hostname (e.g. `"github.com"` or a GHES host) |
| `authToken` | `string` | Personal access token or GitHub App token for authentication |

**Behavioral contract**:
- MUST set `Host` to the provided `host` value.
- MUST set `AuthToken` to the provided `authToken` value.
- MUST set `Timeout` to `constants.DefaultHTTPClientTimeout`.

## Usage Examples

```go
import (
    "github.com/cli/go-gh/v2/pkg/api"
    "github.com/github/gh-aw/pkg/githubapi"
)

opts := githubapi.ClientOptions("github.com", token)
client, err := api.NewRESTClient(opts)
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/constants` — supplies `DefaultHTTPClientTimeout`

**External**:
- `github.com/cli/go-gh/v2/pkg/api` — defines the `ClientOptions` struct

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
