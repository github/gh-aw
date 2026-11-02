//go:build !integration

package workflow

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeWebhookTriggerConfig(t *testing.T, root, content string) {
	t.Helper()
	if content == "" {
		return
	}
	path := filepath.Join(root, RepoConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestGatewayDispatchRejectsExpressions(t *testing.T) {
	for _, tt := range []struct {
		name, gateway, path string
	}{
		{"whole section", `"${{ vars.GATEWAY }}"`, ""},
		{"trigger key", `{"${{ vars.TRIGGER }}": {}}`, " property name"},
		{"nested key", `{issue: {filters: {"${{ vars.FILTER }}": triage}}}`, ".issue.filters property name"},
		{"repositories", `{repos: [github/gh-aw, "${{ github.repository }}"], issue: {}}`, ".repos[1]"},
		{"ignored repositories", `{repos: ["${{ github.repository }}"], jira-issue: {project: APP}}`, ".repos[0]"},
		{"organizations", `{orgs: ["${{ github.repository_owner }}"], issue: {}}`, ".orgs[0]"},
		{"ignored organizations", `{orgs: ["${{ github.repository_owner }}"], jira-issue: {project: APP}}`, ".orgs[0]"},
		{"project", `{projects-items: {project_id: "${{ vars.PROJECT }}"}}`, ".projects-items.project_id"},
		{"project list", `{projects-items: {project_id: [PVT_example, "${{ vars.PROJECT }}"]}}`, ".projects-items.project_id[1]"},
		{"activities", `{issue: {types: [opened, "${{ vars.ACTIVITY }}"]}}`, ".issue.types[1]"},
		{"code severity", `{code-scanning-alert: {severity: "${{ vars.SEVERITY }}"}}`, ".code-scanning-alert.severity"},
		{"dependabot severity", `{dependabot-alerts: {severity: "${{ vars.SEVERITY }}"}}`, ".dependabot-alerts.severity"},
		{"filter list", `{issue: {filters: {labels: [triage, "${{ vars.LABEL }}"]}}}`, ".issue.filters.labels[1]"},
		{"nested filter", `{projects-items: {project_id: PVT_example, filters: {field: {to: {name: "${{ vars.STATUS }}"}}}}}`, ".projects-items.filters.field.to.name"},
		{"transitions", `{projects-items: {project_id: PVT_example, status-transitions: [{from: Todo, to: "${{ vars.STATUS }}"}]}}`, ".projects-items.status-transitions[0].to"},
		{"Jira scope", `{jira-issue: {project: "${{ vars.PROJECT }}"}}`, ".jira-issue.project"},
		{"Linear scope", `{linear-issue: {organization-id: "${{ vars.ORGANIZATION }}"}}`, ".linear-issue.organization-id"},
		{"slash shorthand", `{slash-command: "${{ vars.COMMAND }}"}`, ".slash-command"},
		{"label names", `{label-command: {names: [review, "${{ vars.LABEL }}"]}}`, ".label-command.names[1]"},
		{"command events", `{slash-command: {events: ["${{ vars.EVENT }}"]}}`, ".slash-command.events[0]"},
		{"command strategy", `{slash-command: {strategy: "${{ vars.STRATEGY }}"}}`, ".slash-command.strategy"},
		{"label consumption", `{label-command: {name: review, remove_label: "${{ vars.REMOVE }}"}}`, ".label-command.remove_label"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "on:\n  gateway-dispatch: " + tt.gateway + "\n"
			var frontmatter map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(source), &frontmatter))
			subscriptions, err := ParseWebhookSubscriptions(frontmatter["on"], "expressions")
			require.ErrorContains(t, err, "GitHub Actions expressions are not supported")
			assert.Contains(t, err.Error(), "on.gateway-dispatch"+tt.path+":")
			assert.Nil(t, subscriptions)
			for _, strict := range []bool{true, false} {
				_, err := newWebhookCompiler().ParseWorkflowString(webhookWorkflow(fmt.Sprintf("strict: %t\n%s", strict, source)), "expressions.md")
				require.Error(t, err, "strict=%t", strict)
			}
		})
	}
}

