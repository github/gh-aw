//go:build !integration

// spec_test.go — public API contract tests tied to README documentation.
// Implementation edge cases live in errors_test.go.

package errorutil_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-aw/pkg/errorutil"
)

// TestSpec_PublicAPI_IsNotFoundError validates the documented behavior of
// IsNotFoundError as described in the errorutil README.md.
//
// Specification:
//   - Returns true when err indicates a "not found" condition by matching
//     case-insensitive "404" or "not found" text.
//   - Returns false for nil and non-matching errors.
func TestSpec_PublicAPI_IsNotFoundError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "documented: nil returns false", err: nil, want: false},
		{name: "documented: numeric 404 match", err: errors.New("HTTP 404: Not Found"), want: true},
		{name: "documented: lowercase not found", err: errors.New("not found"), want: true},
		{name: "documented: case-insensitive uppercase NOT FOUND", err: errors.New("RESOURCE NOT FOUND"), want: true},
		{name: "documented: case-insensitive mixed Not Found", err: errors.New("Resource Not Found"), want: true},
		{name: "documented: wrapped not found", err: fmt.Errorf("ctx: %w", errors.New("not found")), want: true},
		{name: "documented: non-matching error returns false", err: errors.New("something else went wrong"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsNotFoundError(tt.err)
			assert.Equal(t, tt.want, got, "IsNotFoundError(%v) mismatch for: %s", tt.err, tt.name)
		})
	}
}

// TestSpec_PublicAPI_IsForbiddenError validates the documented behavior of
// IsForbiddenError as described in the errorutil README.md.
//
// Specification:
//   - Returns true when err indicates an HTTP-style 403/"forbidden" response
//     by matching case-insensitive patterns like "HTTP 403" or "403 Forbidden".
//   - Returns false for nil and non-matching errors.
//   - Design note: requires HTTP-style status context so unrelated phrases
//     like "forbidden character" are not misclassified.
func TestSpec_PublicAPI_IsForbiddenError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "documented: nil returns false", err: nil, want: false},
		{name: "documented: HTTP 403 pattern", err: errors.New("HTTP 403: Forbidden"), want: true},
		{name: "documented: 403 Forbidden pattern", err: errors.New("403 Forbidden"), want: true},
		{name: "documented: case-insensitive http 403", err: errors.New("http 403: forbidden"), want: true},
		{name: "documented: case-insensitive HTTP 403 FORBIDDEN", err: errors.New("HTTP 403: FORBIDDEN"), want: true},
		{name: "documented: wrapped HTTP 403", err: fmt.Errorf("api: %w", errors.New("HTTP 403: Forbidden")), want: true},
		{name: "documented design note: 'forbidden character' is not misclassified", err: errors.New("invalid forbidden character in query"), want: false},
		{name: "documented: non-matching error returns false", err: errors.New("some other failure"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsForbiddenError(tt.err)
			assert.Equal(t, tt.want, got, "IsForbiddenError(%v) mismatch for: %s", tt.err, tt.name)
		})
	}
}

// TestSpec_PublicAPI_IsGoneError validates the documented behavior of
// IsGoneError as described in the errorutil README.md.
//
// Specification:
//   - Returns true when err indicates an HTTP-style 410/"gone" response
//     by matching case-insensitive patterns like "HTTP 410" or "410 Gone".
//   - Returns false for nil and non-matching errors.
//   - Design note: requires HTTP-style status context so unrelated phrases
//     like "gone away" are not misclassified.
func TestSpec_PublicAPI_IsGoneError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "documented: nil returns false", err: nil, want: false},
		{name: "documented: HTTP 410 pattern", err: errors.New("HTTP 410: Gone"), want: true},
		{name: "documented: 410 Gone pattern", err: errors.New("410 Gone"), want: true},
		{name: "documented: case-insensitive http 410", err: errors.New("http 410: gone"), want: true},
		{name: "documented: case-insensitive HTTP 410 GONE", err: errors.New("HTTP 410: GONE"), want: true},
		{name: "documented: wrapped HTTP 410", err: fmt.Errorf("api: %w", errors.New("HTTP 410: Gone")), want: true},
		{name: "documented design note: 'gone away' is not misclassified", err: errors.New("connection has gone away"), want: false},
		{name: "documented: non-matching error returns false", err: errors.New("totally unrelated"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsGoneError(tt.err)
			assert.Equal(t, tt.want, got, "IsGoneError(%v) mismatch for: %s", tt.err, tt.name)
		})
	}
}

// TestSpec_PublicAPI_IsRateLimitError validates the documented behavior of
// IsRateLimitError as described in the errorutil README.md.
func TestSpec_PublicAPI_IsRateLimitError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "documented phrase api rate limit exceeded", output: "403: API rate limit exceeded", want: true},
		{name: "documented phrase rate limit exceeded", output: "rate limit exceeded for installation", want: true},
		{name: "documented phrase secondary rate limit", output: "secondary rate limit triggered", want: true},
		{name: "case-insensitive", output: "API RATE LIMIT EXCEEDED", want: true},
		{name: "non-matching output", output: "404: not found", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, errorutil.IsRateLimitError(tt.output))
		})
	}
}

