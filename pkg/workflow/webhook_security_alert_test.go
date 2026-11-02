package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityAlertSubscriptions(t *testing.T) {
	tests := []struct {
		kind   string
		event  string
		filter string
		value  string
		path   []string
	}{
		{"code-scanning-alert", "code_scanning_alert", "severity", "error", []string{"alert", "rule", "severity"}},
		{"dependabot-alerts", "dependabot_alert", "severity", "critical", []string{"alert", "security_advisory", "severity"}},
	}
	require.Len(t, securityAlertTriggers, len(tests))
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			config := map[string]any{
				"types": []any{"created"},
			}
			if tt.filter == "severity" {
				config["severity"] = tt.value
			} else {
				config["filters"] = map[string]any{tt.filter: tt.value}
			}
			on := map[string]any{"gateway-dispatch": map[string]any{tt.kind: config}}
			before, err := json.Marshal(on)
			require.NoError(t, err)
			subscriptions, err := ParseWebhookSubscriptions(on, "security-agent")
			require.NoError(t, err)
			require.Len(t, subscriptions, 1)
			sub := subscriptions[0]
			assert.Equal(t, tt.event, sub.Event)
			assert.Equal(t, []string{"created"}, sub.Types)
			require.Len(t, sub.Conditions, 1)
			assert.Equal(t, tt.path, sub.Conditions[0].Path)
			assert.Equal(t, []any{tt.value}, sub.Conditions[0].Values)
			after, err := json.Marshal(on)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after))
			require.NoError(t, ValidateEventTypes(map[string]any{"on": on}))
			assert.Equal(t, []string{tt.event}, NormalizeGitHubAppWebhookEvents(on))

			on["gateway-dispatch"].(map[string]any)[tt.kind] = map[string]any{}
			all, err := ParseWebhookSubscriptions(on, "security-agent")
			require.NoError(t, err)
			catalog, err := WebhookCatalog()
			require.NoError(t, err)
			require.Len(t, all, 1)
			assert.Equal(t, tt.event, all[0].Event)
			assert.ElementsMatch(t, catalog[tt.event].Actions, all[0].Types)
			assert.Empty(t, all[0].Conditions)
			assert.True(t, matchWebhookContract(all[0], map[string]any{"action": "created"}))
			assert.False(t, matchWebhookContract(all[0], map[string]any{"action": "completed"}))

		})
	}
}

func TestSecurityAlertRejectsInvalidConfiguration(t *testing.T) {
	for alias := range securityAlertTriggers {
		for name, tt := range map[string]struct {
			config any
			want   string
		}{
			"null":                 {nil, "must be an object"},
			"scalar":               {"dependabot", "must be an object"},
			"removed sources":      {map[string]any{"sources": "dependabot"}, "property \"sources\""},
			"removed sources list": {map[string]any{"sources": []any{"code-scanning", "dependabot"}}, "property \"sources\""},
			"removed type":         {map[string]any{"type": "dependabot"}, "property \"type\""},
			"unsupported property": {map[string]any{"typo": true}, "property \"typo\""},
			"wrong action":         {map[string]any{"types": []any{"completed"}}, "unsupported activity type"},
			"secret action":        {map[string]any{"types": []any{"publicly_leaked"}}, "unsupported activity type"},
			"empty actions":        {map[string]any{"types": []any{}}, "non-empty list"},
			"unsupported filter":   {map[string]any{"filters": map[string]any{"not-a-filter": true}}, "unsupported filter"},
			"nested severity":      {map[string]any{"filters": map[string]any{"severity": "high"}}, "use on.gateway-dispatch." + alias + ".severity beside types"},
			"wrong severity type":  {map[string]any{"severity": true}, "non-empty string"},
			"empty severity":       {map[string]any{"severity": ""}, "non-empty string"},
			"invalid severity":     {map[string]any{"severity": "not-a-severity"}, "not one of"},
			"secret material":      {map[string]any{"filters": map[string]any{"secret": "not-a-secret"}}, "unsupported filter"},
		} {
			t.Run(alias+"/"+name, func(t *testing.T) {
				_, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{alias: tt.config}}, "security-agent")
				require.ErrorContains(t, err, tt.want)
				assert.Contains(t, err.Error(), "on.gateway-dispatch."+alias)
			})
		}
		for _, shorthand := range []any{alias, []any{"push", alias}, map[string]any{alias: map[string]any{}}} {
			_, err := ParseWebhookSubscriptions(shorthand, "security-agent")
			require.ErrorContains(t, err, "on.gateway-dispatch")
		}
	}
}

