//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderIssueSchemaRejectsRemovedNativeRoutes(t *testing.T) {
	for alias, config := range map[string]any{
		"jira-issue":         map[string]any{"project": "APP"},
		"linear-issue":       map[string]any{"organization-id": "organization-uuid"},
		"azure-devops-issue": map[string]any{"project_id": "project-uuid"},
	} {
		t.Run(alias, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": map[string]any{"gateway-dispatch": map[string]any{alias: config}},
			}, "provider.md")
			require.Error(t, err)
		})
	}
}
