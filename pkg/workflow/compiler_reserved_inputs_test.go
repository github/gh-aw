package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectWorkQueueClaimInput(t *testing.T) {
	tests := []struct {
		name string
		on   string
	}{
		{
			name: "dispatch with inputs",
			on:   "on:\n  workflow_dispatch:\n    inputs:\n      task:\n        type: string",
		},
		{
			name: "dispatch shorthand",
			on:   `"on": workflow_dispatch`,
		},
		{
			name: "dispatch in event list",
			on:   `"on": [push, workflow_dispatch]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated := injectWorkQueueClaimIntoOnYAML(tt.on)
			assert.Contains(t, updated, WorkQueueClaimInputName+":")
			assert.Contains(t, updated, `description: "Trusted work queue assignment (Reserved for Agentic Workflows)."`)
			assert.Equal(t, updated, injectWorkQueueClaimIntoOnYAML(updated), "input injection should be idempotent")
		})
	}
}

func TestValidateReservedWorkflowInputs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger string
		input   string
	}{
		{"dispatch aw_context", "workflow_dispatch", AwContextInputName},
		{"call aw_context", "workflow_call", AwContextInputName},
		{"dispatch work queue claim", "workflow_dispatch", WorkQueueClaimInputName},
		{"call work queue claim", "workflow_call", WorkQueueClaimInputName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &WorkflowData{
				RawFrontmatter: map[string]any{
					"on": map[string]any{
						tc.trigger: map[string]any{
							"inputs": map[string]any{tc.input: map[string]any{"type": "string"}},
						},
					},
				},
			}
			err := validateReservedWorkflowInputs(data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "is reserved and managed by the compiler")
			assert.Contains(t, err.Error(), "on."+tc.trigger+".inputs."+tc.input)
			err = NewCompiler().validateWorkflowData(data, "workflow.md")
			require.ErrorContains(t, err, "on."+tc.trigger+".inputs."+tc.input+" is reserved")
		})
	}

	require.NoError(t, validateReservedWorkflowInputs(&WorkflowData{
		RawFrontmatter: map[string]any{"on": map[string]any{
			"workflow_dispatch": map[string]any{"inputs": map[string]any{"task": map[string]any{"type": "string"}}},
		}},
	}))
}

func TestInjectAwContextAndQueueClaimInputs(t *testing.T) {
	on := `"on": workflow_dispatch`
	on = injectAwContextIntoOnYAML(on)
	on = injectWorkQueueClaimIntoOnYAML(on)

	assert.Contains(t, on, "workflow_dispatch:")
	assert.Contains(t, on, "aw_context:")
	assert.Contains(t, on, "work_queue_claim:")
}