func TestSecurityAlertRepositorySelectors(t *testing.T) {
	for name, event := range securityAlertTriggers {
		t.Run(name, func(t *testing.T) {
			on := map[string]any{"gateway-dispatch": map[string]any{"repos": []any{"Foo/*", "bar/baz", "BAR/BAZ", "foo/*"}, name: map[string]any{}}}
			before, err := json.Marshal(on)
			require.NoError(t, err)
			subscriptions, err := ParseWebhookSubscriptions(on, "multi-repo")
			require.NoError(t, err)
			require.Len(t, subscriptions, 1, "each trigger selects only its own event family")
			for _, subscription := range subscriptions {
				assert.Equal(t, []string{"bar/baz", "foo/*"}, subscription.Repos)
				assert.Equal(t, "${{ github.repository }}", subscription.Dispatch.Repository)
				assert.Empty(t, subscription.Conditions, "repository selectors are source metadata, not payload equality predicates")
			}
			assert.Equal(t, []string{event}, NormalizeGitHubAppWebhookEvents(on))
			require.NoError(t, ValidateEventTypes(map[string]any{"on": on}))
			after, err := json.Marshal(on)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after), "parsing must preserve source frontmatter")
		})
	}
}

func TestSecurityAlertRejectsOtherFamilySelectors(t *testing.T) {
	for alias, configs := range map[string][]map[string]any{
		"code-scanning-alert": {
			{"types": []any{"dismissed"}},
			{"severity": "critical"},
			{"filters": map[string]any{"ecosystem": "npm"}},
		},
		"dependabot-alerts": {
			{"types": []any{"appeared_in_branch"}},
			{"severity": "error"},
			{"filters": map[string]any{"rule_id": "rule-id"}},
		},
	} {
		for _, config := range configs {
			_, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{alias: config}}, "wrong-family")
			require.ErrorContains(t, err, "on.gateway-dispatch."+alias)
		}
	}
}

func TestSecurityAlertRejectsInvalidRepositorySelectors(t *testing.T) {
	for _, repos := range []any{
		nil, "bar/baz", true, []any{}, []string{}, []any{7}, []any{"bar/baz", true},
		[]any{""}, []any{" "}, []any{"*"}, []any{"*/*"}, []any{"*/baz"},
		[]any{"bar/**"}, []any{"bar/ba*"}, []any{"bar/?az"}, []any{"bar/[baz]"},
		[]any{"bar"}, []any{"/baz"}, []any{"bar/"}, []any{"bar/baz/other"},
		[]any{"https://github.com/bar/baz"},
		[]any{"bar/baz "}, []any{" bar/baz"}, []any{"bar/baz\n"}, []any{"bar/."}, []any{"bar/.."},
	} {
		t.Run(fmt.Sprint(repos), func(t *testing.T) {
			_, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{"repos": repos, "dependabot-alerts": map[string]any{}}}, "invalid-repos")
			require.ErrorContains(t, err, "on.gateway-dispatch.repos")
			assert.Contains(t, err.Error(), "owner/repo or owner/*")
		})
	}
}

func TestSecurityAlertRejectsRepositoryExpressions(t *testing.T) {
	for name, repos := range map[string]any{
		"untyped list": []any{"${{ github.repository }}"},
		"typed list":   []string{"${{ github.repository }}"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{"repos": repos, "dependabot-alerts": map[string]any{}}}, "expression-repos")
			require.EqualError(t, err, "on.gateway-dispatch.repos[0]: GitHub Actions expressions are not supported in on.gateway-dispatch; keys and values must be literals (for example, repos: [github/gh-aw])")
		})
	}
}

