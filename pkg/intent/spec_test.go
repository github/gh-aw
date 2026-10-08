//go:build !integration

package intent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-aw/pkg/intent"
)

// SPEC_MISMATCH: README.md documents func ResolveRisk(rec IntentRecord) string,
// but the intent package does not currently export that function. A behavioral
// test cannot be compiled until the documented public API exists.

// TestSpec tests derive from pkg/intent/README.md. They enforce the documented
// public surface of the intent package without coupling to implementation internals.

// TestSpec_PublicAPI_AttributionStatusConstants validates the documented
// AttributionStatus constant values.
// Spec (README "AttributionStatus constants"):
//
//	AttributionMapped="mapped", AttributionUnmapped="unmapped", AttributionUnlinked="unlinked",
//	AttributionAmbiguous="ambiguous", AttributionSuggested="suggested"
func TestSpec_PublicAPI_AttributionStatusConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, intent.AttributionMapped, intent.AttributionStatus("mapped"),
		"AttributionMapped should equal \"mapped\" per the spec")
	assert.Equal(t, intent.AttributionUnmapped, intent.AttributionStatus("unmapped"),
		"AttributionUnmapped should equal \"unmapped\" per the spec")
	assert.Equal(t, intent.AttributionUnlinked, intent.AttributionStatus("unlinked"),
		"AttributionUnlinked should equal \"unlinked\" per the spec")
	assert.Equal(t, intent.AttributionAmbiguous, intent.AttributionStatus("ambiguous"),
		"AttributionAmbiguous should equal \"ambiguous\" per the spec")
	assert.Equal(t, intent.AttributionSuggested, intent.AttributionStatus("suggested"),
		"AttributionSuggested should equal \"suggested\" per the spec")
}

// TestSpec_PublicAPI_AttributionSourceConstants validates the documented
// AttributionSource constant values.
// Spec (README "AttributionSource constants"):
//
//	SourceExplicitMetadata="explicit_metadata", SourceClosingIssue="closing_issue", etc.
func TestSpec_PublicAPI_AttributionSourceConstants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		got      intent.AttributionSource
		expected intent.AttributionSource
	}{
		{"SourceExplicitMetadata", intent.SourceExplicitMetadata, "explicit_metadata"},
		{"SourceClosingIssue", intent.SourceClosingIssue, "closing_issue"},
		{"SourceParentIssue", intent.SourceParentIssue, "parent_issue"},
		{"SourceReferencedIssue", intent.SourceReferencedIssue, "referenced_issue"},
		{"SourceProject", intent.SourceProject, "project"},
		{"SourceMilestone", intent.SourceMilestone, "milestone"},
		{"SourceIssueLabels", intent.SourceIssueLabels, "issue_labels"},
		{"SourceArtifactLabels", intent.SourceArtifactLabels, "artifact_labels"},
		{"SourceSuggestion", intent.SourceSuggestion, "suggestion"},
		{"SourceNone", intent.SourceNone, "none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.expected, c.got, "%s should equal %q per the spec", c.name, c.expected)
		})
	}
}

// TestSpec_PublicAPI_ResolvePullRequest_ExplicitIntent validates that an explicit
// intent record is returned as-is, with ResolverVersion stamped when absent.
// Spec: "Explicit intent metadata (PullRequestData.ExplicitIntent) — used as-is."
func TestSpec_PublicAPI_ResolvePullRequest_ExplicitIntent(t *testing.T) {
	t.Parallel()
	resolver := intent.Resolver{ResolverVersion: "v1"}

	explicit := &intent.IntentRecord{
		Status: intent.AttributionMapped,
		Source: intent.SourceExplicitMetadata,
		Rule:   "explicit",
	}
	record := resolver.ResolvePullRequest(intent.PullRequestData{ExplicitIntent: explicit})

	assert.Equal(t, intent.AttributionMapped, record.Status,
		"ResolvePullRequest should honour explicit intent status")
	assert.Equal(t, intent.SourceExplicitMetadata, record.Source,
		"ResolvePullRequest should honour explicit intent source")
	assert.Equal(t, "v1", record.ResolverVersion,
		"ResolvePullRequest should stamp ResolverVersion when absent from explicit intent")
}

