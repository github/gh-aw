# GitHub API Rate Limit Observability

> Added in PR #24694 (2026-04-05)

## Overview

GitHub API rate limits are a failure mode in high-volume workflows with no post-run visibility.
This feature logs rate-limit data to a JSONL artifact during workflow execution and enriches OTLP
conclusion spans with rate-limit attributes.

## Log File

Each job writes rate-limit entries to `/tmp/gh-aw/github_rate_limits.jsonl`.

**Entry format** (one JSON object per line):

```json
{
  "timestamp": "2026-04-05T08:30:00.000Z",
  "source": "response_headers",
  "operation": "issues.listComments",
  "resource": "core",
  "limit": 5000,
  "remaining": 4823,
  "used": 177,
  "reset": "2026-04-05T09:00:00.000Z"
}
```

**`source` values**:
- `"response_headers"` — extracted from `x-ratelimit-*` response headers after a REST call
- `"rate_limit_api"` — fetched via the GitHub rate-limit API (multi-resource snapshot)

**Constant**: `constants.GithubRateLimitsFilename = "github_rate_limits.jsonl"` (Go); `GITHUB_RATE_LIMITS_JSONL_PATH` (JS).

## JS Helper: `github_rate_limit_logger.cjs`

Located at `actions/setup/js/github_rate_limit_logger.cjs`. Provides three usage patterns:

### 1. Per-call logging (from response headers)

```js
logRateLimitFromResponse(response, "issues.listComments");
```

Reads `x-ratelimit-*` headers from the REST response. No additional API call. Skips silently when headers are absent (e.g., GraphQL responses).

### 2. On-demand snapshot (rate-limit API)

```js
await fetchAndLogRateLimit(github, "startup");
```

Calls `github.rest.rateLimit.get()` and writes one entry per resource category (core, search, graphql, etc.). Use at job start/end to observe all rate limit categories.

### 3. Automatic wrapping (no call-site changes required)

```js
const gh = createRateLimitAwareGithub(github);
await gh.rest.issues.get({ owner, repo, issue_number: 1 });
// Every gh.rest.*.*() call now auto-logs rate-limit headers
```

Creates a `Proxy` around the GitHub REST client. Every `github.rest.*.*()` invocation
automatically logs rate-limit headers from the response without modifying call sites.

## Integration via `setup_globals.cjs`

`setupGlobals` wraps the injected `github` object with `createRateLimitAwareGithub` automatically.
All scripts that use `global.github` get rate-limit logging for `github.rest.*.*()` calls without
per-file changes.

`check_rate_limit.cjs` retains a `fetchAndLogRateLimit` call at startup for a full multi-resource
snapshot via the rate-limit API.

## Artifact Upload

`github_rate_limits.jsonl` is included in artifact uploads for both activation and agent jobs.

Relevant compiler files:
- `pkg/workflow/compiler_activation_job.go`
- `pkg/workflow/compiler_yaml_main_job.go`

The upload step uses `if-no-files-found: ignore`, so it is a no-op when no API calls were made.
The artifact has a 1-day retention period.

## OTLP Span Enrichment

`sendJobConclusionSpan` in `send_otlp_span.cjs` reads the **last entry** from
`github_rate_limits.jsonl` and includes rate-limit attributes in the OTLP conclusion span:

| Attribute | Description |
|-----------|-------------|
| `gh-aw.github.rate_limit.remaining` | Remaining API calls at job end |
| `gh-aw.github.rate_limit.limit` | Total API call quota |
| `gh-aw.github.rate_limit.used` | API calls consumed |
| `gh-aw.github.rate_limit.resource` | Resource category (e.g. `core`) |
| `gh-aw.github.rate_limit.reset` | ISO 8601 timestamp when the rate-limit window resets |

This makes rate-limit headroom at job conclusion time visible in any connected OTLP collector
or tracing UI without requiring artifact inspection.

## Debugging Rate Limit Issues

To inspect rate limit consumption after a workflow run:

1. Download the job artifact.
2. Open `github_rate_limits.jsonl`.
3. Parse entries with `jq`:

```bash
# Show all entries sorted by timestamp
jq -s 'sort_by(.timestamp)' github_rate_limits.jsonl

# Show only low-remaining entries (< 500 calls left)
jq 'select(.remaining < 500)' github_rate_limits.jsonl

# Show final snapshot
jq -s 'last' github_rate_limits.jsonl
```
