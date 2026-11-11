//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestCompileRemovedProviderIssueAliases(t *testing.T) {
	for _, test := range []struct{ alias, scope string }{
		{"jira-issue", "project: APP"},
		{"linear-issue", "organization-id: fixture"},
		{"azure-devops-issue", "project_id: fixture"},
	} {
		t.Run(test.alias, func(t *testing.T) {
			root := t.TempDir()
			enableWebhookTriggers(t, root)
			path := filepath.Join(root, test.alias+".md")
			source := "---\non:\n  gateway-dispatch:\n    " + test.alias + ":\n      " + test.scope + "\n      types: [opened, labeled]\npermissions:\n  contents: read\nengine: copilot\n---\nInspect issues.\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			result := compileWorkflowFile(context.Background(), workflow.NewCompiler(), path, compileWorkflowFileOptions{jsonOutput: true})
			require.False(t, result.success)
			require.Empty(t, result.validationResult.WebhookSubscriptions)
			require.NoFileExists(t, filepath.Join(root, test.alias+".lock.yml"))
		})
	}
}

func TestCompileProviderIssueRequiresScope(t *testing.T) {
	for alias, field := range map[string]string{
		"jira-issue": "project",
	} {
		for _, wrapped := range []bool{false, true} {
			name := "missing"
			selector := ""
			if wrapped {
				name = "nested"
				selector = "      filters:\n        " + field + ": source-scope\n"
			}
			t.Run(alias+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				enableWebhookTriggers(t, root)
				path := filepath.Join(root, alias+".md")
				source := "---\non:\n  gateway-dispatch:\n    " + alias + ":\n      types: [opened]\n" + selector +
					"permissions:\n  contents: read\nengine: copilot\n---\nInspect issues.\n"
				require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
				result := compileWorkflowFile(context.Background(), workflow.NewCompiler(), path, compileWorkflowFileOptions{jsonOutput: true})
				require.False(t, result.success)
				require.Empty(t, result.validationResult.WebhookSubscriptions)
				_, err := os.Stat(filepath.Join(root, alias+".lock.yml"))
				require.True(t, os.IsNotExist(err), "invalid scopes must not produce a lock file")
			})
		}
	}
}
