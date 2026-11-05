# errorutil Package

The `errorutil` package provides shared helpers for classifying and inspecting errors returned by the GitHub API and `gh` CLI.

## Overview

This package currently exposes focused helpers for identifying common error categories used across `pkg/cli` and `pkg/parser`, including "not found" (`404`), "forbidden" (`403`), "gone" (`410`), rate-limit, and authentication/authorization responses.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `IsNotFoundError` | `func(err error) bool` | Returns `true` when `err` indicates a "not found" condition by matching case-insensitive `404` or `not found` text; returns `false` for `nil` and non-matching errors |
| `IsForbiddenError` | `func(err error) bool` | Returns `true` when `err` indicates an HTTP-style `403`/"forbidden" response by matching case-insensitive patterns like `HTTP 403` or `403 Forbidden`; returns `false` for `nil` and non-matching errors |
| `IsGoneError` | `func(err error) bool` | Returns `true` when `err` indicates an HTTP-style `410`/"gone" response by matching case-insensitive patterns like `HTTP 410` or `410 Gone`; returns `false` for `nil` and non-matching errors |
| `IsRateLimitError` | `func(output string) bool` | Returns `true` when `output` indicates GitHub API rate limiting by matching case-insensitive `rate limit exceeded` (including `API rate limit exceeded`) or `secondary rate limit` text |
| `IsAuthError` | `func(output string) bool` | Returns `true` when `output` indicates authentication or authorization failures by matching case-insensitive credential-specific markers including `GH_TOKEN`, `GITHUB_TOKEN`, `authentication`, `not logged into`, `unauthorized`, `permission denied`, or `SAML enforcement` |
| `IsInsufficientScopesError` | `func(err error) bool` | Returns `true` when `err` indicates a GitHub GraphQL request was rejected for missing OAuth/PAT scopes by matching the case-insensitive `INSUFFICIENT_SCOPES` literal; returns `false` for `nil` and non-matching errors |
| `IsAlreadyMergedError` | `func(err error) bool` | Returns `true` when `err` indicates a `gh pr merge` operation failed because the pull request was already merged, by matching the case-insensitive phrase `already merged` or the case-sensitive GraphQL state literal `MERGED`; returns `false` for `nil`, non-matching errors, and failure wording such as `could not be merged` |
| `IsNotFoundOutput` | `func(output string) bool` | Reports whether command output represents an HTTP 404 or “not found” response using case-insensitive matching. |

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/errorutil"

if errorutil.IsNotFoundError(err) {
    // Handle missing resource path
}

if errorutil.IsForbiddenError(err) {
    // Handle insufficient permissions
}

if errorutil.IsGoneError(err) {
    // Handle expired or deleted resource
}

if errorutil.IsRateLimitError(output) {
    // Back off and retry
}

if errorutil.IsAuthError(output) {
    // Surface credential guidance
}

if errorutil.IsInsufficientScopesError(err) {
    // Prompt for a token with additional scopes
}

if errorutil.IsAlreadyMergedError(err) {
    // Treat the merge as already complete
}
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — package-scoped logging used for error-classification diagnostics.

**External**:
- None beyond the Go standard library (`strings`).

## Design Notes

- `IsNotFoundError`, `IsForbiddenError`, and `IsGoneError` intentionally accept multiple message formats to cover errors produced by GitHub API responses, `gh` CLI output, and `go-gh` wrappers.
- `IsRateLimitError` and `IsAuthError` provide shared case-insensitive string classifiers for GitHub API and `gh` CLI output so callers avoid duplicating inline substring checks.
- `IsForbiddenError` and `IsGoneError` intentionally require HTTP-style status context so unrelated phrases like `forbidden character` or `gone away` are not misclassified.
- `IsInsufficientScopesError` and `IsAlreadyMergedError` centralize gh CLI error-text classification that would otherwise require inline `strings.Contains(err.Error(), ...)` checks at call sites, keeping the brittle substring matching in one reviewed location. Both are documented exceptions to the `errstringmatch` convention: the gh CLI exposes no structured type, status code, or state for these conditions, so text matching is the only available signal. Because the fragility is centralized here, a gh CLI wording change is fixed in one place and is covered by tests, including negative cases such as `could not be merged`.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
