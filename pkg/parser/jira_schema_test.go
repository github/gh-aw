package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJiraToolSchema(t *testing.T) {
	tests := []struct {
		name    string
		jira    any
		wantErr bool
	}{
		{
			name: "service account",
			jira: map[string]any{
				"auth": map[string]any{
					"type":  "service-account",
					"token": "${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}",
				},
				"allowed": []any{"getJiraIssue"},
			},
		},
		{
			name: "API token",
			jira: map[string]any{
				"auth": map[string]any{
					"type":  "api-token",
					"email": "${{ secrets.ATLASSIAN_EMAIL }}",
					"token": "${{ secrets.ATLASSIAN_API_TOKEN }}",
				},
				"allowed": []any{"getVisibleJiraProjects"},
			},
		},
		{
			name: "missing allowlist is rejected",
			jira: map[string]any{
				"auth": map[string]any{
					"type":  "service-account",
					"token": "${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}",
				},
			},
			wantErr: true,
		},
		{
			name: "all tools wildcard is accepted",
			jira: map[string]any{
				"auth": map[string]any{
					"type":  "service-account",
					"token": "${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}",
				},
				"allowed": []any{"*"},
			},
		},
		{
			name: "write tool is rejected",
			jira: map[string]any{
				"auth": map[string]any{
					"type":  "service-account",
					"token": "${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}",
				},
				"allowed": []any{"createJiraIssue"},
			},
			wantErr: true,
		},
		{
			name: "OAuth is rejected",
			jira: map[string]any{
				"auth": map[string]any{
					"type":  "oauth",
					"token": "${{ secrets.ATLASSIAN_API_TOKEN }}",
				},
			},
			wantErr: true,
		},
		{
			name: "literal token is rejected",
			jira: map[string]any{
				"auth": map[string]any{
					"type":  "service-account",
					"token": "literal-token",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(
				map[string]any{
					"on":    map[string]any{"workflow_dispatch": nil},
					"tools": map[string]any{"jira": tt.jira},
				},
				"/tmp/gh-aw/jira-schema-test.md",
			)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
