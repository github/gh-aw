//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderIssueSubscriptionsRejectRemovedNativeRoutes(t *testing.T) {
	for alias, config := range map[string]any{
		"jira-issue":         map[string]any{"project": "APP"},
		"linear-issue":       map[string]any{"organization-id": "organization-uuid"},
		"azure-devops-issue": map[string]any{"project_id": "project-uuid"},
	} {
		t.Run(alias, func(t *testing.T) {
			on := map[string]any{"gateway-dispatch": map[string]any{alias: config}}
			_, err := ParseWebhookSubscriptions(on, "provider")
			require.Error(t, err)
			assert.Empty(t, NormalizeGitHubAppWebhookEvents(on))
			assert.Empty(t, ComputeGitHubAppManifestWebhookPermissions(on))
		})
	}
	assert.Empty(t, providerIssueTriggers)
	catalog := map[string]WebhookEvent{}
	require.NoError(t, addProviderIssueCatalog(catalog))
	assert.Empty(t, catalog)
}

func TestProviderIssueRemovalPreservesGitHubGatewayRequirements(t *testing.T) {
	on := map[string]any{"gateway-dispatch": map[string]any{
		"issue":             map[string]any{"types": []any{"opened"}},
		"dependabot-alerts": map[string]any{},
		"projects-items":    map[string]any{"project_id": "PVT_example"},
	}}
	assert.Equal(t, []string{"dependabot_alert", "issues", "projects_v2_item"}, NormalizeGitHubAppWebhookEvents(on))
	assert.Equal(t, map[string]string{"issues": "read", "organization_projects": "read", "vulnerability_alerts": "read"}, ComputeGitHubAppManifestWebhookPermissions(on))
}
