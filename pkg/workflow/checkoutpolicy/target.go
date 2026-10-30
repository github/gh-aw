// Package checkoutpolicy defines the compiler's pull_request_target trust predicates.
package checkoutpolicy

import (
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

var (
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	refPattern        = regexp.MustCompile(`^[^\s${}*?\[\\:~^]+$`)
)

// MatchesExpression accepts only one complete expression naming the expected context.
func MatchesExpression(value, expected string) bool {
	expression, ok := actionslanguageservice.WrappedExpression(value)
	if !ok {
		return false
	}
	path, ok := actionslanguageservice.Reference(expression)
	return ok && slices.Equal(path, strings.Split(expected, "."))
}

// Base accepts the supported base-repository/ref forms. Callers must separately
// reject additional fetches; this predicate does not authorize those refs.
func Base(repository, ref string) bool {
	repository = strings.TrimSpace(repository)
	if repository != "" && !MatchesExpression(repository, "github.repository") {
		return false
	}
	ref = strings.TrimSpace(ref)
	return ref == "" ||
		MatchesExpression(ref, "github.event.pull_request.base.sha") ||
		MatchesExpression(ref, "github.event.pull_request.base.ref")
}

// LiteralPair identifies pairs eligible for exact allowlist matching, not
// repository/ref existence or trust. Callers must reject wiki and fetch options.
func LiteralPair(repository, ref string) bool {
	return LiteralRepository(repository) && LiteralRef(ref)
}

// LiteralRepository reports whether a selector can participate in a literal allowlist pair.
func LiteralRepository(repository string) bool {
	return repositoryPattern.MatchString(repository)
}

// LiteralRef excludes expression, wildcard, and pull-request refs from allowlist pairs.
func LiteralRef(ref string) bool {
	return refPattern.MatchString(ref) && !strings.HasPrefix(ref, "refs/pull/")
}