func TestWebhookRepositoriesAreSharedOnlyWithRepositoryAwareEvents(t *testing.T) {
	_, err := ParseWebhookSubscriptions(map[string]any{"repos": []any{"bar/baz"}}, "missing-gateway")
	require.ErrorContains(t, err, "under on.gateway-dispatch")

	on := map[string]any{"gateway-dispatch": map[string]any{
		"repos":               []any{"Bar/Baz", "foo/*"},
		"orgs":                []any{"GitHub", "MY-*", "github"},
		"projects-items":      map[string]any{"project_id": "PVT_example"},
		"code-scanning-alert": map[string]any{},
		"dependabot-alerts":   map[string]any{},
		"jira-issue":          map[string]any{"project": "APP"},
		"linear-issue":        map[string]any{"organization-id": "org"},
	}}
	before, err := json.Marshal(on)
	require.NoError(t, err)
	subscriptions, err := ParseWebhookSubscriptions(on, "shared-repos")
	require.NoError(t, err)
	require.Len(t, subscriptions, 6)
	for _, subscription := range subscriptions {
		switch subscription.Event {
		case "projects_v2_item", "code_scanning_alert", "dependabot_alert":
			assert.Equal(t, []string{"github", "my-*"}, subscription.Orgs)
		default:
			assert.Empty(t, subscription.Orgs, "provider scopes are not GitHub organization names")
		}
		switch subscription.Event {
		case "code_scanning_alert", "dependabot_alert":
			assert.Equal(t, []string{"bar/baz", "foo/*"}, subscription.Repos)
		default:
			assert.Empty(t, subscription.Repos, "non-repository events keep their own required source scopes")
		}
	}
	after, err := json.Marshal(on)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "shared repository normalization must not mutate source frontmatter")
	_, err = ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{"repos": []any{"bar/baz"}, "projects-items": map[string]any{"project_id": "PVT_example"}}}, "projects-only")
	require.NoError(t, err)
}

func TestWebhookOrganizationSelectors(t *testing.T) {
	for _, orgs := range []any{
		[]any{"*"}, []any{"GitHub", "my-*", "*-labs", "a*b", "g**b"},
		[]string{"GITHUB", "github", "MY-*"},
	} {
		on := map[string]any{"gateway-dispatch": map[string]any{
			"orgs": orgs, "projects-items": map[string]any{"project_id": "PVT_example"},
			"code-scanning-alert": map[string]any{},
			"dependabot-alerts":   map[string]any{},
		}}
		before, err := json.Marshal(on)
		require.NoError(t, err)
		subscriptions, err := ParseWebhookSubscriptions(on, "orgs")
		require.NoError(t, err)
		require.Len(t, subscriptions, 3)
		for _, subscription := range subscriptions {
			assert.NotEmpty(t, subscription.Orgs)
		}
		after, err := json.Marshal(on)
		require.NoError(t, err)
		assert.JSONEq(t, string(before), string(after))
	}
	for _, orgs := range []any{
		nil, "github", true, []any{}, []any{true}, []any{""}, []any{" "},
		[]any{"github/repo"}, []any{"github/*"}, []any{" github"}, []any{"github\n"},
		[]any{"-github"}, []any{"github-"}, []any{"foo_bar"}, []any{"foo.bar"},
		[]any{"foo?"}, []any{"[org]"}, []any{"${{ github.repository_owner }}"},
		[]any{"abcdefghijklmnopqrstuvwxyzabcdefghijklmn"},
	} {
		_, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{"orgs": orgs, "dependabot-alerts": map[string]any{}}}, "invalid-orgs")
		require.ErrorContains(t, err, "on.gateway-dispatch.orgs")
	}
	_, err := ParseWebhookSubscriptions(map[string]any{"orgs": []any{"github"}}, "flat-orgs")
	require.ErrorContains(t, err, "under on.gateway-dispatch")
	_, err = ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{"orgs": []any{"github"}}}, "no-trigger")
	require.ErrorContains(t, err, "at least one nested gateway trigger")
}

func TestSecurityAlertOmittedSelectors(t *testing.T) {
	for name, event := range securityAlertTriggers {
		subscriptions, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{name: map[string]any{}}}, "defaults")
		require.NoError(t, err)
		require.Len(t, subscriptions, 1)
		assert.Equal(t, event, subscriptions[0].Event)
		for _, subscription := range subscriptions {
			assert.Nil(t, subscription.Repos, "omission does not introduce a new selector or implicit wildcard")
			encoded, err := json.Marshal(subscription)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), `"repos"`)
		}
		subscriptions, err = ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{"repos": []string{"bar/baz"}, name: map[string]any{}}}, "explicit-source")
		require.NoError(t, err)
		require.Len(t, subscriptions, 1)
		assert.Equal(t, event, subscriptions[0].Event)
		assert.Equal(t, []string{"bar/baz"}, subscriptions[0].Repos)
		_, err = ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{name: map[string]any{"repos": []string{"bar/baz"}}}}, "local-repos")
		require.ErrorContains(t, err, "on.gateway-dispatch."+name+".repos is not supported")
	}
}

