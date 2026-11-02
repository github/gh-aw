//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func providerIssueOn(alias string, config any) map[string]any {
	if settings, ok := config.(map[string]any); ok {
		settings = maps.Clone(settings)
		scope := map[string]string{
			"jira-issue": "project", "linear-issue": "organization-id",
		}[alias]
		if _, exists := settings[scope]; !exists {
			settings[scope] = "source-scope"
		}
		config = settings
	}
	return map[string]any{"gateway-dispatch": map[string]any{alias: config}}
}

func providerIssueScopeYAML(alias string) string {
	return map[string]string{
		"jira-issue":   "      project: APP\n",
		"linear-issue": "      organization-id: organization-uuid\n",
	}[alias]
}

func TestProviderIssueSubscriptions(t *testing.T) {
	for alias, event := range providerIssueTriggers {
		t.Run(alias, func(t *testing.T) {
			config := map[string]any{"types": []any{"labeled", "opened", "opened"}}
			on := providerIssueOn(alias, config)
			before, err := json.Marshal(on)
			require.NoError(t, err)
			subscriptions, err := ParseWebhookSubscriptions(on, "provider-agent")
			require.NoError(t, err)
			require.Len(t, subscriptions, 1)
			sub := subscriptions[0]
			assert.Equal(t, event, sub.Event)
			assert.Equal(t, []string{"labeled", "opened"}, sub.Types)
			require.NotEmpty(t, sub.Provider)
			require.NotNil(t, sub.ActivityNormalization)
			assert.Equal(t, sub.Provider+"-issue/v1", sub.ActivityNormalization.Policy)
			assert.Len(t, sub.NativeEvents, 2)
			assert.Equal(t, 2, sub.SchemaVersion)
			assert.Equal(t, WebhookDispatchEventType("provider-agent"), sub.Dispatch.EventType)
			require.NotNil(t, sub.Dispatch.PayloadProjection)
			assert.Equal(t, WebhookPayloadProjectionPolicy, sub.Dispatch.PayloadProjection.Policy)
			assert.Empty(t, sub.Repos)
			assert.Empty(t, NormalizeGitHubAppWebhookEvents(on), "external providers must not become GitHub App webhook requirements")
			after, err := json.Marshal(on)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after), "subscription parsing must not mutate source configuration")
		})
	}
}

func TestProviderIssueMixedGatewayRequirements(t *testing.T) {
	on := providerIssueOn("jira-issue", map[string]any{})
	gateway := on["gateway-dispatch"].(map[string]any)
	gateway["linear-issue"] = map[string]any{"organization-id": "organization-uuid"}
	gateway["issue"] = map[string]any{"types": []any{"opened"}}
	gateway["dependabot-alerts"] = map[string]any{}
	gateway["projects-items"] = map[string]any{"project_id": "PVT_example"}
	assert.Equal(t, []string{"dependabot_alert", "issues", "projects_v2_item"}, NormalizeGitHubAppWebhookEvents(on))
	assert.Equal(t, map[string]string{"issues": "read", "organization_projects": "read", "vulnerability_alerts": "read"}, ComputeGitHubAppManifestWebhookPermissions(on))
	on["issues"] = map[string]any{"types": []any{"opened"}}
	assert.Empty(t, NormalizeGitHubAppWebhookEvents(on))
	assert.Empty(t, ComputeGitHubAppManifestWebhookPermissions(on))
}

func TestProviderIssueExperimentalNotice(t *testing.T) {
	for alias, event := range providerIssueTriggers {
		t.Run(alias, func(t *testing.T) {
			subscriptions, err := ParseWebhookSubscriptions(providerIssueOn(alias, map[string]any{}), "notice")
			require.NoError(t, err)
			var output bytes.Buffer
			NewCompiler().emitExperimentalFeatureWarningsTo(&WorkflowData{WebhookSubscriptions: subscriptions}, &output)
			assert.Contains(t, output.String(), "Using experimental feature: on.gateway-dispatch."+alias+" (approved webhook bridge required)")
			assert.NotContains(t, output.String(), "on.gateway-dispatch.code-scanning-alert")
			assert.NotContains(t, output.String(), "on.gateway-dispatch.dependabot-alerts")
			assert.Equal(t, event, subscriptions[0].Event)
		})
	}
	on := map[string]any{"gateway-dispatch": map[string]any{}}
	for alias := range providerIssueTriggers {
		on["gateway-dispatch"].(map[string]any)[alias] = providerIssueOn(alias, map[string]any{})["gateway-dispatch"].(map[string]any)[alias]
	}
	subscriptions, err := ParseWebhookSubscriptions(on, "all-providers")
	require.NoError(t, err)
	var output bytes.Buffer
	NewCompiler().emitExperimentalFeatureWarningsTo(&WorkflowData{WebhookSubscriptions: subscriptions}, &output)
	for alias := range providerIssueTriggers {
		assert.Equal(t, 1, strings.Count(output.String(), "Using experimental feature: on.gateway-dispatch."+alias+" ("))
	}
}

