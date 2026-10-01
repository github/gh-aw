//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/assert"
)

// TestGetDefaultAgentModel verifies that each known engine returns the correct
// built-in default model string, and that unknown engines return an empty string.
func TestGetDefaultAgentModel(t *testing.T) {
	tests := []struct {
		engineID      string
		expectedModel string
	}{
		{
			engineID:      string(constants.CopilotEngine),
			expectedModel: constants.CopilotBYOKDefaultModel,
		},
		{
			engineID:      string(constants.ClaudeEngine),
			expectedModel: constants.AgentDefaultModel,
		},
		{
			engineID:      string(constants.GeminiEngine),
			expectedModel: constants.AgentDefaultModel,
		},
		{
			engineID:      string(constants.PiEngine),
			expectedModel: constants.AgentDefaultModel,
		},
		{
			engineID:      string(constants.CodexEngine),
			expectedModel: "",
		},
		{
			engineID:      "unknown-engine",
			expectedModel: "",
		},
		{
			engineID:      "",
			expectedModel: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.engineID, func(t *testing.T) {
			got := getDefaultAgentModel(tt.engineID)
			assert.Equal(t, tt.expectedModel, got, "getDefaultAgentModel(%q)", tt.engineID)
		})
	}
}

// TestGetDefaultModelOverrideVar verifies that each known engine returns the correct
// enterprise override variable name, and that engines without an override return "".
func TestGetDefaultModelOverrideVar(t *testing.T) {
	tests := []struct {
		engineID    string
		expectedVar string
	}{
		{
			engineID:    string(constants.CopilotEngine),
			expectedVar: compilerenv.DefaultModelCopilot,
		},
		{
			engineID:    string(constants.ClaudeEngine),
			expectedVar: compilerenv.DefaultModelClaude,
		},
		{
			engineID:    string(constants.CodexEngine),
			expectedVar: compilerenv.DefaultModelCodex,
		},
		{
			// GeminiEngine has no enterprise default override variable.
			engineID:    string(constants.GeminiEngine),
			expectedVar: "",
		},
		{
			engineID:    string(constants.PiEngine),
			expectedVar: "",
		},
		{
			engineID:    "unknown-engine",
			expectedVar: "",
		},
		{
			engineID:    "",
			expectedVar: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.engineID, func(t *testing.T) {
			got := getDefaultModelOverrideVar(tt.engineID)
			assert.Equal(t, tt.expectedVar, got, "getDefaultModelOverrideVar(%q)", tt.engineID)
		})
	}
}
