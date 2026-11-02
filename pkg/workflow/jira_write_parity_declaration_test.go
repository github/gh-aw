//go:build !integration

package workflow

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const jiraParityImport = `imports:
  - uses: ".github/workflows/connectors/jira.md"
    with:
      site: https://example.atlassian.net
      issues: [APP-1]
      projects: [APP]
`

func jiraParityPlan(t *testing.T, imports string) *connectors.Plan {
	t.Helper()
	compiler, data, path, err := parseLinearConnectorWorkflow(t, imports,
		"safe-outputs:\n  jira-create-issue:\n  jira-update-issue:\n"+
			"  jira-update-issue-summary:\n  jira-update-issue-description:\n")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	require.Contains(t, output, "jira_update_issue_summary")
	require.Contains(t, output, "jira_update_issue_description")
	require.NotContains(t, extractJobSection(output, "agent"), "JIRA_REST_BASIC_AUTH")
	return data.Connectors["jira"].SafeOutputs
}

func TestJiraWriteParitySparseDeclarations(t *testing.T) {
	plan := jiraParityPlan(t, jiraParityImport)
	require.Equal(t, []string{"JIRA_REST_BASIC_AUTH"}, plan.Secrets())
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"update-issue-summary", map[string]any{"issue": "APP-1", "summary": linearParityTitle}},
		{"update-issue-description", map[string]any{"issue": "APP-1", "description": "First <&>\nSecond $(not-code)"}},
		{"update-issue", map[string]any{"issue": "APP-1", "summary": linearParityTitle, "description": "Reviewed description"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent, err := plan.PrepareSafeOutput(test.name, test.args)
			require.NoError(t, err)
			calls := 0
			runtime := parityProviderRuntime(t, "https://example.atlassian.net", "JIRA_REST_BASIC_AUTH", func(request *http.Request) string {
				calls++
				require.Equal(t, "PUT", request.Method)
				require.Equal(t, "/rest/api/3/issue/APP-1", request.URL.Path)
				var document struct {
					Fields map[string]any `json:"fields"`
				}
				require.NoError(t, json.NewDecoder(request.Body).Decode(&document))
				require.Len(t, document.Fields, len(test.args)-1)
				if summary, exists := test.args["summary"]; exists {
					require.Equal(t, summary, document.Fields["summary"])
				} else {
					require.NotContains(t, document.Fields, "summary")
				}
				if description, exists := test.args["description"]; exists {
					adf := document.Fields["description"].(map[string]any)
					require.Equal(t, "doc", adf["type"])
					require.EqualValues(t, 1, adf["version"])
					encoded, err := json.Marshal(adf)
					require.NoError(t, err)
					for line := range strings.SplitSeq(description.(string), "\n") {
						part, err := json.Marshal(line)
						require.NoError(t, err)
						require.Contains(t, string(encoded), string(part))
					}
				} else {
					require.NotContains(t, document.Fields, "description")
				}
				return ""
			})
			result, err := plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
			require.NoError(t, err)
			require.Equal(t, http.StatusNoContent, result.StatusCode)
			require.Equal(t, 1, calls, "no read-and-rewrite of untouched content")
			for _, field := range []string{"summary", "description"} {
				if _, exists := test.args[field]; !exists {
					bad := maps.Clone(test.args)
					bad[field] = linearParityTitle
					_, err := plan.PrepareSafeOutput(test.name, bad)
					require.ErrorContains(t, err, "unknown connector argument")
				}
			}
		})
	}
	for _, args := range []map[string]any{
		{"issue": "APP-1", "summary": "short"},
		{"issue": "APP-1"},
		{"issue": "OTHER-1", "summary": linearParityTitle},
	} {
		_, err := plan.PrepareSafeOutput("update-issue-summary", args)
		require.Error(t, err)
	}
	for _, description := range []string{"", " \n"} {
		_, err := plan.PrepareSafeOutput("update-issue-description", map[string]any{"issue": "APP-1", "description": description})
		require.Error(t, err, "Jira clearing remains intentionally unsupported")
	}
}

