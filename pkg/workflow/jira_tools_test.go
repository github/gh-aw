package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandJiraToolConfigServiceAccount(t *testing.T) {
	tools := map[string]any{
		"jira": map[string]any{
			"auth": map[string]any{
				"type":  jiraServiceAccountAuth,
				"token": "${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}",
			},
			"allowed": []any{"getJiraIssue", "searchJiraIssuesUsingJql"},
		},
	}

	require.NoError(t, expandJiraToolConfig(tools))
	config := tools["jira"].(map[string]any)
	assert.Equal(t, "http", config["type"])
	assert.Equal(t, defaultJiraMCPURL, config["url"])
	assert.Equal(t, "Bear"+"er ${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}", config["headers"].(map[string]any)["Authorization"])

	mcpConfig, err := getMCPConfig(config, "jira")
	require.NoError(t, err)
	assert.Nil(t, mcpConfig.Auth)
	assert.Equal(t, []string{"getJiraIssue", "searchJiraIssuesUsingJql"}, mcpConfig.Allowed)
}

func TestExpandJiraToolConfigAPIToken(t *testing.T) {
	tools := map[string]any{
		"jira": map[string]any{
			"url": "https://example.atlassian.net/mcp",
			"auth": map[string]any{
				"type":  jiraAPITokenAuth,
				"email": "${{ secrets.ATLASSIAN_EMAIL }}",
				"token": "${{ secrets.ATLASSIAN_API_TOKEN }}",
			},
			"allowed": []any{"getVisibleJiraProjects"},
		},
	}

	require.NoError(t, expandJiraToolConfig(tools))
	config := tools["jira"].(map[string]any)
	assert.Equal(t, "https://example.atlassian.net/mcp", config["url"])
	assert.Equal(t, "Basic ${{ env.GH_AW_JIRA_BASIC_AUTH }}", config["headers"].(map[string]any)["Authorization"])

	stepEnv := jiraAPIAuthStepEnv(tools)
	assert.Equal(t, "${{ secrets.ATLASSIAN_EMAIL }}", stepEnv[jiraEmailEnvVar])
	assert.Equal(t, "${{ secrets.ATLASSIAN_API_TOKEN }}", stepEnv[jiraTokenEnvVar])

	var setup strings.Builder
	writeJiraAPIAuthPreparation(&setup, tools)
	assert.Contains(t, setup.String(), `printf '%s:%s' "$GH_AW_JIRA_EMAIL" "$GH_AW_JIRA_TOKEN"`)
	assert.Contains(t, setup.String(), `echo "::add-mask::${GH_AW_JIRA_BASIC_AUTH}"`)
	assert.NotContains(t, setup.String(), "ATLASSIAN_EMAIL")
	assert.NotContains(t, setup.String(), "ATLASSIAN_API_TOKEN")
}

func TestExpandJiraToolConfigRejectsInvalidAuth(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		message string
	}{
		{
			name:    "unsupported auth type",
			config:  map[string]any{"auth": map[string]any{"type": "oauth", "token": "${{ secrets.TOKEN }}"}},
			message: "tools.jira.auth.type must be",
		},
		{
			name:    "missing token",
			config:  map[string]any{"auth": map[string]any{"type": jiraServiceAccountAuth}},
			message: "tools.jira.auth.token is required",
		},
		{
			name:    "missing email",
			config:  map[string]any{"auth": map[string]any{"type": jiraAPITokenAuth, "token": "${{ secrets.TOKEN }}"}},
			message: "tools.jira.auth.email is required",
		},
		{
			name:    "literal credential",
			config:  map[string]any{"auth": map[string]any{"type": jiraServiceAccountAuth, "token": "literal-token"}},
			message: "must be a direct GitHub Actions secret expression",
		},
		{
			name: "insecure endpoint",
			config: map[string]any{
				"url":  "http://mcp.atlassian.example/mcp",
				"auth": map[string]any{"type": jiraServiceAccountAuth, "token": "${{ secrets.TOKEN }}"},
			},
			message: "tools.jira.url must be an HTTPS URL",
		},
		{
			name: "endpoint with credentials",
			config: map[string]any{
				"url":  strings.Join([]string{"https://example", "credential@mcp.atlassian.example/mcp"}, ":"),
				"auth": map[string]any{"type": jiraServiceAccountAuth, "token": "${{ secrets.TOKEN }}"},
			},
			message: "without embedded credentials",
		},
		{
			name: "unknown field",
			config: map[string]any{
				"oauth": true,
				"auth":  map[string]any{"type": jiraServiceAccountAuth, "token": "${{ secrets.TOKEN }}"},
			},
			message: "tools.jira.oauth is not supported",
		},
		{
			name: "malformed allowlist",
			config: map[string]any{
				"allowed": []any{"getJiraIssue", 1},
				"auth":    map[string]any{"type": jiraServiceAccountAuth, "token": "${{ secrets.TOKEN }}"},
			},
			message: "tools.jira.allowed must contain only",
		},
		{
			name: "missing allowlist",
			config: map[string]any{
				"auth": map[string]any{"type": jiraServiceAccountAuth, "token": "${{ secrets.TOKEN }}"},
			},
			message: "tools.jira.allowed is required",
		},
		{
			name: "write tool",
			config: map[string]any{
				"allowed": []any{"createJiraIssue"},
				"auth":    map[string]any{"type": jiraServiceAccountAuth, "token": "${{ secrets.TOKEN }}"},
			},
			message: `tool "createJiraIssue" is not an approved read-only Jira tool`,
		},
		{
			name: "duplicate tool",
			config: map[string]any{
				"allowed": []any{"getJiraIssue", "getJiraIssue"},
				"auth":    map[string]any{"type": jiraServiceAccountAuth, "token": "${{ secrets.TOKEN }}"},
			},
			message: `contains duplicate tool "getJiraIssue"`,
		},
		{
			name: "wildcard combined with named tool",
			config: map[string]any{
				"allowed": []any{"*", "getJiraIssue"},
				"auth":    map[string]any{"type": jiraServiceAccountAuth, "token": "${{ secrets.TOKEN }}"},
			},
			message: `tools.jira.allowed must contain only "*" when the wildcard is used`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := map[string]any{"jira": tt.config}
			err := expandJiraToolConfig(tools)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.message)
		})
	}
}