func TestGatewayDispatchRejectsExpressionMarkers(t *testing.T) {
	for _, expression := range []string{
		"${{ vars.LABEL }}", "prefix-${{ github.actor }}-suffix", "${{\n github.repository \n}}",
		"${{", "${{ }}", "${{}}", "${{ 'literal' }}",
	} {
		for _, values := range []any{[]any{"triage", expression}, []string{"triage", expression}} {
			on := map[string]any{"gateway-dispatch": map[string]any{"issue": map[string]any{
				"filters": map[string]any{"labels": values},
			}}}
			_, err := ParseWebhookSubscriptions(on, "markers")
			require.ErrorContains(t, err, "on.gateway-dispatch.issue.filters.labels[1]")
			assert.Contains(t, err.Error(), "GitHub Actions expressions are not supported")
			assert.NotContains(t, err.Error(), expression, "diagnostics must not echo expression contents")
		}
	}
}

func TestGatewayDispatchPreservesOtherExpressions(t *testing.T) {
	compiler := newWebhookCompiler()
	source := webhookWorkflow("env:\n  REPO: ${{ github.repository }}\non:\n  gateway-dispatch:\n    issue: {}\n")
	data, err := compiler.ParseWorkflowString(source, "literal-gateway.md")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, "literal-gateway.md")
	require.NoError(t, err)
	var compiled struct {
		Env map[string]string `yaml:"env"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
	assert.Equal(t, "${{ github.repository }}", compiled.Env["REPO"])
}

func TestWebhookRequiresTriggerOptIn(t *testing.T) {
	for name, config := range map[string]string{
		"missing file":   "",
		"missing option": `{}`,
		"explicit false": `{"gateway-dispatch":false}`,
		"explicit true":  `{"gateway-dispatch":true}`,
	} {
		for trigger, on := range map[string]string{
			"project":    "on:\n  gateway-dispatch:\n    projects-items: {project_id: PVT_example}\n",
			"code":       "on:\n  gateway-dispatch:\n    code-scanning-alert: {}\n",
			"security":   "on:\n  gateway-dispatch:\n    dependabot-alerts: {}\n",
			"multi-repo": "on:\n  gateway-dispatch:\n    repos: [bar/baz, \"foo/*\"]\n    dependabot-alerts: {}\n",
		} {
			t.Run(name+"/"+trigger, func(t *testing.T) {
				compiler := NewCompiler()
				compiler.gitRoot = t.TempDir()
				writeWebhookTriggerConfig(t, compiler.gitRoot, config)
				data, err := compiler.ParseWorkflowString(webhookWorkflow(on), "cao-opt-in.md")
				if name == "explicit true" {
					require.NoError(t, err)
					_, err = compiler.CompileToYAML(data, "cao-opt-in.md")
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "hosted CAO server")
					assert.Contains(t, err.Error(), `{"gateway-dispatch":true}`)
					assert.Contains(t, err.Error(), RepoConfigFileName)
				}
				_, err = compiler.ParseWorkflowString(webhookWorkflow("on: workflow_dispatch\n"), "native.md")
				require.NoError(t, err, "native triggers do not require the opt-in")
			})
		}
	}
}

func TestWebhookOptInCannotBeBypassedByPreparsedData(t *testing.T) {
	data, err := newWebhookCompiler().ParseWorkflowString(webhookWorkflow("on:\n  gateway-dispatch:\n    projects-items: {project_id: PVT_example}\n"), "preparsed.md")
	require.NoError(t, err)
	compiler := NewCompiler()
	compiler.gitRoot = t.TempDir()
	_, err = compiler.CompileToYAML(data, "preparsed.md")
	require.ErrorContains(t, err, "hosted CAO server")

	compiler = NewCompiler(WithNoEmit(true))
	compiler.gitRoot = t.TempDir()
	err = compiler.CompileWorkflowData(data, filepath.Join(t.TempDir(), "preparsed.md"))
	require.ErrorContains(t, err, "hosted CAO server")
}

func TestWebhookMinimumRoles(t *testing.T) {
	for name, trigger := range map[string]string{
		"projects":   "projects-items: {project_id: PVT_example}",
		"code":       "code-scanning-alert: {}",
		"dependabot": "dependabot-alerts: {}",
		"multi-repo": "dependabot-alerts: {}",
	} {
		for _, tt := range []struct {
			roles string
			valid bool
		}{
			{"", true},
			{"write", true},
			{"[write]", true},
			{"maintain", true},
			{"maintainer", true},
			{"[admin]", true},
			{"[admin, maintain, write]", true},
			{"all", false},
			{"read", false},
			{"triage", false},
			{"[admin, write, read]", false},
			{"[write, triage]", false},
		} {
			for _, strict := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/strict=%t", name, tt.roles, strict), func(t *testing.T) {
					roleConfig := ""
					if tt.roles != "" {
						roleConfig = "  roles: " + tt.roles + "\n"
					}
					source := webhookWorkflow(fmt.Sprintf("strict: %t\non:\n  bots: [cao-gateway]\n%s  gateway-dispatch:\n    %s\n", strict, roleConfig, trigger))
					compiler := newWebhookCompiler()
					data, err := compiler.ParseWorkflowString(source, "webhook-roles.md")
					if !tt.valid {
						require.ErrorContains(t, err, "require on.roles with write access or higher")
						assert.Contains(t, err.Error(), "[admin, maintainer, write]")
						return
					}
					require.NoError(t, err)
					output, err := compiler.CompileToYAML(data, "webhook-roles.md")
					require.NoError(t, err)
					preActivation := extractJobSection(output, "pre_activation")
					assert.NotContains(t, preActivation, "check_membership.cjs")
					assert.Contains(t, preActivation, "needs.gateway_provenance.outputs.verified == 'true'")
					assert.Contains(t, extractJobSection(output, "gateway_provenance"), "check_gateway_provenance.cjs")
					assert.True(t, compiler.needsRoleCheck(data, map[string]any{"on": map[string]any{"workflow_dispatch": nil}}))
				})
			}
		}
	}
}

func TestWebhookMinimumRolesCannotBeBypassedByPreparsedData(t *testing.T) {
	for _, roles := range [][]string{nil, {}, {"all"}, {"read"}, {"triage"}, {"write", "read"}, {"admin", "all"}, {"unknown"}} {
		t.Run(fmt.Sprint(roles), func(t *testing.T) {
			data, err := newWebhookCompiler().ParseWorkflowString(webhookWorkflow("strict: false\non:\n  gateway-dispatch:\n    projects-items: {project_id: PVT_example}\n"), "preparsed-roles.md")
			require.NoError(t, err)
			data.Roles = roles
			_, err = newWebhookCompiler().CompileToYAML(data, "preparsed-roles.md")
			require.ErrorContains(t, err, "require on.roles with write access or higher")
			path := filepath.Join(t.TempDir(), "preparsed-roles.md")
			err = newWebhookCompiler().CompileWorkflowData(data, path)
			require.ErrorContains(t, err, "require on.roles with write access or higher")
			_, err = os.Stat(filepath.Join(filepath.Dir(path), "preparsed-roles.lock.yml"))
			require.True(t, os.IsNotExist(err), "invalid roles must not produce a lock file")
		})
	}
}

func TestWebhookMinimumRolesDoesNotRestrictNativeTriggers(t *testing.T) {
	for _, roles := range []string{"all", "read", "triage", "[write, read]"} {
		t.Run(roles, func(t *testing.T) {
			compiler := NewCompiler()
			source := webhookWorkflow("on:\n  issues: {types: [opened]}\n  roles: " + roles + "\n")
			data, err := compiler.ParseWorkflowString(source, "native-roles.md")
			require.NoError(t, err)
			_, err = compiler.CompileToYAML(data, "native-roles.md")
			require.NoError(t, err)
		})
	}
}

func TestGatewayDispatchSettingsSchemaParity(t *testing.T) {
	settings := map[string]any{
		"admission":                          true,
		"allow-bot-authored-trigger-comment": true, "bots": []any{"cao-gateway"}, "cooldown": "5m",
		"github-app":   map[string]any{"client-id": "${{ vars.APP_ID }}", "private-key": "${{ secrets.APP_KEY }}"},
		"github-token": "${{ secrets.TOKEN }}", "labels": []any{"review"}, "manual-approval": "production",
		"needs": []any{"prepare"}, "permissions": map[string]any{"issues": "read"}, "reaction": "none",
		"report-blocked-version": false, "restore-memory": false, "roles": []any{"write"},
		"skip-author-associations": map[string]any{"issue": []any{"BOT"}}, "skip-bots": []any{"other-bot"},
		"skip-if-check-failing": true, "skip-if-match": "is:issue", "skip-if-no-match": "is:pr",
		"skip-roles": []any{"triage"}, "stale-check": false, "status-comment": false,
		"steps": []any{map[string]any{"run": "echo ready"}}, "stop-after": "+1d",
	}
	require.Len(t, settings, len(gatewayDispatchOnSettings))
	for _, name := range gatewayDispatchOnSettings {
		value, exists := settings[name]
		require.True(t, exists, name)
		on := map[string]any{"gateway-dispatch": map[string]any{"slash-command": "review"}, name: value}
		_, err := ParseWebhookSubscriptions(on, "settings")
		require.NoError(t, err, name)
		require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": on}, "settings.md"), name)
	}
}

func TestGatewayExclusiveRouteCannotBeBypassedByPreparsedData(t *testing.T) {
	for _, mutate := range []func(*WorkflowData){
		func(data *WorkflowData) { data.On += "\n  push: {}\n" },
		func(data *WorkflowData) { data.On = "on:\n  workflow_call:\n" },
		func(data *WorkflowData) { data.On = "on:\n  repository_dispatch: {}\n" },
		func(data *WorkflowData) { data.Command = []string{"review"} },
		func(data *WorkflowData) { data.LabelCommand = []string{"review"} },
	} {
		data, err := newWebhookCompiler().ParseWorkflowString(webhookWorkflow("strict: false\non:\n  gateway-dispatch:\n    slash-command: review\n"), "exclusive.md")
		require.NoError(t, err)
		mutate(data)
		_, err = newWebhookCompiler().CompileToYAML(data, "exclusive.md")
		require.ErrorContains(t, err, "on.gateway-dispatch is exclusive")
		path := filepath.Join(t.TempDir(), "exclusive.md")
		err = newWebhookCompiler().CompileWorkflowData(data, path)
		require.ErrorContains(t, err, "on.gateway-dispatch is exclusive")
		_, err = os.Stat(strings.TrimSuffix(path, ".md") + ".lock.yml")
		require.True(t, os.IsNotExist(err))
	}
}

func TestWebhookRepoConfigValidation(t *testing.T) {
	for _, content := range []string{
		`{"gateway-dispatch":null}`, `{"gateway-dispatch":{}}`,
		`{"gateway-dispatch":"true"}`, `{"gateway-dispatch":1}`,
		`{"gateway-dispatch":[]}`, `{"gateway-dispatch":true`,
	} {
		t.Run(content, func(t *testing.T) {
			root := t.TempDir()
			writeWebhookTriggerConfig(t, root, content)
			_, err := LoadRepoConfig(root)
			require.Error(t, err)
			compiler := NewCompiler()
			compiler.gitRoot = root
			_, err = compiler.ParseWorkflowString(webhookWorkflow("on:\n  gateway-dispatch:\n    dependabot-alerts: {}\n"), "invalid-cao.md")
			require.ErrorContains(t, err, "cannot enable hosted CAO triggers")
		})
	}
}

func TestWebhookFrontmatterCannotEnableTriggers(t *testing.T) {
	for _, declaration := range []string{"gateway-dispatch:\n  ", "features: {gateway-dispatch: true}\n"} {
		compiler := NewCompiler()
		compiler.gitRoot = t.TempDir()
		_, err := compiler.ParseWorkflowString(webhookWorkflow(declaration+"on:\n  gateway-dispatch:\n    projects-items: {project_id: PVT_example}\n"), "frontmatter-cao.md")
		require.Error(t, err)
	}
}

func TestWebhookExperimentalNotices(t *testing.T) {
	for name, events := range map[string][]string{
		"native":   nil,
		"projects": {"projects_v2_item"},
		"security": {"dependabot_alert", "code_scanning_alert"},
		"both":     {"projects_v2_item", "code_scanning_alert"},
		"jira":     {"jira_issue"},
		"linear":   {"linear_issue"},
	} {
		t.Run(name, func(t *testing.T) {
			data := &WorkflowData{}
			for _, event := range events {
				data.WebhookSubscriptions = append(data.WebhookSubscriptions, WebhookSubscription{Event: event})
			}
			compiler := NewCompiler()
			var output bytes.Buffer
			compiler.emitExperimentalFeatureWarningsTo(data, &output)
			expected := len(events)
			if len(events) > 0 {
				expected++
			}
			assert.Equal(t, expected, compiler.GetExperimentalWarningCount())
			if expected > 0 {
				assert.Contains(t, output.String(), "Using experimental feature: on.gateway-dispatch (approved webhook bridge required)")
				assert.Contains(t, output.String(), "approved webhook bridge required")
			}
		})
	}
}