func TestJiraWriteParityApprovedIssueTypes(t *testing.T) {
	for _, test := range []struct {
		name, binding string
		allowed       []string
		denied        []string
	}{
		{"conservative-default", "", []string{"Task"}, []string{"Bug", "Story", "*"}},
		{"explicit-grants", "      issue-types: [Task, Bug]\n", []string{"Task", "Bug"}, []string{"Story", "bug", "*"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := jiraParityPlan(t, jiraParityImport+test.binding)
			for _, issueType := range test.allowed {
				args := map[string]any{"project": "APP", "issue-type": issueType, "summary": linearParityTitle}
				intent, err := plan.PrepareSafeOutput("create-issue", args)
				require.NoError(t, err)
				runtime := parityProviderRuntime(t, "https://example.atlassian.net", "JIRA_REST_BASIC_AUTH", func(request *http.Request) string {
					var document struct {
						Fields map[string]any `json:"fields"`
					}
					require.NoError(t, json.NewDecoder(request.Body).Decode(&document))
					require.Equal(t, map[string]any{"name": issueType}, document.Fields["issuetype"])
					require.Equal(t, map[string]any{"key": "APP"}, document.Fields["project"])
					require.NotContains(t, document.Fields, "description")
					return `{"id":"10002","key":"APP-2","self":"https://example.atlassian.net/rest/api/3/issue/10002"}`
				})
				_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
				require.NoError(t, err)
			}
			for _, issueType := range test.denied {
				_, err := plan.PrepareSafeOutput("create-issue", map[string]any{"project": "APP", "issue-type": issueType, "summary": linearParityTitle})
				require.Error(t, err)
			}
		})
	}
	for _, binding := range []string{"      issue-types: []\n", "      issue-types: ['*']\n"} {
		_, _, _, err := parseLinearConnectorWorkflow(t, jiraParityImport+binding, "safe-outputs:\n  jira-create-issue:\n")
		require.Error(t, err, "creation needs exact nonempty type grants")
	}
}

