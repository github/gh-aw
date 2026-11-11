//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func checkConnectorProviderAliasExpansion(t *testing.T, compiler *Compiler, data *WorkflowData, path, provider string) {
	t.Helper()
	declarationPath := filepath.Join(compiler.gitRoot, ".github", "workflows", "connectors", provider+".md")
	source, err := os.ReadFile(declarationPath)
	require.NoError(t, err)
	frontmatter, body, found := strings.Cut(strings.TrimPrefix(string(source), "---\n"), "\n---\n")
	require.True(t, found)
	var expanded map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &expanded))
	parsed, err := parser.ExtractFrontmatterFromContent(string(source))
	require.NoError(t, err)
	expectedJSON, err := json.Marshal(expanded)
	require.NoError(t, err)
	actualJSON, err := json.Marshal(parsed.Frontmatter)
	require.NoError(t, err)
	require.JSONEq(t, string(expectedJSON), string(actualJSON), "goccy must preserve nested aliases and explicit merge overrides")
	encoded, err := yaml.Marshal(expanded)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(declarationPath, []byte("---\n"+string(encoded)+"---\n"+body), 0600))
	recompiled := NewCompiler(WithNoEmit(true), WithVersion(compiler.version))
	recompiled.gitRoot = compiler.gitRoot
	recompiled.repoConfig, recompiled.repoConfigLoaded = compiler.repoConfig, compiler.repoConfigLoaded
	recompiled.SetActionMode(ActionModeDev)
	result, err := recompiled.ParseWorkflowFile(path)
	require.NoError(t, err)
	require.Equal(t, data.WebhookSubscriptions, result.WebhookSubscriptions)
	for phase, plan := range map[string]*connectors.Plan{
		"agent": data.Connectors[provider].Agent, "safe-output": data.Connectors[provider].SafeOutputs,
		"full": data.Connectors[provider].Plan,
	} {
		if plan == nil {
			continue
		}
		var equivalent *connectors.Plan
		switch phase {
		case "agent":
			equivalent = result.Connectors[provider].Agent
		case "safe-output":
			equivalent = result.Connectors[provider].SafeOutputs
		default:
			equivalent = result.Connectors[provider].Plan
		}
		snapshot, err := plan.Encode()
		require.NoError(t, err)
		expandedSnapshot, err := equivalent.Encode()
		require.NoError(t, err)
		require.Equal(t, snapshot, expandedSnapshot, "alias expansion must preserve the complete frozen %s contract", phase)
		decoded, err := connectors.DecodePlan(snapshot)
		require.NoError(t, err)
		require.Equal(t, plan.Digest, decoded.Digest)
		node, err := exec.LookPath("node")
		require.NoError(t, err)
		runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
		require.NoError(t, err)
		command := exec.CommandContext(t.Context(), node, "-e", `
const assert = require("node:assert/strict");
const encoded = require("node:fs").readFileSync(0, "utf8");
const expected = JSON.parse(Buffer.from(encoded, "base64").toString("utf8"));
const actual = require(process.argv[1]).decodePlan(encoded);
assert.deepEqual(JSON.parse(JSON.stringify(actual)), expected);
`, runtime)
		command.Env = []string{}
		command.Stdin = strings.NewReader(snapshot)
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
}

func TestConnectorJiraMCPAliasesPreserveFrozenProfiles(t *testing.T) {
	for _, profile := range []string{"personal", "service-account"} {
		for _, targeted := range []bool{false, true} {
			t.Run(profile+map[bool]string{false: "/all", true: "/targeted"}[targeted], func(t *testing.T) {
				selection := "tools:\n  jira: [" + profile + "]\n"
				expected := []string{profile + "__getJiraIssue", profile + "__searchJiraIssuesUsingJql"}
				if targeted {
					selection = "tools:\n  jira:\n    " + profile + ": [getJiraIssue]\n"
					expected = expected[:1]
				}
				compiler, data, path, err := parseJiraMigrationWorkflow(t, jiraEmbeddedImport+selection+"safe-outputs:\n  jira-add-comment:\n")
				require.NoError(t, err)
				require.Equal(t, expected, data.Connectors["jira"].Agent.ToolNames())
				secret := "JIRA_MCP_PERSONAL_BASIC_AUTH"
				if profile == "service-account" {
					secret = "JIRA_MCP_SERVICE_ACCOUNT_KEY"
				}
				require.Equal(t, []string{secret}, data.Connectors["jira"].Agent.Secrets())
				require.Equal(t, []string{"JIRA_REST_BASIC_AUTH"}, data.Connectors["jira"].SafeOutputs.Secrets())
				checkConnectorProviderAliasExpansion(t, compiler, data, path, "jira")
			})
		}
	}
}

func TestConnectorConfluenceTriggerMergePreservesFrozenEvents(t *testing.T) {
	compiler, root := awConnectorCompiler(t)
	compiler.SetSkipValidation(false)
	installConnectorTestSources(t, root)
	compiler.repoConfig, compiler.repoConfigLoaded = &RepoConfig{GatewayDispatch: true}, true
	path := filepath.Join(root, ".github", "workflows", "confluence.md")
	source := strings.Replace(awConnectorHeader, "on: workflow_dispatch\n",
		"on:\n  gateway-dispatch:\n    confluence: [page-created, page-updated]\n", 1) + `imports:
  - uses: ".github/workflows/connectors/confluence.md"
    with:
      clouds: [11111111-2222-4333-8444-555555555555]
      pages: ["456"]
      spaces: ["123"]
tools:
  confluence: [get-page]
safe-outputs:
  confluence-add-review-comment:
---
Review only approved runbook evidence.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	require.Len(t, data.WebhookSubscriptions, 2)
	plan := data.Connectors["confluence"].Plan
	require.Equal(t, []string{"CONFLUENCE_READ_ACCESS_TOKEN"}, data.Connectors["confluence"].Agent.Secrets())
	require.Equal(t, []string{"CONFLUENCE_COMMENT_WRITE_ACCESS_TOKEN"}, data.Connectors["confluence"].SafeOutputs.Secrets())
	for _, event := range []string{"created", "updated"} {
		native := "avi:confluence:" + event + ":page"
		payload := map[string]any{"eventType": native, "content": map[string]any{
			"id": "456", "space": map[string]any{"id": 123}, "title": "Runbook",
			"type": "page", "status": "current", "version": map[string]any{"number": 2},
		}}
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		projection, err := plan.ProjectEvent("page-"+event, native, body)
		require.NoError(t, err)
		require.Equal(t, "456", projection.Fields["subject"])
		other := "avi:confluence:created:page"
		if event == "created" {
			other = "avi:confluence:updated:page"
		}
		_, err = plan.ProjectEvent("page-"+event, other, body)
		require.ErrorContains(t, err, "native event is not declared")
		payload["eventType"] = other
		body, err = json.Marshal(payload)
		require.NoError(t, err)
		_, err = plan.ProjectEvent("page-"+event, native, body)
		require.ErrorContains(t, err, `does not match "/eventType"`)
	}
	checkConnectorProviderAliasExpansion(t, compiler, data, path, "confluence")
}