// TestSpec_PublicAPI_ResolvePullRequest_SingleClosingIssue validates that a single
// closing issue produces a closing-issue attribution.
// Spec: "A single closing issue — resolved from the issue's labels."
func TestSpec_PublicAPI_ResolvePullRequest_SingleClosingIssue(t *testing.T) {
	t.Parallel()
	resolver := intent.Resolver{
		MatchLabels: func(labels []string) []string { return labels },
	}

	record := resolver.ResolvePullRequest(intent.PullRequestData{
		ClosingIssues: []intent.RootReference{{
			NodeID: "I_kwDOAAABCQ4",
			Type:   "issue",
			URL:    "https://github.com/owner/repo/issues/1",
			Labels: []string{"security"},
		}},
	})

	assert.Equal(t, intent.AttributionMapped, record.Status,
		"single closing issue with matching labels should produce mapped status")
	assert.Equal(t, intent.SourceClosingIssue, record.Source,
		"single closing issue should produce closing_issue source")
	assert.Equal(t, "single_closing_issue", record.Rule,
		"single closing issue should produce single_closing_issue rule")
}

// TestSpec_PublicAPI_ResolvePullRequest_MultipleClosingIssues validates that
// multiple closing issues produce an ambiguous attribution.
// Spec: "Multiple competing sources were found → ambiguous."
func TestSpec_PublicAPI_ResolvePullRequest_MultipleClosingIssues(t *testing.T) {
	t.Parallel()
	resolver := intent.Resolver{}

	record := resolver.ResolvePullRequest(intent.PullRequestData{
		ClosingIssues: []intent.RootReference{
			{URL: "https://github.com/owner/repo/issues/1"},
			{URL: "https://github.com/owner/repo/issues/2"},
		},
	})

	assert.Equal(t, intent.AttributionAmbiguous, record.Status,
		"multiple closing issues should produce ambiguous status")
	assert.Equal(t, intent.SourceClosingIssue, record.Source,
		"multiple closing issues should produce closing_issue source")
	assert.Equal(t, "multiple_closing_issues", record.Rule,
		"multiple closing issues should produce multiple_closing_issues rule")
}

// TestSpec_PublicAPI_ResolvePullRequest_ArtifactFallback validates that PR labels
// are used when no closing issues are present.
// Spec: "PR labels — used as an artifact fallback when no closing issues are present."
func TestSpec_PublicAPI_ResolvePullRequest_ArtifactFallback(t *testing.T) {
	t.Parallel()
	resolver := intent.Resolver{
		MatchLabels: func(labels []string) []string { return labels },
	}

	record := resolver.ResolvePullRequest(intent.PullRequestData{
		NodeID: "PR_kwDOAAABCD4",
		URL:    "https://github.com/owner/repo/pull/77",
		Labels: []string{"automation"},
	})

	assert.Equal(t, intent.AttributionMapped, record.Status,
		"PR labels should produce mapped status when labels match")
	assert.Equal(t, intent.SourceArtifactLabels, record.Source,
		"PR label fallback should produce artifact_labels source")
	assert.Equal(t, "pull_request_label_fallback", record.Rule,
		"PR label fallback should produce pull_request_label_fallback rule")
}

// TestSpec_PublicAPI_ResolvePullRequest_NoSources validates that no supported
// sources produces an unlinked attribution.
// Spec: "No supported sources — returns an AttributionUnlinked record."
func TestSpec_PublicAPI_ResolvePullRequest_NoSources(t *testing.T) {
	t.Parallel()
	resolver := intent.Resolver{}

	record := resolver.ResolvePullRequest(intent.PullRequestData{})

	assert.Equal(t, intent.AttributionUnlinked, record.Status,
		"no sources should produce unlinked status")
	assert.Equal(t, intent.SourceNone, record.Source,
		"no sources should produce none source")
	assert.Equal(t, "no_supported_intent_source", record.Rule,
		"no sources should produce no_supported_intent_source rule")
}

// TestSpec_PublicAPI_ResolveIssue_Mapped validates that an issue with matching
// labels produces a mapped intent record.
// Spec: "Resolver.ResolveIssue — resolves intent for an issue using its labels."
func TestSpec_PublicAPI_ResolveIssue_Mapped(t *testing.T) {
	t.Parallel()
	resolver := intent.Resolver{
		MatchLabels: func(labels []string) []string { return labels },
	}

	record := resolver.ResolveIssue(
		"I_kwDOAAABCQ4",
		"https://github.com/owner/repo/issues/42",
		[]string{"security"},
	)

	assert.Equal(t, intent.AttributionMapped, record.Status,
		"issue with matching labels should produce mapped status")
	assert.Equal(t, intent.SourceIssueLabels, record.Source,
		"ResolveIssue should produce issue_labels source")
	assert.Equal(t, "issue_label_fallback", record.Rule,
		"ResolveIssue should produce issue_label_fallback rule")
	assert.Equal(t, "issue", record.RootType,
		"ResolveIssue should set RootType to issue")
}

