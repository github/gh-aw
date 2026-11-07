//go:build !integration

package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
)

func TestDocumentPermissionComments(t *testing.T) {
	source := `# Agent permissions
permissions: # Agent context
  issues: read # Read the triggering issue
  contents: read
on:
  permissions:
    # Read run metadata
    actions: read
jobs:
  custom:
    permissions: {checks: write} # Publish check results
  empty:
    permissions: {} # No token access
  shorthand:
    permissions: read-all # Inspect the repository
safe-outputs:
  jobs:
    publish:
      permissions:
        issues: write # Publish the report
`
	content := `permissions: {}
jobs:
  agent:
    permissions:
      contents: read
      issues: read
  pre_activation:
    permissions:
      actions: read
  custom:
    permissions:
      checks: write
      statuses: write
    steps:
      - run: |
          permissions:
            issues: write
  empty:
    permissions: {}
  shorthand:
    permissions: read-all
  publish:
    permissions:
      issues: write
  generated:
    permissions:
      actions: read
      contents: write
      id-token: write
      issues: none
`
	got, err := documentPermissionComments(content, source)
	require.NoError(t, err)
	for _, expected := range []string{
		"    # Agent permissions\n    permissions: # Agent context",
		"issues: read # Read the triggering issue",
		"      # Read run metadata\n      actions: read",
		"checks: write # Publish check results",
		"statuses: write # Create or update commit statuses.",
		"permissions: {} # No token access",
		"permissions: read-all # Inspect the repository",
		"issues: write # Publish the report",
		"actions: read # Read workflow runs and artifacts for workflow context.",
		"contents: write # Create or update repository contents.",
		"id-token: write # Request an OIDC token for authenticated services.",
		"issues: none # Deny access to issues.",
		"          permissions:\n            issues: write\n",
	} {
		require.Contains(t, got, expected)
	}
	require.NotContains(t, got, "contents: read #")
	var before, after map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(content), &before))
	require.NoError(t, yaml.Unmarshal([]byte(got), &after))
	require.Equal(t, before, after, "documentation must not change permissions or step contents")
}

func TestGeneratedPermissionScopeComments(t *testing.T) {
	for _, level := range []PermissionLevel{PermissionRead, PermissionWrite, PermissionNone} {
		for _, scope := range append(GetAllPermissionScopes(), PermissionCopilotRequests) {
			if scope == PermissionMetadata || scope == PermissionIdToken && level == PermissionRead {
				continue
			}
			t.Run(fmt.Sprintf("%s/%s", scope, level), func(t *testing.T) {
				content := fmt.Sprintf("jobs:\n  generated:\n    permissions:\n      %s: %s\n", scope, level)
				got, err := documentPermissionComments(content, "")
				require.NoError(t, err)
				if scope == PermissionContents && level == PermissionRead {
					require.Equal(t, content, got)
				} else {
					require.Contains(t, got, fmt.Sprintf("%s: %s # ", scope, level))
				}
			})
		}
	}
}

func TestPermissionCommentsStayWithTheirJob(t *testing.T) {
	source := `jobs:
    first:
        permissions:
            issues: read # First issue lookup
    second:
        permissions:
            issues: write # Second issue update
`
	content := "jobs:\n  first:\n    permissions:\n      issues: read\n  second:\n    permissions:\n      issues: write\n"
	got, err := documentPermissionComments(content, source)
	require.NoError(t, err)
	require.Contains(t, got, "issues: read # First issue lookup")
	require.Contains(t, got, "issues: write # Second issue update")
	require.Equal(t, 1, strings.Count(got, "# First issue lookup"))
	require.Equal(t, 1, strings.Count(got, "# Second issue update"))
}

