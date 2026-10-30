//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenBrokerSchema(t *testing.T) {
	for _, value := range []any{true, false} {
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "token-broker": value,
		}, "broker.md"))
	}
	for _, value := range []any{"true", 1, nil, map[string]any{"enabled": true}} {
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "token-broker": value,
		}, "broker.md"))
	}
}
