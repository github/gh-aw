//go:build !integration

package workflow

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "go.yaml.in/yaml/v3"
)

// formalTransitionAllowed models the allowed-transitions gate.
// Empty/absent transition config allows any (from,to) pair.
func formalTransitionAllowed(transitions []LabelTransition, from, to string) bool {
	if len(transitions) == 0 {
		return true
	}
	for _, t := range transitions {
		if t.From == from && t.To == to {
			return true
		}
	}
	return false
}

func formalEvaluateTransition(transitions []LabelTransition, from, to string) formalReplaceLabelOutcome {
	if !formalTransitionAllowed(transitions, from, to) {
		return formalReplaceLabelOutcome{
			Success: false,
			Skipped: true,
		}
	}
	return formalReplaceLabelOutcome{Success: true}
}

type formalPostSetLabelsVerification struct {
	Success bool
	Error   string
}

// formalVerifyPostSetLabels is a formal-model stub for RL-057/RL-058/RL-059.
// stub — replace with real implementation once JS runtime verification is implemented.
func formalVerifyPostSetLabels(labelToRemove, labelToAdd string, responseLabels []string) formalPostSetLabelsVerification {
	hasAdded := slices.Contains(responseLabels, labelToAdd)
	hasRemoved := !slices.Contains(responseLabels, labelToRemove)

	if hasAdded && hasRemoved {
		return formalPostSetLabelsVerification{Success: true}
	}

	if !hasAdded {
		return formalPostSetLabelsVerification{
			Success: false,
			Error:   fmt.Sprintf("replace_label: label_to_add %q not found in POST-setLabels response", labelToAdd),
		}
	}

	return formalPostSetLabelsVerification{
		Success: false,
		Error:   fmt.Sprintf("replace_label: label_to_remove %q still present after setLabels call", labelToRemove),
	}
}

func TestFormalTransitionSetEmptyAllowsAny(t *testing.T) {
	assert.True(t, formalTransitionAllowed(nil, "todo", "in-progress"))
	assert.True(t, formalTransitionAllowed([]LabelTransition{}, "in-progress", "done"))
}

func TestFormalTransitionExactMatchRequired(t *testing.T) {
	transitions := []LabelTransition{{From: "todo", To: "in-progress"}}
	assert.True(t, formalTransitionAllowed(transitions, "todo", "in-progress"))
	assert.False(t, formalTransitionAllowed(transitions, "in-progress", "todo"))
	assert.False(t, formalTransitionAllowed(transitions, "todo", "done"))
}

func TestFormalTransitionRejectedYieldsSoftSkip(t *testing.T) {
	outcome := formalEvaluateTransition([]LabelTransition{{From: "todo", To: "in-progress"}}, "todo", "done")
	assert.False(t, outcome.Success)
	assert.True(t, outcome.Skipped)
}

func TestFormalTransitionConfigShape(t *testing.T) {
	var transition LabelTransition
	require.NoError(t, yamlv3.Unmarshal([]byte("from: todo\nto: done\n"), &transition))
	assert.Equal(t, "todo", transition.From)
	assert.Equal(t, "done", transition.To)
}

func TestFormalTransitionConfigListShape(t *testing.T) {
	var cfg ReplaceLabelConfig
	require.NoError(t, yamlv3.Unmarshal([]byte("allowed-transitions:\n  - from: todo\n    to: in-progress\n  - from: in-progress\n    to: done\n"), &cfg))
	require.Len(t, cfg.AllowedTransitions, 2)
	assert.Equal(t, LabelTransition{From: "todo", To: "in-progress"}, cfg.AllowedTransitions[0])
	assert.Equal(t, LabelTransition{From: "in-progress", To: "done"}, cfg.AllowedTransitions[1])
}

func TestFormalPostSetLabelsAddPresent(t *testing.T) {
	outcome := formalVerifyPostSetLabels("todo", "done", []string{"done", "triaged"})
	assert.True(t, outcome.Success)
	assert.Empty(t, outcome.Error)
}

func TestFormalPostSetLabelsRemoveAbsent(t *testing.T) {
	outcome := formalVerifyPostSetLabels("todo", "done", []string{"done", "todo"})
	assert.False(t, outcome.Success)
	require.NotEmpty(t, outcome.Error)
	assert.Contains(t, outcome.Error, "label_to_remove")
}

func TestFormalPartialSuccessRejected(t *testing.T) {
	tests := []struct {
		name       string
		labels     []string
		expectPass bool
	}{
		{name: "missing add", labels: []string{"todo", "triaged"}, expectPass: false},
		{name: "stale remove", labels: []string{"todo", "done"}, expectPass: false},
		{name: "both satisfied", labels: []string{"done", "triaged"}, expectPass: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcome := formalVerifyPostSetLabels("todo", "done", tt.labels)
			assert.Equal(t, tt.expectPass, outcome.Success)
			if tt.expectPass {
				assert.Empty(t, outcome.Error)
			} else {
				require.NotEmpty(t, outcome.Error)
			}
		})
	}
}

func TestFormalPartialSuccessNoNewErrorCode(t *testing.T) {
	outcome := formalVerifyPostSetLabels("todo", "done", []string{"todo"})
	assert.False(t, outcome.Success)
	require.NotEmpty(t, outcome.Error)

	typ := reflect.TypeFor[formalPostSetLabelsVerification]()
	_, hasErrorCode := typ.FieldByName("ErrorCode")
	assert.False(t, hasErrorCode, "partial-success must use existing {success,error} shape")
}

func TestFormalTransitionEdge_EmptyResponseLabels(t *testing.T) {
	outcome := formalVerifyPostSetLabels("todo", "done", []string{})
	assert.False(t, outcome.Success)
	assert.Contains(t, outcome.Error, "label_to_add")
}

func TestFormalTransitionEdge_SelfTransitionRejectedWhenNotListed(t *testing.T) {
	transitions := []LabelTransition{{From: "todo", To: "in-progress"}}
	assert.False(t, formalTransitionAllowed(transitions, "todo", "todo"))
}

func TestFormalTransitionEdge_DuplicateTransitionEntriesIdempotent(t *testing.T) {
	dupe := []LabelTransition{{From: "todo", To: "done"}, {From: "todo", To: "done"}}
	single := []LabelTransition{{From: "todo", To: "done"}}

	assert.Equal(t,
		formalTransitionAllowed(single, "todo", "done"),
		formalTransitionAllowed(dupe, "todo", "done"),
	)
	assert.Equal(t,
		formalTransitionAllowed(single, "todo", "in-progress"),
		formalTransitionAllowed(dupe, "todo", "in-progress"),
	)
}
