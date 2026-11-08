//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookManifestCompilation(t *testing.T) {
	for name, trigger := range map[string]string{
		"native": "  workflow_dispatch:\n  repository_dispatch:\n    types: [manual]\n",
		"projects": `  gateway-dispatch:
    projects-items:
      project_id: [PVT_one, PVT_two]
      status-transitions:
        - {from: Todo, to: Ready}
        - {from: In progress, to: Done}
`,
		"security": `  gateway-dispatch:
    code-scanning-alert:
      types: [appeared_in_branch]
      severity: error
    dependabot-alerts:
      types: [created]
      severity: critical
      filters:
        alert_number: 42
`,
		"multi-repo": `  gateway-dispatch:
    repos: [bar/baz, "foo/*"]
    orgs: [BAR, "foo*"]
    code-scanning-alert: {}
    dependabot-alerts: {}
`,
		"combined": `  gateway-dispatch:
    projects-items:
      project_id: PVT_one
      types: [edited]
      status-transitions: [{from: Todo, to: "Ready\nfor review"}]
    dependabot-alerts:
      types: [created, reopened]
      severity: [high, critical]
`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			compiler := NewCompiler()
			compiler.gitRoot = root
			if name != "native" {
				writeWebhookTriggerConfig(t, root, `{"gateway-dispatch":true}`)
			}
			path := filepath.Join(root, name+".md")
			source := webhookWorkflow("on:\n" + trigger)
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			data, err := compiler.ParseWorkflowString(source, path)
			require.NoError(t, err)
			expected, err := json.Marshal(data.WebhookSubscriptions)
			require.NoError(t, err)
			require.NoError(t, compiler.CompileWorkflow(path))
			lock, err := os.ReadFile(stringutil.MarkdownToLockFile(path))
			require.NoError(t, err)
			manifest, err := ExtractGHAWManifestFromLockFile(string(lock))
			require.NoError(t, err)
			require.NotNil(t, manifest)
			assert.Equal(t, currentGHAWManifestVersion, manifest.Version)
			actual, err := json.Marshal(manifest.WebhookSubscriptions)
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(actual))
			var manifestLine string
			for line := range strings.SplitSeq(string(lock), "\n") {
				if strings.HasPrefix(line, "# gh-aw-manifest: ") {
					require.Empty(t, manifestLine, "the manifest must occupy exactly one line")
					manifestLine = line
				}
			}
			require.NotEmpty(t, manifestLine)
			if name == "native" {
				assert.NotContains(t, manifestLine, `"webhook_subscriptions"`)
			} else {
				assert.Contains(t, manifestLine, `"webhook_subscriptions":[`)
				assert.NotContains(t, manifestLine, "secret_scanning")
				if name == "security" || name == "multi-repo" {
					require.Len(t, manifest.WebhookSubscriptions, 2)
				}
				for _, subscription := range manifest.WebhookSubscriptions {
					assert.Equal(t, 2, subscription.SchemaVersion)
					assert.Equal(t, WebhookBatch{Mode: "same-event", DeduplicateBy: "delivery_id"}, subscription.Dispatch.Batch)
					assert.Equal(t, "github.event.client_payload.aw_context.webhook.deliveries", subscription.Dispatch.Context.Deliveries)
					assert.Equal(t, "aw_context.items", subscription.Dispatch.Context.Items)
					assert.Equal(t, WebhookDispatchEventType(name), subscription.Dispatch.EventType)
					assert.Equal(t, ".github/workflows/"+name+".lock.yml", subscription.Dispatch.Workflow)
					assert.Equal(t, "${{ github.repository }}", subscription.Dispatch.Repository)
					assert.Equal(t, 65535, subscription.Dispatch.MaxPayloadBytes)
					assert.Equal(t, 65535, subscription.Dispatch.MaxRequestBytes)
					if name == "multi-repo" {
						assert.Equal(t, []string{"bar/baz", "foo/*"}, subscription.Repos)
						assert.Equal(t, []string{"bar", "foo*"}, subscription.Orgs)
					} else {
						assert.Empty(t, subscription.Repos)
						assert.NotContains(t, manifestLine, `"repos"`)
					}
					require.NotNil(t, subscription.Dispatch.PayloadProjection)
					assert.Equal(t, WebhookPayloadProjectionPolicy, subscription.Dispatch.PayloadProjection.Policy)
				}
			}
			encoded, err := manifest.ToJSON()
			require.NoError(t, err)
			roundtrip, err := ExtractGHAWManifestFromLockFile("# gh-aw-manifest: " + encoded + "\n")
			require.NoError(t, err)
			assert.Equal(t, manifest, roundtrip)
		})
	}
}

func TestWebhookManifestSerializationFailure(t *testing.T) {
	var header strings.Builder
	data := &WorkflowData{WebhookSubscriptions: []WebhookSubscription{{
		Conditions: []WebhookCondition{{Values: []any{make(chan string)}}},
	}}}
	err := NewCompiler().generateManifestHeader(&header, data, nil, nil)
	require.ErrorContains(t, err, "cannot generate gh-aw-manifest header")
	assert.Empty(t, header.String(), "compilation must not silently omit an unserializable declaration")
}

func TestWorkQueueMetadataManifestCompilation(t *testing.T) {
	const contract = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := &WorkflowData{
		Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
		WorkQueuePolicy: &WorkQueuePolicyConfig{
			WorkerContract: contract,
		},
	}

	var header strings.Builder
	require.NoError(t, NewCompiler().generateManifestHeader(&header, data, nil, nil))
	manifest, err := ExtractGHAWManifestFromLockFile(header.String())
	require.NoError(t, err)
	require.NotNil(t, manifest.WorkQueue)
	assert.True(t, manifest.WorkQueue.Worker)
	assert.Equal(t, contract, manifest.WorkQueue.LogicalContract)
}
