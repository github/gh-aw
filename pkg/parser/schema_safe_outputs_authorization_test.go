//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeOutputAuthorizationSchema(t *testing.T) {
	authority := map[string]any{"endpoint": "https://authority.example/authorize", "audience": "gh-aw"}
	for _, output := range []map[string]any{
		{"authorization": authority, "create-issue": nil},
		{"create-issue": map[string]any{"authorization": authority}},
		{"noop": map[string]any{"authorization": authority}},
		{"scripts": map[string]any{"notify": map[string]any{"script": "return async () => ({})", "authorization": authority}}},
		{"actions": map[string]any{"notify": map[string]any{"uses": "./reviewed", "authorization": authority}}},
		{"jobs": map[string]any{"notify": map[string]any{"steps": []any{map[string]any{"run": "echo reviewed"}}, "authorization": authority}}},
	} {
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "push", "safe-outputs": output,
		}, "authorization.md"))
	}
	for _, authority := range []any{false, nil, map[string]any{"endpoint": "https://authority.example/a"}, map[string]any{
		"endpoint": "https://authority.example/a", "audience": "gh-aw", "ignore": true,
	}} {
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "push", "safe-outputs": map[string]any{"authorization": authority},
		}, "authorization.md"))
	}
	keys, err := GetSafeOutputTypeKeys()
	require.NoError(t, err)
	require.NotContains(t, keys, "authorization")
}
