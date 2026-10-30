//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenBrokerSchema(t *testing.T) {
	for _, value := range []any{
		map[string]any{"copilot-request": true},
		map[string]any{"copilot-task": true},
		map[string]any{"copilot-request": true, "copilot-task": true},
	} {
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "token-broker": value,
		}, "broker.md"))
	}
	for _, value := range []any{
		"true",
		1,
		nil,
		true,
		map[string]any{"enabled": true},
		map[string]any{"copilot-task": false},
		map[string]any{"copilot-request": "true"},
	} {
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "token-broker": value,
		}, "broker.md"))
	}
}