func TestValidateJiraAllowedToolsAcceptsApprovedReadOnlyTools(t *testing.T) {
	allowed := []string{
		"getIssueLinkTypes",
		"getJiraIssue",
		"getJiraIssueRemoteIssueLinks",
		"getJiraIssueTypeMetaWithFields",
		"getJiraProjectIssueTypesMetadata",
		"getTransitionsForJiraIssue",
		"getVisibleJiraProjects",
		"lookupJiraAccountId",
		"searchJiraIssuesUsingJql",
	}
	require.NoError(t, validateJiraAllowedTools(allowed))
}

func TestExpandJiraToolConfigWildcardExpandsToApprovedReadOnlyTools(t *testing.T) {
	tools := map[string]any{
		"jira": map[string]any{
			"auth":    map[string]any{"type": jiraServiceAccountAuth, "token": "${{ secrets.ATLASSIAN_TOKEN }}"},
			"allowed": []any{"*"},
		},
	}

	require.NoError(t, expandJiraToolConfig(tools))
	config := tools["jira"].(map[string]any)

	mcpConfig, err := getMCPConfig(config, "jira")
	require.NoError(t, err)
	assert.Equal(t, jiraApprovedReadOnlyToolsList, mcpConfig.Allowed)
	assert.NotContains(t, mcpConfig.Allowed, "*")
}

func TestExpandJiraToolConfigDisablesInheritedConfiguration(t *testing.T) {
	tools := map[string]any{"jira": false}
	require.NoError(t, expandJiraToolConfig(tools))
	assert.NotContains(t, tools, "jira")
}

func TestJiraHeadersUseRuntimeEnvironmentReferences(t *testing.T) {
	serviceHeaders := renderCustomMCPHeadersTOML(
		map[string]string{"Authorization": "Bear" + "er ${{ secrets.JIRA_TOKEN }}"},
		map[string]string{"JIRA_TOKEN": "${{ secrets.JIRA_TOKEN }}"},
	)
	assert.Equal(t, "Bear"+"er ${JIRA_TOKEN}", serviceHeaders["Authorization"])

	apiHeaders := renderCustomMCPHeadersTOML(
		map[string]string{"Authorization": "Basic ${{ env.GH_AW_JIRA_BASIC_AUTH }}"},
		nil,
	)
	assert.Equal(t, "Basic ${GH_AW_JIRA_BASIC_AUTH}", apiHeaders["Authorization"])
}

