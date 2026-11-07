//go:build !integration

// errors_test.go — behavioral edge-case and boundary tests.
// Spec-contract tests (documented public API) live in spec_test.go.

package errorutil_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-aw/pkg/errorutil"
)

type testCloser struct {
	err error
}

func (c testCloser) Close() error {
	return c.err
}

func TestCloseWithError(t *testing.T) {
	originalErr := errors.New("read failed")
	closeErr := errors.New("close failed")
	err := originalErr

	errorutil.CloseWithError(testCloser{err: closeErr}, &err)

	assert.ErrorIs(t, err, originalErr)
	assert.ErrorIs(t, err, closeErr)
}

func TestIsNotFoundError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "404 numeric literal", err: errors.New("HTTP 404: Not Found"), want: true},
		{name: "lowercase not found", err: errors.New("failed to fetch file: not found"), want: true},
		{name: "uppercase NOT FOUND", err: errors.New("RESOURCE NOT FOUND"), want: true},
		{name: "wrapped lowercase not found", err: fmt.Errorf("request failed: %w", errors.New("not found")), want: true},
		{name: "bare 404 in message", err: errors.New("server returned 404"), want: true},
		{name: "exact 404 boundary", err: errors.New("404"), want: true},
		{name: "partial prefix 404abc", err: errors.New("404abc"), want: true},
		{name: "double-wrapped not found", err: fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", errors.New("not found"))), want: true},
		{name: "Could not resolve (DNS)", err: errors.New("Could not resolve host"), want: false},
		{name: "401 Unauthorized", err: errors.New("HTTP 401: Unauthorized"), want: false},
		{name: "500 Internal Server Error", err: errors.New("HTTP 500: Internal Server Error"), want: false},
		{name: "generic error", err: errors.New("something went wrong"), want: false},
		{name: "410 Gone", err: errors.New("HTTP 410: Gone"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsNotFoundError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsNotFoundOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "empty output", output: "", want: false},
		{name: "404 numeric literal", output: "HTTP 404: Not Found", want: true},
		{name: "title case not found", output: "GraphQL: Not Found", want: true},
		{name: "uppercase not found", output: "RESOURCE NOT FOUND", want: true},
		{name: "generic output", output: "something went wrong", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, errorutil.IsNotFoundOutput(tt.output))
		})
	}
}

func TestIsForbiddenError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "http 403 forbidden", err: errors.New("HTTP 403: Forbidden"), want: true},
		{name: "parenthesized http 403", err: errors.New("gh: API rate limit exceeded (HTTP 403)"), want: true},
		{name: "status 403", err: errors.New("request failed with status 403"), want: true},
		{name: "wrapped http 403", err: fmt.Errorf("request failed: %w", errors.New("HTTP 403: access denied")), want: true},
		{name: "double-wrapped http 403", err: fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", errors.New("HTTP 403: access denied"))), want: true},
		{name: "forbidden without http status", err: errors.New("request forbidden"), want: false},
		{name: "forbidden character", err: errors.New("invalid forbidden character in query"), want: false},
		{name: "bare 403 in message", err: errors.New("server returned 403"), want: false},
		{name: "404 Not Found", err: errors.New("HTTP 404: Not Found"), want: false},
		{name: "generic error", err: errors.New("something went wrong"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsForbiddenError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsGoneError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "http 410 gone", err: errors.New("HTTP 410: Gone"), want: true},
		{name: "parenthesized http 410", err: errors.New("gh: workflow logs expired (HTTP 410)"), want: true},
		{name: "status 410", err: errors.New("request failed with status 410"), want: true},
		{name: "wrapped http 410", err: fmt.Errorf("request failed: %w", errors.New("HTTP 410: logs unavailable")), want: true},
		{name: "double-wrapped http 410", err: fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", errors.New("HTTP 410: logs unavailable"))), want: true},
		{name: "gone without http status", err: errors.New("artifact gone"), want: false},
		{name: "gone away", err: errors.New("connection has gone away"), want: false},
		{name: "bare 410 in message", err: errors.New("server returned 410"), want: false},
		{name: "403 Forbidden", err: errors.New("HTTP 403: Forbidden"), want: false},
		{name: "generic error", err: errors.New("something went wrong"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsGoneError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsRateLimitError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "api rate limit exceeded", output: "API rate limit exceeded", want: true},
		{name: "rate limit exceeded", output: "rate limit exceeded", want: true},
		{name: "secondary rate limit", output: "secondary rate limit triggered", want: true},
		{name: "case-insensitive", output: "API RATE LIMIT EXCEEDED", want: true},
		{name: "non-rate-limit error", output: "HTTP 404: Not Found", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, errorutil.IsRateLimitError(tt.output))
		})
	}
}

func TestIsAuthError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "gh_token", output: "GH_TOKEN is not set", want: true},
		{name: "github_token", output: "GITHUB_TOKEN is invalid", want: true},
		{name: "authentication", output: "authentication required", want: true},
		{name: "not logged into", output: "not logged into any GitHub hosts", want: true},
		{name: "unauthorized", output: "HTTP 401: Unauthorized", want: true},
		{name: "forbidden is not inherently an auth failure", output: "HTTP 403: Forbidden", want: false},
		{name: "permission denied", output: "permission denied: insufficient scope", want: true},
		{name: "saml enforcement", output: "Resource protected by organization SAML enforcement", want: true},
		{name: "non-auth error", output: "API rate limit exceeded for installation", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, errorutil.IsAuthError(tt.output))
		})
	}
}

func TestIsInsufficientScopesError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "graphql insufficient scopes", err: errors.New("GraphQL: Your token has not been granted the required scopes (INSUFFICIENT_SCOPES)"), want: true},
		{name: "case-insensitive lowercase", err: errors.New("insufficient_scopes"), want: true},
		{name: "wrapped insufficient scopes", err: fmt.Errorf("mutation failed: %w", errors.New("INSUFFICIENT_SCOPES")), want: true},
		{name: "unrelated error", err: errors.New("something went wrong"), want: false},
		{name: "generic forbidden", err: errors.New("HTTP 403: Forbidden"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsInsufficientScopesError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsAlreadyMergedError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "already merged phrase", err: errors.New("GraphQL: Pull request is already merged (mergePullRequest)"), want: true},
		{name: "MERGED state literal", err: errors.New("pull request state is MERGED"), want: true},
		{name: "case-insensitive merged", err: errors.New("pull request already merged"), want: true},
		{name: "wrapped already merged", err: fmt.Errorf("merge failed: %w", errors.New("already merged")), want: true},
		{name: "unrelated error", err: errors.New("network timeout"), want: false},
		{name: "could not be merged", err: errors.New("GraphQL: Pull request could not be merged (mergePullRequest)"), want: false},
		{name: "not merged", err: errors.New("pull request is not merged"), want: false},
		{name: "lowercase merged word", err: errors.New("failed to merge: branch was merged upstream"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := errorutil.IsAlreadyMergedError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}
