//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	yamlparser "github.com/goccy/go-yaml/parser"
	"github.com/stretchr/testify/require"
)

type connectorCompilerScenario struct {
	provider string
	readers  []string
	secret   string
	output   string
}

var connectorCompilerScenarios = []connectorCompilerScenario{
	{"asana", []string{"get_task"}, "ASANA_READ_ACCESS_TOKEN", "asana-add-comment"},
	{"linear", []string{"get-issue", "list-comments"}, "LINEAR_READ_API_KEY", "linear-add-comment"},
	{"jira", []string{"personal__getJiraIssue"}, "JIRA_MCP_PERSONAL_BASIC_AUTH", "jira-add-comment"},
	{"azure-devops", []string{"get_work_item_description"}, "AZURE_DEVOPS_READ_TOKEN", "azure-devops-comment-on-work-item"},
	{"slack", []string{"read-thread"}, "SLACK_READ_TOKEN", "slack-reply-in-thread"},
	{"teams", []string{"get-channel-message"}, "TEAMS_GRAPH_READ_ACCESS_TOKEN", "create-issue"},
	{"confluence", []string{"get-page"}, "CONFLUENCE_READ_ACCESS_TOKEN", "create-issue"},
	{"sentry", []string{"sentry__get_issue_details"}, "SENTRY_MCP_OAUTH_TOKEN", "create-issue"},
	{"servicenow", []string{"get-incident"}, "SERVICENOW_READ_TOKEN", "servicenow-append-incident-work-note"},
	{"gitlab", []string{"gitlab_get_merge_request"}, "GITLAB_READ_TOKEN", "gitlab-add-merge-request-note"},
	{"pagerduty", []string{"pagerduty_get_incident"}, "PAGERDUTY_READ_AUTHORIZATION", "pagerduty-add-note"},
	{"datadog", []string{"datadog_get_monitor"}, "DATADOG_MONITOR_READ_OAUTH_TOKEN", "create-issue"},
	{"zendesk", []string{"get_ticket"}, "ZENDESK_READ_OAUTH_TOKEN", "create-issue"},
	{"notion", []string{"get_page"}, "NOTION_READ_TOKEN", "notion-add-comment"},
}

func assertConnectorScenarioAuthority(t *testing.T, data *WorkflowData, scenario connectorCompilerScenario) {
	t.Helper()
	require.Len(t, data.Connectors, 1)
	connector := data.Connectors[scenario.provider]
	require.NotNil(t, connector)
	require.NotNil(t, connector.Agent)
	require.Equal(t, scenario.readers, connector.Agent.ToolNames())
	require.Equal(t, []string{scenario.secret}, connector.Agent.Secrets())
	require.Empty(t, connector.Agent.SafeOutputNames())
	require.Nil(t, connector.Context)
	require.Empty(t, data.WebhookSubscriptions)
	require.NotNil(t, data.SafeOutputs)
	require.True(t, templatableBoolIsTrue(data.SafeOutputs.Staged))
	require.False(t, usesSafeOutputsArtifactStaging(data.SafeOutputs))
	if scenario.output == "create-issue" {
		require.Nil(t, connector.SafeOutputs, "handoff workflows select no provider writer")
		require.Empty(t, data.SafeOutputs.Scripts)
		require.NotNil(t, data.SafeOutputs.CreateIssues)
		require.NotNil(t, data.SafeOutputs.CreateIssues.Max)
		require.Equal(t, "1", *data.SafeOutputs.CreateIssues.Max)
	} else {
		require.NotNil(t, connector.SafeOutputs)
		require.Equal(t, []string{strings.TrimPrefix(scenario.output, scenario.provider+"-")}, connector.SafeOutputs.SafeOutputNames())
		require.Len(t, data.SafeOutputs.Scripts, 1)
		require.Nil(t, data.SafeOutputs.CreateIssues, "provider effects do not imply GitHub issue creation")
	}
}

func assertConnectorScenarioEmission(t *testing.T, compiler *Compiler, data *WorkflowData, scenario connectorCompilerScenario, output string) {
	t.Helper()
	require.Contains(t, output, "${{ secrets."+scenario.secret+" }}", "staging must retain the selected reader credential")
	if plan := data.Connectors[scenario.provider].SafeOutputs; plan != nil {
		for _, secret := range plan.Secrets() {
			require.NotContains(t, output, "${{ secrets."+secret+" }}", "staging must omit provider writer delivery")
		}
	}
	for _, section := range compiler.collectPromptSections(data) {
		require.False(t, section.IsFile && section.Content == connectorContextPath)
	}
	require.NotContains(t, output, "CONNECTOR_SCENARIO_AUTHORING_ONLY")
	require.NotContains(t, output, "Upload upload-artifact staging")
}

