package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueStatusLifecycleRule(t *testing.T) {
	rule, ok := RuleFor("set_issue_status")
	require.True(t, ok)
	assert.Equal(t, "mutation", rule.EffectClass)
	assert.Equal(t, "issue", rule.ParentResource)
	assert.True(t, rule.FallbackToDelivery)
	assert.True(t, rule.ValueEligible)
	assert.True(t, rule.SurvivalRequired)
	assert.Empty(t, rule.HumanDecision, "native status transitions do not imply suggestion approval")
	assert.Equal(t, "set_issue_status", testDelivery(t, "set_issue_status").Type)
}