// TestSpec_PublicAPI_IsAuthError validates the documented behavior of
// IsAuthError as described in the errorutil README.md.
func TestSpec_PublicAPI_IsAuthError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "GH_TOKEN reference", output: "GH_TOKEN is invalid or expired", want: true},
		{name: "GITHUB_TOKEN reference", output: "GITHUB_TOKEN: authentication failed", want: true},
		{name: "authentication marker", output: "authentication required", want: true},
		{name: "not logged into marker", output: "you are not logged into any GitHub hosts", want: true},
		{name: "unauthorized", output: "401: unauthorized", want: true},
		{name: "permission denied marker", output: "permission denied for this resource", want: true},
		{name: "forbidden is not inherently an auth failure", output: "403: forbidden", want: false},
		{name: "saml enforcement", output: "Resource protected by organization SAML enforcement", want: true},
		{name: "non-auth output", output: "404: not found", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, errorutil.IsAuthError(tt.output))
		})
	}
}

// TestSpec_PublicAPI_IsInsufficientScopesError validates the documented
// behavior of IsInsufficientScopesError as described in the errorutil
// README.md.
//
// Specification:
//   - Returns true when err indicates a GitHub GraphQL request was rejected
//     for missing OAuth/PAT scopes by matching the case-insensitive
//     "INSUFFICIENT_SCOPES" literal.
//   - Returns false for nil and non-matching errors.
func TestSpec_PublicAPI_IsInsufficientScopesError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "documented: nil returns false", err: nil, want: false},
		{name: "documented: INSUFFICIENT_SCOPES literal", err: errors.New("GraphQL: INSUFFICIENT_SCOPES"), want: true},
		{name: "documented: case-insensitive lowercase", err: errors.New("insufficient_scopes"), want: true},
		{name: "documented: wrapped INSUFFICIENT_SCOPES", err: fmt.Errorf("mutation failed: %w", errors.New("INSUFFICIENT_SCOPES")), want: true},
		{name: "documented: non-matching error returns false", err: errors.New("something else went wrong"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsInsufficientScopesError(tt.err)
			assert.Equal(t, tt.want, got, "IsInsufficientScopesError(%v) mismatch for: %s", tt.err, tt.name)
		})
	}
}

// TestSpec_PublicAPI_IsAlreadyMergedError validates the documented behavior
// of IsAlreadyMergedError as described in the errorutil README.md.
//
// Specification:
//   - Returns true when err indicates a `gh pr merge` operation failed
//     because the pull request was already merged, by matching the
//     case-insensitive phrase "already merged" or the case-sensitive
//     GraphQL state literal "MERGED".
//   - Returns false for nil, non-matching errors, and failure wording such
//     as "could not be merged".
func TestSpec_PublicAPI_IsAlreadyMergedError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "documented: nil returns false", err: nil, want: false},
		{name: "documented: already merged phrase", err: errors.New("Pull request is already merged"), want: true},
		{name: "documented: MERGED state literal", err: errors.New("state is MERGED"), want: true},
		{name: "documented: non-matching error returns false", err: errors.New("network timeout"), want: false},
		{name: "documented: failed merge wording returns false", err: errors.New("Pull request could not be merged"), want: false},
		{name: "documented: not merged wording returns false", err: errors.New("pull request is not merged"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsAlreadyMergedError(tt.err)
			assert.Equal(t, tt.want, got, "IsAlreadyMergedError(%v) mismatch for: %s", tt.err, tt.name)
		})
	}
}

// TestSpec_UsageExample_ErrorClassifiers validates that the documented usage
// example pattern compiles and runs.
//
// Specification (Usage Examples):
//
//	if errorutil.IsNotFoundError(err) { ... }
//	if errorutil.IsForbiddenError(err) { ... }
//	if errorutil.IsGoneError(err) { ... }
//	if errorutil.IsRateLimitError(output) { ... }
//	if errorutil.IsAuthError(output) { ... }
func TestSpec_UsageExample_ErrorClassifiers(t *testing.T) {
	t.Parallel()
	notFound := errors.New("HTTP 404: Not Found")
	forbidden := errors.New("HTTP 403: Forbidden")
	gone := errors.New("HTTP 410: Gone")
	rateLimit := "API rate limit exceeded"
	authOutput := "GH_TOKEN is missing"

	assert.True(t, errorutil.IsNotFoundError(notFound), "usage example: 404 path triggered")
	assert.True(t, errorutil.IsForbiddenError(forbidden), "usage example: 403 path triggered")
	assert.True(t, errorutil.IsGoneError(gone), "usage example: 410 path triggered")
	assert.True(t, errorutil.IsRateLimitError(rateLimit), "usage example: rate-limit path triggered")
	assert.True(t, errorutil.IsAuthError(authOutput), "usage example: auth path triggered")

	assert.False(t, errorutil.IsForbiddenError(notFound), "documented: classifiers are exclusive — 404 is not forbidden")
	assert.False(t, errorutil.IsGoneError(notFound), "documented: classifiers are exclusive — 404 is not gone")
	assert.False(t, errorutil.IsNotFoundError(forbidden), "documented: classifiers are exclusive — 403 is not not-found")
}
