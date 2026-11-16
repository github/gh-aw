package safeoutput

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdoptionRegistryMatchesRuntimeAndSafeOutputInventory(t *testing.T) {
	t.Parallel()
	runtimeJSON, err := os.ReadFile(filepath.Join("..", "..", "actions", "setup", "js", "safe_output_adoption_rules.json"))
	require.NoError(t, err)
	assert.JSONEq(t, string(adoptionRulesJSON), string(runtimeJSON))

	toolsJSON, err := os.ReadFile(filepath.Join("..", "workflow", "js", "safe_outputs_tools.json"))
	require.NoError(t, err)
	var tools []struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(toolsJSON, &tools))
	expected := make([]string, 0, len(tools)+4)
	for _, tool := range tools {
		expected = append(expected, tool.Name)
	}
	expected = append(expected, "call_workflow", "comment_memory", "dispatch_repository", "dispatch_workflow")
	slices.Sort(expected)
	expected = slices.Compact(expected)

	rules := Rules()
	actual := make([]string, 0, len(rules))
	for _, rule := range rules {
		actual = append(actual, rule.Type)
		assert.Equal(t, rule.AttributionEligible, rule.EffectClass != EffectInfra)
		assert.Equal(t, rule.AttributionEligible, adoptionFromRule(rule).SurvivalRequired)
	}
	assert.Equal(t, expected, actual)
	assert.Len(t, rules, 55)
}

func TestEvaluateDelivery(t *testing.T) {
	t.Parallel()
	const deliveredAt = "2026-11-16T12:34:56.123456789Z"
	tests := []struct {
		name         string
		outputType   string
		state        AdoptionState
		signal       string
		source       string
		adoptionAt   string
		effect       EffectClass
		attributable bool
	}{
		{"proposal", "create_issue", AdoptionPending, "human_decision_pending", "adoption-registry", "", EffectProposal, true},
		{"publication", "add_comment", AdoptionAccepted, "delivered", "delivery-rule", deliveredAt, EffectPublication, true},
		{"mutation", "add_labels", AdoptionAccepted, "delivered", "delivery-rule", deliveredAt, EffectMutation, true},
		{"delegation", "dispatch_workflow", AdoptionAccepted, "delivered", "delivery-rule", deliveredAt, EffectDelegation, true},
		{"infrastructure", "noop", AdoptionNotApplicable, "no_repository_value_effect", "adoption-registry", "", EffectInfra, false},
		{"unknown", "custom_output", AdoptionUnknown, "unsupported_safe_output", "adoption-registry", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delivery, adoption := EvaluateDelivery(tt.outputType, deliveredAt)
			assert.Equal(t, DeliveryDelivered, delivery.State)
			assert.Equal(t, deliveredAt, delivery.At)
			assert.Equal(t, tt.state, adoption.State)
			assert.Equal(t, tt.signal, adoption.Signal)
			assert.Equal(t, tt.source, adoption.Source)
			assert.Equal(t, tt.adoptionAt, adoption.AdoptionAt)
			assert.Equal(t, tt.effect, adoption.EffectClass)
			assert.Equal(t, tt.attributable, adoption.AttributionEligible)
		})
	}
}

func TestEvaluateAdoptionPrecedence(t *testing.T) {
	t.Parallel()
	const deliveredAt = "2026-11-16T12:00:00Z"
	const decisionAt = "2026-11-16T13:00:00Z"
	tests := []struct {
		name       string
		outputType string
		outcome    ObservedAdoption
		state      AdoptionState
		source     string
		adoptionAt string
	}{
		{"accepted proposal", "create_issue", ObservedAdoption{Status: "accepted", Signal: "completed", At: decisionAt}, AdoptionAccepted, "adoption-evaluator", decisionAt},
		{"rejected proposal", "create_issue", ObservedAdoption{Status: "rejected", Signal: "closed_not_planned", At: decisionAt}, AdoptionRejected, "adoption-evaluator", ""},
		{"pending proposal", "create_issue", ObservedAdoption{Status: "pending", Signal: "open"}, AdoptionPending, "adoption-evaluator", ""},
		{"pending publication retains fallback delivery", "add_comment", ObservedAdoption{Status: "pending", Signal: "pending"}, AdoptionAccepted, "delivery-rule", deliveredAt},
		{"unknown publication retains fallback delivery", "add_comment", ObservedAdoption{Status: "unknown", Signal: "target_exists_only"}, AdoptionAccepted, "delivery-rule", deliveredAt},
		{"failed delegation rejects delivery", "dispatch_workflow", ObservedAdoption{Status: "rejected", Signal: "workflow_no_effect"}, AdoptionRejected, "adoption-evaluator", ""},
		{"lifecycle close", "close_issue", ObservedAdoption{Status: "lifecycle", Signal: "lifecycle_bot_close"}, AdoptionLifecycle, "adoption-evaluator", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, adoption := EvaluateAdoption(tt.outputType, deliveredAt, tt.outcome)
			assert.Equal(t, tt.state, adoption.State)
			assert.Equal(t, tt.source, adoption.Source)
			assert.Equal(t, tt.adoptionAt, adoption.AdoptionAt)
		})
	}
}

func TestRulesReturnsCopy(t *testing.T) {
	t.Parallel()
	rules := Rules()
	rules[0].Type = "changed"
	rule, ok := RuleFor("add_comment")
	require.True(t, ok)
	assert.Equal(t, "add_comment", rule.Type)
}