func TestJiraAPITokenCompilation(t *testing.T) {
	workflow := `---
on:
  workflow_dispatch:
strict: false
engine: copilot
tools:
  jira:
    auth:
      type: api-token
      email: ${{ secrets.ATLASSIAN_EMAIL }}
      token: ${{ secrets.ATLASSIAN_API_TOKEN }}
    allowed:
      - getJiraIssue
      - searchJiraIssuesUsingJql
---

Read Jira issues.
`
	file, err := os.CreateTemp("", "jira-api-token-*.md")
	require.NoError(t, err)
	defer os.Remove(file.Name())
	_, err = file.WriteString(workflow)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	data, err := compiler.ParseWorkflowFile(file.Name())
	require.NoError(t, err)
	compiled, _, _, err := compiler.generateYAML(data, file.Name())
	require.NoError(t, err)

	assert.Contains(t, compiled, defaultJiraMCPURL)
	assert.Contains(t, compiled, `"getJiraIssue"`)
	assert.Contains(t, compiled, `"searchJiraIssuesUsingJql"`)
	assert.Contains(t, compiled, `GH_AW_JIRA_EMAIL: ${{ secrets.ATLASSIAN_EMAIL }}`)
	assert.Contains(t, compiled, `GH_AW_JIRA_TOKEN: ${{ secrets.ATLASSIAN_API_TOKEN }}`)
	assert.Contains(t, compiled, `printf '%s:%s' "$GH_AW_JIRA_EMAIL" "$GH_AW_JIRA_TOKEN"`)
	assert.Contains(t, compiled, `"Authorization": "Basic \${GH_AW_JIRA_BASIC_AUTH}"`)
	assert.NotContains(t, compiled, `"email": "${{ secrets.ATLASSIAN_EMAIL }}"`)
	assert.NotContains(t, compiled, `"token": "${{ secrets.ATLASSIAN_API_TOKEN }}"`)
}

func TestJiraServiceAccountCompilationAcrossEngines(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex", "gemini", "pi"} {
		t.Run(engine, func(t *testing.T) {
			workflow := `---
on:
  workflow_dispatch:
strict: false
engine: ` + engine + `
tools:
  jira:
    auth:
      type: service-account
      token: ${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}
    allowed:
      - getJiraIssue
---

Read a Jira issue.
`
			file, err := os.CreateTemp("", "jira-service-account-*.md")
			require.NoError(t, err)
			defer os.Remove(file.Name())
			_, err = file.WriteString(workflow)
			require.NoError(t, err)
			require.NoError(t, file.Close())

			compiler := NewCompiler()
			compiler.SetSkipValidation(true)
			data, err := compiler.ParseWorkflowFile(file.Name())
			require.NoError(t, err)
			compiled, _, _, err := compiler.generateYAML(data, file.Name())
			require.NoError(t, err)

			assert.Contains(t, compiled, defaultJiraMCPURL)
			assert.Contains(t, compiled, "getJiraIssue")
			assert.Contains(t, compiled, "ATLASSIAN_SERVICE_ACCOUNT_API_KEY: ${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}")
			assert.NotContains(t, compiled, `"token": "${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}"`)
			if engine == "codex" {
				assert.Contains(t, compiled, "Bear"+"er ${ATLASSIAN_SERVICE_ACCOUNT_API_KEY}")
			} else {
				assert.Contains(t, compiled, "Bear"+`er \${ATLASSIAN_SERVICE_ACCOUNT_API_KEY}`)
			}
		})
	}
}

func TestImportedJiraConfigurationIsValidatedAfterMerge(t *testing.T) {
	tests := []struct {
		name    string
		jira    string
		message string
	}{
		{
			name: "missing allowlist",
			jira: `    auth:
      type: service-account
      token: ${{ secrets.ATLASSIAN_TOKEN }}`,
			message: "tools.jira.allowed is required",
		},
		{
			name: "literal credential",
			jira: `    allowed:
      - getJiraIssue
    auth:
      type: service-account
      token: literal-token`,
			message: "must be a direct GitHub Actions secret expression",
		},
		{
			name: "insecure endpoint",
			jira: `    url: http://mcp.atlassian.example/mcp
    allowed:
      - getJiraIssue
    auth:
      type: service-account
      token: ${{ secrets.ATLASSIAN_TOKEN }}`,
			message: "tools.jira.url must be an HTTPS URL",
		},
		{
			name: "unknown field",
			jira: `    browser-oauth: true
    allowed:
      - getJiraIssue
    auth:
      type: service-account
      token: ${{ secrets.ATLASSIAN_TOKEN }}`,
			message: "tools.jira.browser-oauth is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			importPath := filepath.Join(dir, "jira.md")
			require.NoError(t, os.WriteFile(importPath, []byte("---\ntools:\n  jira:\n"+tt.jira+"\n---\n"), 0o600))

			workflowPath := filepath.Join(dir, "workflow.md")
			workflow := `---
on:
  workflow_dispatch:
strict: false
engine: copilot
imports:
  - jira.md
---

Read Jira issues.
`
			require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o600))

			err := NewCompiler().CompileWorkflow(workflowPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.message)
		})
	}
}
