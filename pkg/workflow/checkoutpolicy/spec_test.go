//go:build !integration

package checkoutpolicy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-aw/pkg/workflow/checkoutpolicy"
)

// TestSpec_PublicAPI_MatchesExpression checks README.md / Public API:
// acceptance requires one complete expression with the exact context path.
func TestSpec_PublicAPI_MatchesExpression(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		value string
		path  string
		want  bool
	}{
		{name: "complete repository expression", value: "${{ github.repository }}", path: "github.repository", want: true},
		{name: "complete ref expression", value: "${{ github.base_ref }}", path: "github.base_ref", want: true},
		{name: "different context path", value: "${{ github.repository }}", path: "github.base_ref"},
		{name: "path prefix differs", value: "${{ github.repository_owner }}", path: "github.repository"},
		{name: "bare path", value: "github.repository", path: "github.repository"},
		{name: "literal prefix", value: "prefix-${{ github.repository }}", path: "github.repository"},
		{name: "literal suffix", value: "${{ github.repository }}-suffix", path: "github.repository"},
		{name: "two expressions", value: "${{ github.repository }}${{ github.repository }}", path: "github.repository"},
		{name: "incomplete expression", value: "${{ github.repository", path: "github.repository"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, checkoutpolicy.MatchesExpression(tt.value, tt.path),
				"only one complete expression referencing the expected path should match")
		})
	}
}

// TestSpec_PublicAPI_LiteralRepository checks README.md / Public API:
// a literal repository selector has the owner/repository form.
func TestSpec_PublicAPI_LiteralRepository(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "owner and repository", value: "example/project", want: true},
		{name: "another owner and repository", value: "github/gh-aw", want: true},
		{name: "empty selector"},
		{name: "missing repository", value: "example"},
		{name: "subdirectory", value: "example/project/path"},
		{name: "expression", value: "${{ github.repository }}"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, checkoutpolicy.LiteralRepository(tt.value),
				"repository selectors should be literal owner/repository pairs")
		})
	}
}

// TestSpec_PublicAPI_LiteralRef checks README.md / Public API:
// literal refs exclude expressions, wildcards, and refs/pull/ forms.
func TestSpec_PublicAPI_LiteralRef(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "branch", value: "main", want: true},
		{name: "tag", value: "v1.2.3", want: true},
		{name: "expression", value: "${{ github.base_ref }}"},
		{name: "wildcard", value: "release/*"},
		{name: "pull request head", value: "refs/pull/42/head"},
		{name: "pull request merge", value: "refs/pull/42/merge"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, checkoutpolicy.LiteralRef(tt.value),
				"literal refs should obey the documented expression, wildcard, and pull-ref exclusions")
		})
	}
}

// SPEC_AMBIGUITY: README.md does not enumerate the repository/ref forms
// accepted by Base, or define the complete LiteralPair eligibility rule.
// These predicates alone do not establish authorization or allowlist membership.