func TestJiraWriteParityAuthenticatedSparseFollowups(t *testing.T) {
	plan := jiraParityPlan(t, jiraParityImport+"      issue-types: [Bug]\n")
	host := connectors.CreatedResourceHost{Run: "jira-parity-run", Key: bytes.Repeat([]byte{7}, 32)}
	calls := 0
	runtime := parityProviderRuntime(t, "https://example.atlassian.net", "JIRA_REST_BASIC_AUTH", func(request *http.Request) string {
		calls++
		if request.Method == http.MethodPost {
			require.Equal(t, "/rest/api/3/issue", request.URL.Path)
			return `{"id":"10002","key":"APP-2","self":"https://example.atlassian.net/rest/api/3/issue/10002"}`
		}
		require.Equal(t, "/rest/api/3/issue/APP-2", request.URL.Path)
		var document struct {
			Fields map[string]any `json:"fields"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&document))
		require.Len(t, document.Fields, 1, "receipt resolution must preserve sparse updates")
		if summary, present := document.Fields["summary"]; present {
			require.Equal(t, linearParityTitle, summary)
		} else {
			require.Equal(t, "doc", document.Fields["description"].(map[string]any)["type"])
		}
		return ""
	})
	created, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", map[string]any{
		"project": "APP", "issue-type": "Bug", "summary": linearParityTitle, "temporary_id": "aw_created",
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.CreatedResource)
	host.Receipts = []string{created.CreatedResource}
	for _, test := range []struct {
		name, field, content string
	}{
		{"update-issue-summary", "summary", linearParityTitle},
		{"update-issue-description", "description", "Reviewed description"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := map[string]any{"issue": "#aw_created", test.field: test.content}
			_, err := plan.PrepareSafeOutputIntent(test.name, args)
			require.NoError(t, err, "staging records intent but grants no temporary authority")
			_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, test.name, args)
			require.NoError(t, err)
			before := calls
			unbound := host
			unbound.Receipts = nil
			_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, unbound, test.name, args)
			require.Error(t, err)
			wrongRun := host
			wrongRun.Run = "another-run"
			_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, wrongRun, test.name, args)
			require.Error(t, err)
			wrongKey := host
			wrongKey.Key = bytes.Repeat([]byte{8}, 32)
			_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, wrongKey, test.name, args)
			require.Error(t, err, "receipts must authenticate against the host key")
			duplicate := host
			duplicate.Receipts = []string{created.CreatedResource, created.CreatedResource}
			_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, duplicate, test.name, args)
			require.Error(t, err, "duplicate receipt bindings are rejected")
			raw := maps.Clone(args)
			raw["issue"] = "APP-2"
			_, err = plan.PrepareSafeOutput(test.name, raw)
			require.Error(t, err, "creation does not widen existing issue grants")
			require.Equal(t, before, calls, "missing or cross-run receipts fail before provider access")
		})
	}
	require.Equal(t, 3, calls)
}

func TestJiraWriteParityBundledAuthenticatedSparseFollowups(t *testing.T) {
	plan := jiraParityPlan(t, jiraParityImport+"      issue-types: [Bug]\n")
	encoded, err := plan.Encode()
	require.NoError(t, err)
	host := connectors.CreatedResourceHost{Run: "jira-parity-cross-host", Key: bytes.Repeat([]byte{9}, 32)}
	goCalls := 0
	runtime := parityProviderRuntime(t, "https://example.atlassian.net", "JIRA_REST_BASIC_AUTH", func(request *http.Request) string {
		goCalls++
		if request.Method == http.MethodPost {
			return `{"id":"10002","key":"APP-2","self":"https://example.atlassian.net/rest/api/3/issue/10002"}`
		}
		require.Equal(t, "/rest/api/3/issue/APP-3", request.URL.Path)
		return ""
	})
	created, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", map[string]any{
		"project": "APP", "issue-type": "Bug", "summary": linearParityTitle, "temporary_id": "aw_golang",
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.CreatedResource)
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	assets, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	input, err := json.Marshal(map[string]any{
		"assets": assets, "plan": encoded, "run": host.Run,
		"key": base64.StdEncoding.EncodeToString(host.Key), "receipt": created.CreatedResource,
		"title": linearParityTitle,
	})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", `
const assert = require("node:assert/strict");
const path = require("node:path");
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const { decodePlan, executeConnector } = require(path.join(input.assets, "connector_runtime.cjs"));
const { formatConnectorArguments } = require(path.join(input.assets, "connector_content.cjs"));
const plan = decodePlan(input.plan);
for (const name of ["update-issue-summary", "update-issue-description"]) {
  assert.equal(plan.spec.safeOutputs[name].createdResources.issue, "create-issue");
}
process.env = {
  GH_AW_WORKFLOW_NAME: "Reviewed parity workflow",
  GITHUB_REPOSITORY: "owner/repository",
  GITHUB_RUN_ID: "123",
};
let calls = 0, secrets = 0;
const env = {
  GH_AW_CONNECTOR_BINDING_RUN: input.run,
  GH_AW_CONNECTOR_BINDING_KEY: input.key,
  GH_AW_CONNECTOR_BINDING_RECEIPTS: JSON.stringify([input.receipt]),
};
Object.defineProperty(env, "GH_AW_CONNECTOR_SECRET_JIRA_REST_BASIC_AUTH", {get() {
  secrets++;
  return "fixture-writer";
}, enumerable: true});
const fetchImpl = async (url, options) => {
  calls++;
  assert.equal(options.headers.Authorization, "Basic fixture-writer");
  const body = JSON.parse(options.body);
  if (options.method === "POST") {
    assert.equal(String(url), "https://example.atlassian.net/rest/api/3/issue");
    assert.equal(body.fields.issuetype.name, "Bug");
    return new Response(JSON.stringify({id:"10003", key:"APP-3", self:"https://example.atlassian.net/rest/api/3/issue/10003"}), {status:201});
  }
  assert.equal(options.method, "PUT");
  assert.match(String(url), /^https:\/\/example\.atlassian\.net\/rest\/api\/3\/issue\/APP-[23]$/);
  assert.equal(Object.keys(body.fields).length, 1, "authenticated resolution must preserve sparse publication");
  if (Object.hasOwn(body.fields, "summary")) {
    assert.equal(body.fields.summary, input.title);
  } else {
    assert.equal(body.fields.description.type, "doc");
    assert.equal(body.fields.description.version, 1);
    assert.ok(JSON.stringify(body.fields.description).includes("gh-aw-agentic-workflow"));
    assert.ok(JSON.stringify(body.fields.description).includes("Trusted policy"));
  }
  return new Response(null, {status:204});
};
const invoke = (name, args, environment = env, staged = false) => executeConnector(
  input.plan, "safe-output", name,
  formatConnectorArguments(plan, name, args, {config:{footer:true, body_footer:"Trusted policy"}}),
  {env:environment, fetchImpl, staged}
);
(async () => {
  const nodeCreated = await invoke("create-issue", {
    project:"APP", "issue-type":"Bug", summary:input.title, temporary_id:"aw_node",
  });
  assert.ok(nodeCreated.createdResource);
  env.GH_AW_CONNECTOR_BINDING_RECEIPTS = JSON.stringify([input.receipt, nodeCreated.createdResource]);
  const missing = {...env, GH_AW_CONNECTOR_BINDING_RECEIPTS:"[]"};
  const wrongRun = {...env, GH_AW_CONNECTOR_BINDING_RUN:"different-run"};
  const wrongKey = {...env, GH_AW_CONNECTOR_BINDING_KEY:Buffer.alloc(32, 8).toString("base64")};
  for (const reference of ["#aw_golang", "aw_node"]) {
    for (const [operation, field, content] of [
      ["update-issue-summary", "summary", input.title],
      ["update-issue-description", "description", "Reviewed description"],
    ]) {
      const args = {issue:reference, [field]:content};
      const result = await invoke(operation, args);
      assert.equal(result.statusCode, 204);
      const before = calls;
      for (const invalid of [missing, wrongRun, wrongKey]) {
        await assert.rejects(invoke(operation, args, invalid));
      }
      assert.equal(calls, before, "invalid receipts must fail before provider access");
    }
  }
  const before = calls;
  const beforeSecrets = secrets;
  const denied = new Proxy({}, {get() { throw new Error("Staging must not read credentials or receipts"); }});
  const stagedCreate = await invoke("create-issue", {
    project:"APP", "issue-type":"Bug", summary:input.title, temporary_id:"aw_staged",
  }, denied, true);
  assert.equal(stagedCreate.staged, true);
  assert.equal(stagedCreate.createdResource, undefined);
  await assert.rejects(invoke("update-issue-summary", {issue:"#aw_staged", summary:input.title}));
  assert.equal(calls, before);
  assert.equal(secrets, beforeSecrets);
  assert.equal(calls, 5);
  process.stdout.write(JSON.stringify({receipt:nodeCreated.createdResource}));
})().catch(error => { console.error(error.stack); process.exitCode = 1; });
`)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var nodeResult struct {
		Receipt string `json:"receipt"`
	}
	require.NoError(t, json.Unmarshal(output, &nodeResult))
	require.NotEmpty(t, nodeResult.Receipt)
	host.Receipts = []string{nodeResult.Receipt}
	for _, test := range []struct {
		operation, field, content string
	}{
		{"update-issue-summary", "summary", linearParityTitle},
		{"update-issue-description", "description", "Reviewed description"},
	} {
		_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, test.operation, map[string]any{
			"issue": "#aw_node", test.field: test.content,
		})
		require.NoError(t, err, "Go consumers must accept the authenticated Node creator receipt")
	}
	require.Equal(t, 3, goCalls)
}