func TestProviderIssueValidation(t *testing.T) {
	for alias := range providerIssueTriggers {
		for name, config := range map[string]any{
			"null":            nil,
			"scalar":          true,
			"unknown key":     map[string]any{"typo": true},
			"native activity": map[string]any{"types": []any{"created"}},
			"misspelling":     map[string]any{"types": []any{"labelled"}},
			"empty types":     map[string]any{"types": []any{}},
			"GitHub repos":    map[string]any{"repos": []any{"owner/repo"}},
			"filters wrapper": map[string]any{"filters": map[string]any{"project": "APP"}},
			"unknown filter":  map[string]any{"repository": "owner/repo"},
			"expression":      map[string]any{"sender": "${{ github.actor }}"},
		} {
			t.Run(alias+"/"+name, func(t *testing.T) {
				_, err := ParseWebhookSubscriptions(providerIssueOn(alias, config), "invalid")
				require.Error(t, err)
			})
		}
		for _, on := range []any{alias, []any{"push", alias}, map[string]any{alias: map[string]any{}}} {
			_, err := ParseWebhookSubscriptions(on, "ungated")
			require.Error(t, err)
		}
	}
}

func TestProviderIssuePayloadFilters(t *testing.T) {
	for alias, test := range map[string]struct {
		filters map[string]any
		field   string
		path    []string
	}{
		"jira-issue":   {map[string]any{"labels": []any{"bug", "urgent"}}, "labels", []string{"issue", "fields", "labels", "*"}},
		"linear-issue": {map[string]any{"labels": "label-uuid"}, "labels", []string{"data", "labelIds", "*"}},
	} {
		t.Run(alias, func(t *testing.T) {
			config := maps.Clone(test.filters)
			config["types"] = []any{"edited"}
			subscriptions, err := ParseWebhookSubscriptions(providerIssueOn(alias, config), "provider")
			require.NoError(t, err)
			require.Len(t, subscriptions[0].Conditions, 2)
			conditions := map[string]WebhookCondition{}
			for _, condition := range subscriptions[0].Conditions {
				conditions[condition.Filter] = condition
			}
			require.Contains(t, conditions, test.field)
			assert.Equal(t, test.path, conditions[test.field].Path)
			assert.Equal(t, "string", conditions[test.field].ValueType)
		})
	}
}

func TestCompileProviderIssueManifest(t *testing.T) {
	for alias, event := range providerIssueTriggers {
		t.Run(alias, func(t *testing.T) {
			source := webhookWorkflow("on:\n  bots: [cao-gateway]\n  gateway-dispatch:\n    " + alias + ":\n" + providerIssueScopeYAML(alias) + "      types: [opened, labeled]\n")
			compiler := newWebhookCompiler()
			path := filepath.Join(t.TempDir(), alias+".md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			data, err := compiler.ParseWorkflowString(source, path)
			require.NoError(t, err)
			assert.Empty(t, ComputeGitHubAppManifestWebhookPermissions(providerIssueOn(alias, map[string]any{})), "provider events do not add GitHub App permissions")
			var triggers map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(data.On), &triggers))
			on := triggers["on"].(map[string]any)
			assert.NotContains(t, on, alias)
			assert.NotContains(t, on, "workflow_dispatch")
			assert.NotContains(t, on, "gateway-dispatch")
			assert.Contains(t, on, "repository_dispatch")
			require.NoError(t, compiler.CompileWorkflow(path))
			lock, err := os.ReadFile(stringutil.MarkdownToLockFile(path))
			require.NoError(t, err)
			output := string(lock)
			assert.Contains(t, extractJobSection(output, "gateway_provenance"), "check_gateway_provenance.cjs")
			assert.Contains(t, output, "validate_webhook.cjs")
			manifest, err := ExtractGHAWManifestFromLockFile(output)
			require.NoError(t, err)
			require.NotNil(t, manifest)
			require.Len(t, manifest.WebhookSubscriptions, 1)
			assert.Equal(t, event, manifest.WebhookSubscriptions[0].Event)
			assert.Equal(t, data.WebhookSubscriptions, manifest.WebhookSubscriptions)
		})
	}
}

