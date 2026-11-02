package connectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServiceCopiesInlineDeclarationAndGrants(t *testing.T) {
	t.Parallel()
	original := fixturePlan(t, "https://api.example.com")
	service, err := NewService(original.spec, original.connection)
	require.NoError(t, err)
	original.spec.Endpoints["api"] = "https://attacker.example"
	original.connection.Resources["issues"][0] = "OTHER"
	plan, err := service.Select(Selection{MCP: []string{"get-issue"}})
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", plan.spec.Endpoints["api"])
	assert.Equal(t, []string{"ISSUE-1"}, plan.connection.Resources["issues"])
}