// TestSpec_PublicAPI_ResolveIssue_NoLabelsUnlinked validates that an issue with
// no labels produces an unlinked record.
// Spec: "No supported sources — returns an AttributionUnlinked record."
func TestSpec_PublicAPI_ResolveIssue_NoLabelsUnlinked(t *testing.T) {
	t.Parallel()
	resolver := intent.Resolver{}

	record := resolver.ResolveIssue("I_kwDOAAABCQ4", "https://github.com/owner/repo/issues/1", nil)

	assert.Equal(t, intent.AttributionUnlinked, record.Status,
		"issue with no labels should produce unlinked status")
	assert.Equal(t, "no_supported_intent_source", record.Rule,
		"issue with no labels should produce no_supported_intent_source rule")
}

// TestSpec_PublicAPI_AuthorizeTool validates deny precedence and the documented
// nil-versus-empty allowlist contract.
func TestSpec_PublicAPI_AuthorizeTool(t *testing.T) {
	t.Parallel()
	authorizer := intent.Authorizer{}
	tests := []struct {
		name   string
		policy intent.ExecutionPolicy
		tool   string
		want   error
	}{
		{name: "nil allowlist is unrestricted", policy: intent.ExecutionPolicy{}, tool: "read"},
		{name: "listed tool is allowed", policy: intent.ExecutionPolicy{AllowedTools: []string{"read"}}, tool: "read"},
		{name: "unlisted tool is not allowed", policy: intent.ExecutionPolicy{AllowedTools: []string{"read"}}, tool: "write", want: intent.ErrToolNotAllowed},
		{name: "non-nil empty allowlist denies all", policy: intent.ExecutionPolicy{AllowedTools: []string{}}, tool: "read", want: intent.ErrToolNotAllowed},
		{name: "explicit denial wins over allowlist", policy: intent.ExecutionPolicy{AllowedTools: []string{"write"}, DeniedTools: []string{"write"}}, tool: "write", want: intent.ErrToolDenied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := authorizer.AuthorizeTool(tt.policy, tt.tool)
			if tt.want == nil {
				assert.NoError(t, err, "AuthorizeTool should allow the tool for %s", tt.name)
				return
			}
			assert.ErrorIs(t, err, tt.want, "AuthorizeTool should return the documented sentinel for %s", tt.name)
		})
	}
}

// TestSpec_PublicAPI_PolicyCompilerConditions validates the documented matching
// rules: empty fields are wildcards, label conditions use Labels, and Org may
// match either the repository organization or owner.
func TestSpec_PublicAPI_PolicyCompilerConditions(t *testing.T) {
	t.Parallel()
	compiler := intent.PolicyCompiler{Rules: []intent.PolicyRule{
		{
			ID:    "label-and-org",
			Scope: "repository",
			When:  intent.PolicyCondition{Domain: "security", Org: "octo-org"},
			Set:   intent.ExecutionPolicy{Autonomy: "supervised", WriteScope: "feature_branch"},
		},
	}}

	tests := []struct {
		name string
		repo intent.RepositoryContext
	}{
		{name: "organization matches", repo: intent.RepositoryContext{Org: "octo-org"}},
		{name: "owner matches", repo: intent.RepositoryContext{Owner: "octo-org"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := compiler.Compile(intent.IntentRecord{Status: intent.AttributionMapped, Labels: []string{"security"}}, tt.repo)
			assert.Contains(t, policy.RuleIDs, "label-and-org", "Compile should record the ID of a matching rule")
			assert.Equal(t, "supervised", policy.Autonomy, "Compile should apply the matching policy fragment")
		})
	}

	policy := compiler.Compile(intent.IntentRecord{
		Status:  intent.AttributionMapped,
		Domains: []string{"security"},
	}, intent.RepositoryContext{Org: "octo-org"})
	assert.NotContains(t, policy.RuleIDs, "label-and-org", "Domain conditions must match Labels, not IntentRecord.Domains")
}
