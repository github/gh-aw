// Package errorutil provides shared helpers for classifying and inspecting errors
// returned by the GitHub API and gh CLI.
package errorutil

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var errorutilLog = logger.New("errorutil:errors")

// CloseWithError closes closer and joins any close failure into err.
func CloseWithError(closer io.Closer, err *error) {
	if closeErr := closer.Close(); closeErr != nil {
		*err = errors.Join(*err, fmt.Errorf("failed to close resource: %w", closeErr))
	}
}

// IsNotFoundError reports whether err represents an HTTP 404 / "not found" response.
// It returns false when err is nil.
// The check is case-insensitive and matches both the numeric literal "404" and
// the phrase "not found", which covers all known forms returned by the GitHub API,
// the gh CLI, and the go-gh library.
func IsNotFoundError(err error) bool {
	matched := containsErrorSubstring(err, "404", "not found")
	if matched {
		errorutilLog.Printf("Classified error as not-found (404): %v", err)
	}
	return matched
}

// IsNotFoundOutput reports whether output represents an HTTP 404 / "not found" response.
// The check is case-insensitive and matches both the numeric literal "404" and
// the phrase "not found".
func IsNotFoundOutput(output string) bool {
	matched := containsSubstring(output, "404", "not found")
	if matched {
		errorutilLog.Printf("Classified output as not-found (404): %s", output)
	}
	return matched
}

// IsForbiddenError reports whether err represents an HTTP 403 / "forbidden" response.
// It returns false when err is nil.
// The check is case-insensitive and only matches HTTP-style 403 patterns such as
// "HTTP 403" or "403 Forbidden", which avoids misclassifying unrelated errors
// like "forbidden character".
func IsForbiddenError(err error) bool {
	matched := containsHTTPStatusSubstring(err, "403", "forbidden")
	if matched {
		errorutilLog.Printf("Classified error as forbidden (403): %v", err)
	}
	return matched
}

// IsGoneError reports whether err represents an HTTP 410 / "gone" response.
// It returns false when err is nil.
// The check is case-insensitive and only matches HTTP-style 410 patterns such as
// "HTTP 410" or "410 Gone", which avoids misclassifying unrelated errors like
// "connection has gone away".
func IsGoneError(err error) bool {
	matched := containsHTTPStatusSubstring(err, "410", "gone")
	if matched {
		errorutilLog.Printf("Classified error as gone (410): %v", err)
	}
	return matched
}

// IsRateLimitError reports whether output indicates a GitHub API rate-limit error.
// The check is case-insensitive and matches known API phrases.
func IsRateLimitError(output string) bool {
	matched := containsSubstring(output,
		"rate limit exceeded",
		"secondary rate limit",
	)
	if matched {
		errorutilLog.Printf("Classified output as rate-limit related (len=%d)", len(output))
	}
	return matched
}

// IsAuthError reports whether output indicates an authentication or
// authorization issue from the GitHub API or gh CLI.
func IsAuthError(output string) bool {
	matched := containsSubstring(output,
		"gh_token",
		"github_token",
		"authentication",
		"not logged into",
		"unauthorized",
		"permission denied",
		"saml enforcement",
	)
	if matched {
		errorutilLog.Printf("Classified output as auth-related (len=%d)", len(output))
	}
	return matched
}

// IsInsufficientScopesError reports whether err indicates that a GitHub
// GraphQL API request was rejected because the authenticated token is
// missing required OAuth/PAT scopes. The gh CLI and GitHub GraphQL API
// surface this as the literal "INSUFFICIENT_SCOPES" error type.
// It returns false when err is nil.
//
// Exception: the gh CLI returns this condition only as error text, with no
// machine-readable type or status code, so classification is necessarily a
// substring match. The match lives here rather than at call sites so the
// fragile literal is documented, tested, and updated in one place.
func IsInsufficientScopesError(err error) bool {
	matched := containsErrorSubstring(err, "insufficient_scopes")
	if matched {
		errorutilLog.Printf("Classified error as insufficient scopes: %v", err)
	}
	return matched
}

// IsAlreadyMergedError reports whether err indicates that a `gh pr merge`
// operation failed because the pull request was already merged. The gh CLI
// only surfaces this state via error text: either the phrase "already merged"
// (matched case-insensitively) or the uppercase GraphQL "MERGED" state literal
// (matched case-sensitively so that ordinary wording such as "could not be
// merged" or "not merged" is not misclassified as success).
// It returns false when err is nil.
//
// Exception: `gh pr merge` exposes no structured state for this condition, so
// classification is necessarily a substring match. The match lives here rather
// than at call sites so the fragile literals are documented, tested, and
// updated in one place.
func IsAlreadyMergedError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	matched := containsSubstring(message, "already merged") ||
		containsCaseSensitiveSubstring(message, "MERGED")
	if matched {
		errorutilLog.Printf("Classified error as already-merged: %v", err)
	}
	return matched
}

// containsErrorSubstring reports whether err contains any of the provided
// substrings after lowercasing the full error message for case-insensitive
// matching.
func containsErrorSubstring(err error, substrings ...string) bool {
	if err == nil {
		return false
	}
	return containsSubstring(err.Error(), substrings...)
}

// containsCaseSensitiveSubstring reports whether value contains any of the
// provided substrings, preserving case. It is used for markers such as the
// GraphQL "MERGED" state literal where case carries meaning.
func containsCaseSensitiveSubstring(value string, substrings ...string) bool {
	for _, substring := range substrings {
		if strings.Contains(value, substring) {
			return true
		}
	}
	return false
}

func containsSubstring(value string, substrings ...string) bool {
	msg := strings.ToLower(value)
	for _, substring := range substrings {
		if strings.Contains(msg, substring) {
			return true
		}
	}
	return false
}

// containsHTTPStatusSubstring reports whether err contains a recognized
// HTTP-style status pattern for the provided status code and keyword.
func containsHTTPStatusSubstring(err error, code, keyword string) bool {
	return containsErrorSubstring(
		err,
		"http "+code,
		"http status "+code,
		"status "+code,
		code+": "+keyword,
		code+" "+keyword,
	)
}