func TestProviderIssueAuthorizationAndGates(t *testing.T) {
	for alias := range providerIssueTriggers {
		for name, test := range map[string]struct {
			enabled bool
			flag    string
			roles   string
			want    string
		}{
			"repository opt-in":  {false, "  gateway-dispatch:\n", "", `{"gateway-dispatch":true}`},
			"workflow namespace": {true, "", "", alias},
			"false workflow":     {true, "  gateway-dispatch: false\n", "", "gateway-dispatch"},
			"weak role":          {true, "  gateway-dispatch:\n", "  roles: [read]\n", "write access"},
			"mixed weak role":    {true, "  gateway-dispatch:\n", "  roles: [write, triage]\n", "write access"},
		} {
			t.Run(alias+"/"+name, func(t *testing.T) {
				compiler := newWebhookCompiler()
				compiler.repoConfig.GatewayDispatch = test.enabled
				indent := "    "
				if test.flag != "  gateway-dispatch:\n" {
					indent = "  "
				}
				source := webhookWorkflow("strict: false\non:\n" + test.roles + test.flag + indent + alias + ":\n" + providerIssueScopeYAML(alias) + "      types: [opened]\n")
				_, err := compiler.ParseWorkflowString(source, "provider-gate.md")
				require.ErrorContains(t, err, test.want)
			})
		}
	}
}

func TestProviderIssueRequiredScopes(t *testing.T) {
	for alias, test := range map[string]struct {
		field string
		path  []string
	}{
		"jira-issue":   {"project", []string{"issue", "fields", "project", "key"}},
		"linear-issue": {"organization-id", []string{"organizationId"}},
	} {
		t.Run(alias, func(t *testing.T) {
			_, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{alias: map[string]any{}}}, "missing")
			require.ErrorContains(t, err, "on.gateway-dispatch."+alias+"."+test.field+" is required")
			for _, value := range []any{nil, "", " ", 1, []any{}, []any{"scope", ""}, []any{false}, "${{ github.actor }}"} {
				config := map[string]any{test.field: value}
				_, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{alias: config}}, "invalid")
				require.Error(t, err, "scope value: %#v", value)
			}
			config := map[string]any{test.field: []any{"scope-b", "scope-a", "scope-a"}, "types": []any{"opened"}}
			subscriptions, err := ParseWebhookSubscriptions(providerIssueOn(alias, config), "scoped")
			require.NoError(t, err)
			require.Len(t, subscriptions[0].Conditions, 1)
			assert.Equal(t, test.path, subscriptions[0].Conditions[0].Path)
			assert.Equal(t, []any{"scope-a", "scope-b"}, subscriptions[0].Conditions[0].Values)
		})
	}
}

func TestLinearIssueTeamID(t *testing.T) {
	config := map[string]any{"organization-id": "organization-uuid", "team-id": []any{"team-b", "team-a"}, "types": []any{"opened"}}
	on := providerIssueOn("linear-issue", config)
	subscriptions, err := ParseWebhookSubscriptions(on, "linear-team")
	require.NoError(t, err)
	require.Len(t, subscriptions[0].Conditions, 2)
	assert.Equal(t, WebhookCondition{
		Filter: "team-id", Path: []string{"data", "teamId"}, Operator: "equals", ValueType: "string", Values: []any{"team-a", "team-b"},
	}, subscriptions[0].Conditions[1])
	assert.Equal(t, config, on["gateway-dispatch"].(map[string]any)["linear-issue"], "normalization must not mutate source selectors")
	for _, value := range []any{nil, "", " ", 1, []any{}, "${{ secrets.LINEAR_TEAM_ID }}"} {
		config["team-id"] = value
		_, err := ParseWebhookSubscriptions(providerIssueOn("linear-issue", config), "invalid-team")
		require.Error(t, err, "team-id: %#v", value)
	}
}

func TestProviderIssueCatalogNativeMappings(t *testing.T) {
	catalog, err := WebhookCatalog()
	require.NoError(t, err)
	for alias, event := range providerIssueTriggers {
		t.Run(alias, func(t *testing.T) {
			definition := catalog[event]
			require.NotEmpty(t, definition.Provider)
			assert.True(t, strings.HasPrefix(definition.Description, "Experimental. "), "every provider catalog description must mark its trigger experimental")
			assert.Len(t, definition.NativeEvents, len(definition.Actions))
			for _, activity := range definition.Actions {
				require.NotEmpty(t, definition.NativeEvents[activity], "every activity must identify a real provider event")
			}
		})
	}
}