func TestConnectorScenarioImports(t *testing.T) {
	for _, scenario := range connectorCompilerScenarios {
		t.Run(scenario.provider, func(t *testing.T) {
			for _, pinned := range []bool{false, true} {
				name := "local"
				if pinned {
					name = "pinned"
				}
				t.Run(name, func(t *testing.T) {
					compiler, path := prepareConnectorScenario(t, scenario, pinned, nil)
					data, err := compiler.ParseWorkflowFile(path)
					require.NoError(t, err)
					assertConnectorScenarioAuthority(t, data, scenario)
					output, err := compiler.CompileToYAML(data, path)
					require.NoError(t, err)
					assertConnectorScenarioEmission(t, compiler, data, scenario, output)
					if pinned {
						_, err := os.Stat(filepath.Join(compiler.gitRoot, ".github/workflows/connectors", scenario.provider+".md"))
						require.True(t, os.IsNotExist(err), "cached imports must not install a local provider catalog")
					}
				})
			}
		})
	}
}

func TestConnectorScenarioImportDoesNotSelectCapabilities(t *testing.T) {
	for _, scenario := range connectorCompilerScenarios {
		t.Run(scenario.provider, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, scenario, false, func(consumer, _ map[string]any) {
				tools := consumer["tools"].(map[string]any)
				delete(tools, scenario.provider)
				delete(consumer, "safe-outputs")
			})
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			require.Empty(t, data.Connectors)
			require.Empty(t, data.WebhookSubscriptions)
		})
	}
}

func TestConnectorScenarioIndependentPhases(t *testing.T) {
	for _, scenario := range connectorCompilerScenarios {
		t.Run(scenario.provider, func(t *testing.T) {
			for _, phase := range []string{"read-only", "effect-only"} {
				t.Run(phase, func(t *testing.T) {
					compiler, path := prepareConnectorScenario(t, scenario, false, func(consumer, _ map[string]any) {
						if phase == "read-only" {
							delete(consumer, "safe-outputs")
						} else {
							delete(consumer["tools"].(map[string]any), scenario.provider)
						}
					})
					data, err := compiler.ParseWorkflowFile(path)
					require.NoError(t, err)
					output, err := compiler.CompileToYAML(data, path)
					require.NoError(t, err)
					if phase == "read-only" {
						connector := data.Connectors[scenario.provider]
						require.NotNil(t, connector)
						require.NotNil(t, connector.Agent)
						require.Equal(t, scenario.readers, connector.Agent.ToolNames())
						require.Equal(t, []string{scenario.secret}, connector.Agent.Secrets())
						require.Nil(t, connector.SafeOutputs)
						require.Nil(t, connector.Context)
						return
					}
					require.NotContains(t, output, "${{ secrets."+scenario.secret+" }}")
					if scenario.output == "create-issue" {
						require.Empty(t, data.Connectors, "a GitHub handoff does not select provider capabilities")
						require.NotNil(t, data.SafeOutputs)
						require.NotNil(t, data.SafeOutputs.CreateIssues)
						return
					}
					connector := data.Connectors[scenario.provider]
					require.NotNil(t, connector)
					require.Nil(t, connector.Agent)
					require.Nil(t, connector.Context)
					require.NotNil(t, connector.SafeOutputs)
					require.Equal(t, []string{strings.TrimPrefix(scenario.output, scenario.provider+"-")}, connector.SafeOutputs.SafeOutputNames())
					require.Len(t, connector.SafeOutputs.Secrets(), 1)
					for _, secret := range connector.SafeOutputs.Secrets() {
						require.NotContains(t, output, "${{ secrets."+secret+" }}")
					}
				})
			}
		})
	}
}

func TestConnectorScenarioNumericIDsRequireStrings(t *testing.T) {
	for _, scenario := range connectorCompilerScenarios {
		var input string
		var number int
		switch scenario.provider {
		case "azure-devops":
			input, number = "work-item", 9876
		case "zendesk":
			input, number = "ticket", 35436
		default:
			continue
		}
		t.Run(scenario.provider, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, scenario, false, func(consumer, _ map[string]any) {
				for _, raw := range consumer["imports"].([]any) {
					entry := raw.(map[string]any)
					entry["with"].(map[string]any)[input] = number
				}
			})
			_, err := compiler.ParseWorkflowFile(path)
			require.ErrorContains(t, err, "string", "numeric-looking resource IDs must not be coerced")
		})
	}
}

func TestConnectorScenarioRejectsInvalidConfiguration(t *testing.T) {
	for _, scenario := range connectorCompilerScenarios {
		t.Run(scenario.provider, func(t *testing.T) {
			mutations := map[string]func(map[string]any, map[string]any){
				"unknown-reader": func(consumer, _ map[string]any) {
					consumer["tools"].(map[string]any)[scenario.provider] = []any{"missing-capability"}
				},
				"unknown-effect": func(consumer, _ map[string]any) {
					consumer["safe-outputs"] = map[string]any{"staged": true, scenario.provider + "-missing-operation": map[string]any{}}
				},
				"missing-exact-grants": func(_, declaration map[string]any) {
					for name := range declaration["resources"].(map[string]any) {
						declaration["resources"].(map[string]any)[name] = []any{}
					}
				},
				"writer-credential-for-reader": func(_, declaration map[string]any) {
					for _, credential := range declaration["credentials"].(map[string]any) {
						credential.(map[string]any)["phase"] = "safe-output"
					}
				},
			}
			for name, mutate := range mutations {
				t.Run(name, func(t *testing.T) {
					compiler, path := prepareConnectorScenario(t, scenario, false, mutate)
					_, err := compiler.ParseWorkflowFile(path)
					require.Error(t, err, "invalid connector configuration must fail before runtime generation")
					messages := map[string]string{
						"unknown-reader":               "missing-capability",
						"unknown-effect":               "missing-operation",
						"missing-exact-grants":         "resource",
						"writer-credential-for-reader": "phase",
					}
					require.NotEmpty(t, messages[name], "each mutation must identify its intended rejection")
					require.ErrorContains(t, err, messages[name])
				})
			}
		})
	}
}

