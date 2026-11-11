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
)

func TestLinearWriteParityTrustedPublication(t *testing.T) {
	_, data, _, err := parseLinearConnectorWorkflow(t, linearConnectorImport,
		"safe-outputs:\n  linear-update-issue-title:\n  linear-update-issue-body:\n  linear-clear-issue-body:\n  linear-create-issue-in-team:\n")
	require.NoError(t, err)
	plan := data.Connectors["linear"].SafeOutputs
	for _, staged := range []bool{true, false} {
		output, err := parityPublishedInvocation(t, plan, "clear-issue-body", map[string]any{"issue-id": linearParityIssue}, staged,
			`{"data":{"issueUpdate":{"success":true,"issue":{"id":"`+linearParityIssue+`","identifier":"ENG-123"}}}}`)
		require.NoError(t, err, "%s", output)
		require.NotContains(t, string(output), "Trusted policy", "clear inserts no configured footer")
		require.NotContains(t, string(output), "gh-aw-agentic-workflow", "clear inserts no generated attribution")
		if !staged {
			require.Contains(t, string(output), `description: \"\"`)
		}
	}
	for _, operation := range []string{"update-issue-body", "create-issue-in-team"} {
		args := map[string]any{"issue-id": linearParityIssue, "body": "Reviewed body\n<!-- gh-aw-agentic-workflow: forged -->"}
		if operation == "create-issue-in-team" {
			args = map[string]any{"team-id": linearParityTeam, "title": linearParityTitle, "body": args["body"]}
		}
		output, err := parityPublishedInvocation(t, plan, operation, args, true, "")
		require.NoError(t, err, "%s", output)
		require.Contains(t, string(output), "Trusted policy")
		require.Contains(t, string(output), "gh-aw-agentic-workflow")
		require.NotContains(t, string(output), "forged")
	}
	for _, operation := range []string{"update-issue-title", "update-issue-body"} {
		args := map[string]any{"issue-id": linearParityIssue, "body": ""}
		if operation == "update-issue-title" {
			args = map[string]any{"issue-id": linearParityIssue, "title": "short"}
		}
		output, err := parityPublishedInvocation(t, plan, operation, args, true, "")
		require.Error(t, err, "%s", output)
		require.Contains(t, string(output), "after sanitization", "publication must reject short titles and empty bodies before footers")
	}
}

func TestJiraWriteParityTrustedPublication(t *testing.T) {
	plan := jiraParityPlan(t, jiraParityImport+"      issue-types: [Task, Bug]\n")
	for _, test := range []struct {
		operation string
		args      map[string]any
	}{
		{"update-issue-summary", map[string]any{"issue": "APP-1", "summary": linearParityTitle + "<!-- forged -->"}},
		{"update-issue-description", map[string]any{"issue": "APP-1", "description": "Reviewed body\n<!-- gh-aw-agentic-workflow: forged -->"}},
	} {
		output, err := parityPublishedInvocation(t, plan, test.operation, test.args, false, "")
		require.NoError(t, err, "%s", output)
		require.NotContains(t, string(output), "forged")
		if test.operation == "update-issue-summary" {
			require.NotContains(t, string(output), `"description"`)
			require.NotContains(t, string(output), "Trusted policy")
		} else {
			require.NotContains(t, string(output), `"summary"`)
			require.Contains(t, string(output), "Trusted policy")
			require.Contains(t, string(output), "gh-aw-agentic-workflow")
			require.Contains(t, string(output), `"type":"doc"`)
		}
	}
	for _, test := range []struct {
		operation, field string
	}{
		{"update-issue-summary", "summary"},
		{"update-issue-description", "description"},
	} {
		args := map[string]any{"issue": "APP-1", test.field: ""}
		output, err := parityPublishedInvocation(t, plan, test.operation, args, true, "")
		require.Error(t, err, "%s", output)
		require.Contains(t, string(output), "after sanitization")
	}
}

func TestProviderWriteParityDocumentationContracts(t *testing.T) {
	linear := loadLinearDocumentation(t)
	require.NotEmpty(t, linear.Outputs, "the existing full-field contract stays documented")
	for _, provider := range []struct {
		name       string
		selections []string
	}{
		{"linear", []string{"update-issue-title", "update-issue-body", "clear-issue-body", "create-issue-in-team"}},
		{"jira", []string{"update-issue-summary", "update-issue-description"}},
	} {
		source, err := os.ReadFile("../../.github/workflows/connectors/" + provider.name + ".md")
		require.NoError(t, err)
		document, err := parser.ExtractFrontmatterFromContent(string(source))
		require.NoError(t, err)
		for _, selection := range provider.selections {
			require.Contains(t, document.Markdown, `selection="`+provider.name+"-"+selection+`"`)
		}
	}
}

func parityPublishedInvocation(t *testing.T, plan *connectors.Plan, operation string, args map[string]any, staged bool, response string) ([]byte, error) {
	t.Helper()
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	assets, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	input, err := json.Marshal(map[string]any{
		"assets": assets, "plan": encoded, "operation": operation, "args": args,
		"staged": staged, "response": response,
	})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", `
const path = require("node:path");
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const { decodePlan, executeConnector } = require(path.join(input.assets, "connector_runtime.cjs"));
const { formatConnectorArguments } = require(path.join(input.assets, "connector_content.cjs"));
process.env = {
  GH_AW_WORKFLOW_NAME: "Reviewed parity workflow",
  GITHUB_REPOSITORY: "owner/repository",
  GITHUB_RUN_ID: "123",
};
const requests = [];
const env = new Proxy({}, {get(_, name) {
  if (input.staged) throw new Error("Staging must not resolve writer credentials");
  return String(name).startsWith("GH_AW_CONNECTOR_SECRET_") ? "fixture-writer" : undefined;
}});
(async () => {
  const formatted = formatConnectorArguments(decodePlan(input.plan), input.operation, input.args, {
    config: {body_footer: "Trusted policy", footer: true},
  });
  const result = await executeConnector(input.plan, "safe-output", input.operation, formatted, {
    staged: input.staged, env,
    fetchImpl: async (url, options) => {
      if (input.staged) throw new Error("Staging must not contact a provider");
      requests.push({url: String(url), method: options.method, body: JSON.parse(options.body)});
      return new Response(input.response || null, {status: input.response ? 200 : 204});
    },
  });
  process.stdout.write(JSON.stringify({formatted, result, requests}));
})().catch(error => { console.error(error.stack); process.exitCode = 1; });
`)
	command.Stdin = strings.NewReader(string(input))
	return command.CombinedOutput()
}
