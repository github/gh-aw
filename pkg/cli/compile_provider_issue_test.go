//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileProviderIssueJSONContract(t *testing.T) {
	for _, alias := range []string{"jira-issue", "linear-issue"} {
		t.Run(alias, func(t *testing.T) {
			root := t.TempDir()
			enableWebhookTriggers(t, root)
			path := filepath.Join(root, alias+".md")
			scope := map[string]string{
				"jira-issue": "project: APP", "linear-issue": "organization-id: organization-uuid\n      team-id: team-uuid",
			}[alias]
			source := "---\non:\n  gateway-dispatch:\n    " + alias + ":\n      " + scope + "\n      types: [opened, labeled]\npermissions:\n  contents: read\nengine: copilot\n---\nInspect every issue in github.aw.context.items.\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			result := compileWorkflowFile(context.Background(), workflow.NewCompiler(), path, compileWorkflowFileOptions{jsonOutput: true})
			require.True(t, result.success, "%+v", result.validationResult.Errors)
			require.Len(t, result.validationResult.WebhookSubscriptions, 1)
			sub := result.validationResult.WebhookSubscriptions[0]
			require.NotNil(t, sub.ActivityNormalization)
			assert.NotEmpty(t, sub.Provider)
			assert.NotEmpty(t, sub.NativeEvents)
			require.NotEmpty(t, sub.Conditions, "the required source scope must be present in emitted subscriptions")
			if alias == "linear-issue" {
				require.Len(t, sub.Conditions, 2)
				assert.Equal(t, []string{"data", "teamId"}, sub.Conditions[1].Path)
				assert.Equal(t, []any{"team-uuid"}, sub.Conditions[1].Values)
			}
			output, err := json.Marshal(result.validationResult)
			require.NoError(t, err)
			assert.Contains(t, string(output), `"activity_normalization":`)
			lock, err := os.ReadFile(filepath.Join(root, alias+".lock.yml"))
			require.NoError(t, err)
			manifest, err := workflow.ExtractGHAWManifestFromLockFile(string(lock))
			require.NoError(t, err)
			require.Len(t, manifest.WebhookSubscriptions, 1)
			assert.Equal(t, sub, manifest.WebhookSubscriptions[0])
		})
	}
}

func TestCompileProviderIssueRequiresScope(t *testing.T) {
	for alias, field := range map[string]string{
		"jira-issue": "project", "linear-issue": "organization-id",
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