func TestSecurityAlertIndependentSubscriptions(t *testing.T) {
	on := map[string]any{"gateway-dispatch": map[string]any{
		"code-scanning-alert": map[string]any{"types": []any{"appeared_in_branch"}, "severity": "error", "filters": map[string]any{"rule_id": "rule-id"}},
		"dependabot-alerts":   map[string]any{"types": []any{"dismissed"}, "severity": "critical", "filters": map[string]any{"ecosystem": "npm"}},
	}}
	before, err := json.Marshal(on)
	require.NoError(t, err)
	subscriptions, err := ParseWebhookSubscriptions(on, "security-agent")
	require.NoError(t, err)
	require.Len(t, subscriptions, 2)
	expected := []string{"code_scanning_alert", "dependabot_alert"}
	for i, subscription := range subscriptions {
		assert.Equal(t, expected[i], subscription.Event)
		assert.Equal(t, subscriptions[0].Dispatch.EventType, subscription.Dispatch.EventType)
	}
	assert.Equal(t, []string{"appeared_in_branch"}, subscriptions[0].Types)
	assert.Equal(t, []string{"dismissed"}, subscriptions[1].Types)
	require.Len(t, subscriptions[0].Conditions, 2)
	require.Len(t, subscriptions[1].Conditions, 2)
	assert.Equal(t, "rule_id", subscriptions[0].Conditions[0].Filter)
	assert.Equal(t, []any{"error"}, subscriptions[0].Conditions[1].Values)
	assert.Equal(t, "ecosystem", subscriptions[1].Conditions[0].Filter)
	assert.Equal(t, []any{"critical"}, subscriptions[1].Conditions[1].Values)
	assert.Equal(t, expected, NormalizeGitHubAppWebhookEvents(on))
	after, err := json.Marshal(on)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
	var output bytes.Buffer
	NewCompiler().emitExperimentalFeatureWarningsTo(&WorkflowData{WebhookSubscriptions: subscriptions}, &output)
	assert.Contains(t, output.String(), "on.gateway-dispatch.code-scanning-alert (hosted CAO triggers)")
	assert.Contains(t, output.String(), "on.gateway-dispatch.dependabot-alerts (hosted CAO triggers)")
	assert.NotContains(t, output.String(), "on.gateway-dispatch.security-alerts")
}

func TestSecurityAlertRejectsBundledAndUnsupportedTriggers(t *testing.T) {
	for _, alias := range []string{"security-alert", "security-alerts", "dependabot-alert", "code-scanning-alerts", "secret-scanning-alert", "secret-scanning-alerts", "secret-scanning-scan", "secret-scanning-alert-location"} {
		for _, config := range []any{map[string]any{}, map[string]any{"sources": []any{"code-scanning", "dependabot"}}} {
			_, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{
				"dependabot-alerts": map[string]any{}, alias: config,
			}}, "removed-security")
			require.ErrorContains(t, err, "unsupported on.gateway-dispatch property")
		}
	}
}

func TestSecurityAlertSeverityAlternatives(t *testing.T) {
	subscriptions, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{"dependabot-alerts": map[string]any{"types": []any{"created"}, "severity": []any{"high", "critical"}}}}, "security-agent")
	require.NoError(t, err)
	require.Len(t, subscriptions, 1)
	require.Len(t, subscriptions[0].Conditions, 1)
	assert.Equal(t, []any{"critical", "high"}, subscriptions[0].Conditions[0].Values)
	for _, tt := range []struct {
		severity string
		match    bool
	}{{"critical", true}, {"high", true}, {"low", false}, {"HIGH", false}} {
		payload := map[string]any{"action": "created", "alert": map[string]any{
			"security_advisory": map[string]any{"severity": tt.severity},
		}}
		assert.Equal(t, tt.match, matchWebhookContract(subscriptions[0], payload))
	}
}