func TestPermissionCommentsWithOverridesAndNormalizedSafeJobs(t *testing.T) {
	source := `permissions:
  issues: read # Agent issue lookup
jobs:
  agent:
    permissions:
      contents: read # Checkout the repository
safe-outputs:
  jobs:
    publish-report:
      permissions:
        issues: write # Publish the report
`
	content := "jobs:\n  agent:\n    permissions:\n      contents: read\n      issues: read\n  publish_report:\n    permissions:\n      issues: write\n"
	got, err := documentPermissionComments(content, source)
	require.NoError(t, err)
	require.Contains(t, got, "contents: read # Checkout the repository")
	require.Contains(t, got, "issues: read # Agent issue lookup")
	require.Contains(t, got, "issues: write # Publish the report")
}

func TestEmptyPermissionCommentGetsReason(t *testing.T) {
	content := "jobs:\n  agent:\n    permissions:\n      issues: read\n"
	got, err := documentPermissionComments(content, "permissions:\n  issues: read #\n")
	require.NoError(t, err)
	require.Contains(t, got, "# Read issues and comments for workflow context.")
}

func TestAuxiliaryWorkflowPermissionComments(t *testing.T) {
	ctx := context.Background()
	maintenance, err := buildMaintenanceWorkflowYAML(ctx, buildMaintenanceWorkflowYAMLOptions{
		cronSchedule: "37 0 * * *", scheduleDesc: "Daily", runsOnValue: "ubuntu-latest",
		actionMode: ActionModeDev, version: "dev", defaultBranch: "main",
		hasCacheMemory: true, hasSteering: true, hasWorkQueue: true,
		compactionLedgers: []LedgerConfig{{Name: "events"}},
	})
	require.NoError(t, err)
	sideRepo, err := buildSideRepoMaintenanceWorkflowYAML(newSideRepoMaintenanceRenderContext(ctx, generateSideRepoMaintenanceWorkflowOptions{
		target: SideRepoTarget{Repository: "owner/repo"}, actionMode: ActionModeDev,
		version: "dev", runsOnValue: "ubuntu-latest", hasExpires: true, minExpiresDays: 7,
	}))
	require.NoError(t, err)
	commands, err := buildCentralSlashCommandWorkflowYAML(nil, nil, map[string]map[string]struct{}{
		"issue_comment": {}, "pull_request": {}, "discussion": {},
	}, "ubuntu-latest", "./actions/setup", nil, true, false)
	require.NoError(t, err)
	queue, err := buildWorkQueueEventWorkflowYAML(ActionModeDev, "./actions/setup", nil, false)
	require.NoError(t, err)
	update, err := buildAutoUpdateWorkflowYAML("37 0 * * *", "./actions/setup", getActionPin("actions/github-script"), "", "gh aw", true, nil, false)
	require.NoError(t, err)
	for name, content := range map[string]string{"maintenance": maintenance, "side-repo": sideRepo, "commands": commands, "queue": queue, "update": update} {
		t.Run(name, func(t *testing.T) {
			var document yamlv3.Node
			require.NoError(t, yamlv3.Unmarshal([]byte(content), &document))
			requireDocumentedPermissionScopes(t, permissionCommentEntry(document.Content[0], "permissions").value)
			jobs := permissionCommentEntry(document.Content[0], "jobs").value
			require.NotNil(t, jobs)
			for i := 0; i+1 < len(jobs.Content); i += 2 {
				perms := permissionCommentEntry(jobs.Content[i+1], "permissions").value
				if perms == nil {
					continue
				}
				requireDocumentedPermissionScopes(t, perms)
			}

		})
	}
}

func requireDocumentedPermissionScopes(t *testing.T, perms *yamlv3.Node) {
	t.Helper()
	require.NotNil(t, perms)
	for j := 0; j+1 < len(perms.Content); j += 2 {
		scope := permissionCommentSource{key: perms.Content[j], value: perms.Content[j+1]}
		if scope.key.Value != "contents" || scope.value.Value != "read" {
			require.True(t, hasPermissionScopeComment(scope), "%s needs a reason", scope.key.Value)
		}
	}
}