func prepareConnectorScenario(t *testing.T, scenario connectorCompilerScenario, pinned bool, mutate func(map[string]any, map[string]any)) (*Compiler, string) {
	t.Helper()
	consumerBytes, err := os.ReadFile(filepath.Join("../..", connectorGoldenWorkflowDirectory, scenario.provider+"-golden.md"))
	require.NoError(t, err)
	declarationPath := ".github/workflows/connectors/" + scenario.provider + ".md"
	declarationBytes, err := os.ReadFile(filepath.Join("../..", declarationPath))
	require.NoError(t, err)
	consumer, err := parser.ExtractFrontmatterFromContent(string(consumerBytes))
	require.NoError(t, err)
	declaration, err := parser.ExtractFrontmatterFromContent(string(declarationBytes))
	require.NoError(t, err)
	fields := declaration.Frontmatter["connectors"].(map[string]any)[scenario.provider].(map[string]any)
	originalFields := map[string][]byte{}
	for name, value := range fields {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		originalFields[name] = encoded
	}
	if mutate != nil {
		mutate(consumer.Frontmatter, fields)
	}
	consumerBody, err := parser.ExtractMarkdownContent(string(consumerBytes))
	require.NoError(t, err)
	declarationSource := connectorScenarioDeclaration(t, string(declarationBytes), scenario.provider, fields, originalFields)
	declarationSource = strings.Replace(declarationSource, "</connector-documentation>", "<scenario>CONNECTOR_SCENARIO_AUTHORING_ONLY</scenario>\n</connector-documentation>", 1)
	_, err = parser.ExtractFrontmatterFromContent(declarationSource)
	require.NoError(t, err, "negative scenarios must use syntactically valid declarations")
	root := t.TempDir()
	path := filepath.Join(root, ".github/workflows", scenario.provider+"-golden.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	if pinned {
		const revision = "1111111111111111111111111111111111111111"
		_, err := parser.NewImportCache(root).Set("github", "gh-aw", declarationPath, revision, []byte(declarationSource))
		require.NoError(t, err)
		imports := consumer.Frontmatter["imports"].([]any)
		for _, raw := range imports {
			entry := raw.(map[string]any)
			if entry["uses"] == declarationPath {
				entry["uses"] = "github/gh-aw/" + declarationPath + "@" + revision
			}
		}
	} else {
		local := filepath.Join(root, declarationPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(local), 0700))
		require.NoError(t, os.WriteFile(local, []byte(declarationSource), 0600))
	}
	if pinned || mutate != nil {
		consumerBytes = []byte(connectorScenarioMarkdown(t, consumer.Frontmatter, consumerBody))
	}
	require.NoError(t, os.WriteFile(path, consumerBytes, 0600))
	compiler := NewCompiler(WithVersion("v0.0.0"))
	compiler.gitRoot = root
	compiler.SetActionMode(ActionModeDev)
	compiler.SetSkipValidation(false)
	return compiler, path
}

func connectorScenarioMarkdown(t *testing.T, frontmatter map[string]any, body string) string {
	t.Helper()
	header, err := yaml.Marshal(frontmatter)
	require.NoError(t, err)
	return "---\n" + string(header) + "---\n" + body
}

func connectorScenarioDeclaration(t *testing.T, source, provider string, fields map[string]any, original map[string][]byte) string {
	t.Helper()
	header, body, found := strings.Cut(strings.TrimPrefix(source, "---\n"), "\n---\n")
	require.True(t, found)
	document, err := yamlparser.ParseBytes([]byte(header), 0)
	require.NoError(t, err)
	changed := false
	for name, value := range fields {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		if bytes.Equal(encoded, original[name]) {
			continue
		}
		// Replace leaf values only, retaining template quotes and YAML anchors.
		for key, leaf := range value.(map[string]any) {
			path := (&yaml.PathBuilder{}).Root().Child("connectors").Child(provider).Child(name).Child(key)
			switch name {
			case "resources":
			case "credentials":
				path = path.Child("phase")
				leaf = leaf.(map[string]any)["phase"]
			default:
				t.Fatalf("unsupported declaration mutation %q", name)
			}
			replacement, err := yaml.Marshal(leaf)
			require.NoError(t, err)
			require.NoError(t, path.Build().ReplaceWithReader(document, bytes.NewReader(replacement)))
		}
		changed = true
	}
	if !changed {
		return source
	}
	return "---\n" + strings.TrimSuffix(document.String(), "\n") + "\n---\n" + body
}
