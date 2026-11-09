//go:build !integration

package intent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/intent"
)

// Formal test suite derived from specs/intent-attribution-agent-governance.md,
// focusing on Enforcement (Authorizer.AuthorizeTool) sections, plus fail-closed
// policy compilation for unlinked/ambiguous attribution. Each test corresponds
// to a named predicate or invariant in the behavioral coverage map.

// TestAuthorizeTool_DeniedWins (P9 — AuthorizeToolDeniedWins)
// Invariant: a tool in DeniedTools is rejected even if it also appears in
// AllowedTools.
func TestAuthorizeTool_DeniedWins(t *testing.T) {
	t.Parallel()
	policy := intent.ExecutionPolicy{
		AllowedTools: []string{"read", "write"},
		DeniedTools:  []string{"write"},
	}
	err := intent.Authorizer{}.AuthorizeTool(policy, "write")
	require.ErrorIs(t, err, intent.ErrToolDenied,
		"P9: denied tool must return ErrToolDenied")
}

// TestAuthorizeTool_AllowlistGate (P10 — AuthorizeToolAllowlistGate)
// Invariant: a non-nil allow list rejects tools not listed.
func TestAuthorizeTool_AllowlistGate(t *testing.T) {
	t.Parallel()
	policy := intent.ExecutionPolicy{AllowedTools: []string{"read"}}
	err := intent.Authorizer{}.AuthorizeTool(policy, "exec")
	require.ErrorIs(t, err, intent.ErrToolNotAllowed,
		"P10: tool absent from allow list must return ErrToolNotAllowed")

	require.NoError(t, intent.Authorizer{}.AuthorizeTool(policy, "read"),
		"P10: tool present in the allow list must be authorized")
}

// TestAuthorizeTool_UnrestrictedWhenAllowedToolsNil (P11 — AuthorizeToolUnrestricted)
// Invariant: nil AllowedTools means unrestricted (except explicit denies).
func TestAuthorizeTool_UnrestrictedWhenAllowedToolsNil(t *testing.T) {
	t.Parallel()
	policy := intent.ExecutionPolicy{AllowedTools: nil, DeniedTools: []string{"exec"}}

	require.NoError(t, intent.Authorizer{}.AuthorizeTool(policy, "read"),
		"P11: nil AllowedTools must permit any tool that isn't denied")
	require.NoError(t, intent.Authorizer{}.AuthorizeTool(policy, "anything"),
		"P11: nil AllowedTools must permit any tool that isn't denied")

	err := intent.Authorizer{}.AuthorizeTool(policy, "exec")
	require.ErrorIs(t, err, intent.ErrToolDenied,
		"P11: an explicit deny must still return ErrToolDenied")
}

// TestAuthorizeTool_EmptyAllowedToolsDeniesAll (P12 — AuthorizeToolEmptyDenyAll)
// Invariant: a non-nil, empty AllowedTools denies every tool, distinct from nil.
func TestAuthorizeTool_EmptyAllowedToolsDeniesAll(t *testing.T) {
	t.Parallel()
	policy := intent.ExecutionPolicy{AllowedTools: []string{}}
	err := intent.Authorizer{}.AuthorizeTool(policy, "read")
	require.ErrorIs(t, err, intent.ErrToolNotAllowed,
		"P12: non-nil empty AllowedTools must return ErrToolNotAllowed")
}

// TestSafestDefaultPolicy_FailClosedForIndeterminateStatus (P13 — SafestDefaultFailClosed)
// Invariant: indeterminate attribution forces the safest policy regardless of
// configured rules.
func TestSafestDefaultPolicy_FailClosedForIndeterminateStatus(t *testing.T) {
	t.Parallel()
	autoMerge := true
	permissive := intent.PolicyRule{
		ID: "wildcard-permissive",
		Set: intent.ExecutionPolicy{
			Autonomy:              "bounded",
			WriteScope:            "any_branch",
			HumanApprovalRequired: false,
			AutoMergeAllowed:      &autoMerge,
			MaxAttempts:           10,
		},
	}
	compiler := intent.PolicyCompiler{Rules: []intent.PolicyRule{permissive}}
	repo := intent.RepositoryContext{Owner: "owner", Name: "repo"}

	for _, status := range []intent.AttributionStatus{
		intent.AttributionUnlinked,
		intent.AttributionAmbiguous,
		intent.AttributionUnmapped,
		intent.AttributionSuggested,
	} {
		t.Run(string(status), func(t *testing.T) {
			rec := intent.IntentRecord{Status: status}
			policy := compiler.Compile(rec, repo)

			assert.Equal(t, "propose_only", policy.Autonomy, "P13: indeterminate status must force propose_only")
			assert.Equal(t, "none", policy.WriteScope, "P13: indeterminate status must force no write scope")
			assert.True(t, policy.HumanApprovalRequired, "P13: indeterminate status must force human approval")
			require.NotNil(t, policy.AutoMergeAllowed)
			assert.False(t, *policy.AutoMergeAllowed, "P13: indeterminate status must force auto-merge denial")
			assert.Equal(t, 1, policy.MaxAttempts, "P13: indeterminate status must force a single attempt")
		})
	}
}

// TestEdgeCase_NilDeniedAndAllowedTools validates that AuthorizeTool does not
// panic on a zero-value policy.
func TestEdgeCase_NilDeniedAndAllowedTools(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		err := intent.Authorizer{}.AuthorizeTool(intent.ExecutionPolicy{}, "read")
		assert.NoError(t, err, "edge case: zero-value policy (nil AllowedTools/DeniedTools) must be unrestricted")
	})
}

// TestEdgeCase_MultipleMatchingRulesPreserveStricterConstraint validates that a
// stricter constraint from an earlier rule isn't overridden by a later, more
// lenient rule.
func TestEdgeCase_MultipleMatchingRulesPreserveStricterConstraint(t *testing.T) {
	t.Parallel()
	strict := intent.PolicyRule{
		ID: "strict-first",
		Set: intent.ExecutionPolicy{
			Autonomy:   "propose_only",
			WriteScope: "none",
		},
	}
	lenient := intent.PolicyRule{
		ID: "lenient-second",
		Set: intent.ExecutionPolicy{
			Autonomy:   "bounded",
			WriteScope: "any_branch",
		},
	}
	compiler := intent.PolicyCompiler{Rules: []intent.PolicyRule{strict, lenient}}
	rec := intent.IntentRecord{Status: intent.AttributionMapped, Labels: []string{"security"}}
	repo := intent.RepositoryContext{Owner: "owner", Name: "repo"}

	policy := compiler.Compile(rec, repo)

	assert.Equal(t, "propose_only", policy.Autonomy,
		"edge case: a later lenient rule must not override an earlier stricter autonomy constraint")
	assert.Equal(t, "none", policy.WriteScope,
		"edge case: a later lenient rule must not override an earlier stricter write-scope constraint")
}
